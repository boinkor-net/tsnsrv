package tsnsrv

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"golang.org/x/exp/slog"
	"tailscale.com/client/tailscale/apitype"
)

type contextKey struct{}

var proxyContextKey = contextKey{}

var (
	requestDurations = promauto.NewSummary(prometheus.SummaryOpts{
		Name:       "tsnsrv_request_duration_ns",
		Help:       "Duration of requests served",
		Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
	})
	responseStatusClasses = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "tsnsrv_response_status_classes",
		Help: "Responses by status code class (1xx, etc)",
	}, []string{"status_code_class"})
	proxyErrors = promauto.NewCounter(prometheus.CounterOpts{
		Name: "tsnsrv_proxy_errors",
		Help: "Number of errors encountered proxying requests",
	})
)

type proxyContext struct {
	start        time.Time
	who          *apitype.WhoIsResponse
	originalURL  *url.URL
	rewrittenURL *url.URL
}

func (c *proxyContext) observeResponse(res *http.Response) {
	elapsed := time.Since(c.start)
	requestDurations.Observe(float64(elapsed))

	statusClass := fmt.Sprintf("%dxx", res.StatusCode/100)
	responseStatusClasses.With(prometheus.Labels{"status_code_class": statusClass}).Inc()

	login := ""
	node := ""
	if c.who != nil {
		login = c.who.UserProfile.LoginName
		node = c.who.Node.Name
	}
	slog.Info("served",
		"original", c.originalURL,
		"rewritten", c.rewrittenURL,
		"origin_login", login,
		"origin_node", node,
		"duration", elapsed,
		"http_status", res.StatusCode,
	)
}

func (s *ValidTailnetSrv) modifyResponse(res *http.Response) error {
	p := res.Request.Context().Value(proxyContextKey).(*proxyContext)
	if p != nil {
		p.observeResponse(res)
	}
	return nil
}

func (s *ValidTailnetSrv) errorHandler(rw http.ResponseWriter, _ *http.Request, err error) {
	slog.Warn("proxy error",
		"error", err,
	)
	proxyErrors.Inc()
	rw.WriteHeader(http.StatusBadGateway)
}

func (s *ValidTailnetSrv) rewrite(r *httputil.ProxyRequest) {
	r.SetURL(s.DestURL)
	if r.In.URL.Path == "" {
		r.Out.URL.Path = s.DestURL.Path
	}

	r.SetXForwarded()
	if s.RecommendedProxyHeaders {
		if r.In.TLS == nil {
			r.Out.Header.Set("X-Scheme", "http")
		} else {
			r.Out.Header.Set("X-Scheme", "https")
		}
		r.Out.Host = r.In.Host
		remoteIP, _, err := net.SplitHostPort(r.In.RemoteAddr)
		if err == nil {
			r.Out.Header.Set("X-Real-Ip", remoteIP)
		}
		hostOnly, port, err := net.SplitHostPort(r.In.Host)
		if err != nil {
			r.Out.Header.Set("X-Forwarded-Server", r.In.Host)
		} else {
			r.Out.Header.Set("X-Forwarded-Server", hostOnly)
			r.Out.Header.Set("X-Forwarded-Port", port)
		}
	}

	for h, vals := range s.UpstreamHeaders {
		r.Out.Header[h] = vals
	}

	who := s.setWhoisHeaders(r)
	r.Out = r.Out.WithContext(context.WithValue(r.Out.Context(), proxyContextKey, &proxyContext{
		start:        time.Now(),
		originalURL:  r.In.URL,
		rewrittenURL: r.Out.URL,
		who:          who,
	}))
}

// Clean up and set user/node identity headers:.
func (s *ValidTailnetSrv) setWhoisHeaders(r *httputil.ProxyRequest) *apitype.WhoIsResponse {
	// First, clean out any input we received that looks like TS setting headers:
	for k := range r.Out.Header {
		if strings.HasPrefix(k, "X-Tailscale-") {
			r.Out.Header.Del(k)
		}
	}
	if s.SuppressWhois || s.client == nil {
		return nil
	}

	ctx := r.In.Context()
	if s.WhoisTimeout > 0 {
		var cancel func()
		ctx, cancel = context.WithTimeout(ctx, s.WhoisTimeout)
		defer cancel()
	}
	who, err := s.client.WhoIs(ctx, r.In.RemoteAddr)
	if err != nil {
		slog.Warn("could not look up requestor identity",
			"error", err,
			"request", r.In,
		)
		return nil
	}
	h := r.Out.Header
	h.Set("X-Tailscale-User", who.UserProfile.ID.String())
	login := who.UserProfile.LoginName
	h.Set("X-Tailscale-User-LoginName", login)
	ll, ld, splitable := strings.Cut(login, "@")
	if splitable {
		h.Set("X-Tailscale-User-LoginName-Localpart", ll)
		h.Set("X-Tailscale-User-LoginName-Domain", ld)
	}
	h.Set("X-Tailscale-User-DisplayName", who.UserProfile.DisplayName)
	if who.UserProfile.ProfilePicURL != "" {
		h.Set("X-Tailscale-User-ProfilePicURL", who.UserProfile.ProfilePicURL)
	}
	if len(who.CapMap) > 0 {
		var caps []string
		for cap := range who.CapMap {
			caps = append(caps, string(cap))
		}
		h.Set("X-Tailscale-Caps", strings.Join(caps, ", "))
	}

	h.Set("X-Tailscale-Node", who.Node.ID.String())
	h.Set("X-Tailscale-Node-Name", who.Node.ComputedName)
	if len(who.Node.CapMap) > 0 {
		var capabilities []string
		for cap := range who.Node.CapMap {
			capabilities = append(capabilities, string(cap))
		}
		h.Set("X-Tailscale-Node-Caps", strings.Join(capabilities, ", "))
	}
	if len(who.Node.Tags) > 0 {
		h.Set("X-Tailscale-Node-Tags", strings.Join(who.Node.Tags, ", "))
	}
	return who
}

// matchPrefixes acts like the http.StripPrefix middleware, except
// that it checks against several allowed prefixes (an empty list
// means that all prefixes are allowed); if no prefixes match, it
// returns 404.
func matchPrefixes(prefixes []prefix, strip bool, forFunnel bool, handler http.Handler) http.Handler {
	if len(prefixes) == 0 {
		return handler
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, prefix := range prefixes {
			if ok, stripData := prefix.matches(r.URL, forFunnel); ok {
				r2 := new(http.Request)
				*r2 = *r
				if strip {
					r2.URL = new(url.URL)
					*r2.URL = *r.URL
					r2.URL.Path = stripData.path
					r2.URL.RawPath = stripData.rawPath
				}
				handler.ServeHTTP(w, r2)
				return
			}
		}
		slog.WarnCtx(r.Context(), "URL prefix not allowed",
			"url", r.URL,
			"prefixes", prefixes,
			"forFunnel", forFunnel,
		)
		http.NotFound(w, r)
	})
}

func (s *ValidTailnetSrv) mux(transport http.RoundTripper, forFunnel bool) http.Handler {
	proxy := &httputil.ReverseProxy{
		Rewrite:        s.rewrite,
		ModifyResponse: s.modifyResponse,
		ErrorHandler:   s.errorHandler,
		Transport:      transport,
	}
	mux := http.NewServeMux()

	mux.Handle("/", matchPrefixes(s.AllowedPrefixes, s.StripPrefix, forFunnel, proxy))

	return mux
}
