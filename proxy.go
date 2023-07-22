package main

import (
	"context"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"golang.org/x/exp/slog"
)

func (s *validTailnetSrv) rewrite(r *httputil.ProxyRequest) {
	r.SetURL(s.DestURL)
	if r.In.URL.Path == "" {
		r.Out.URL.Path = s.DestURL.Path
	}

	// Set known proxy headers:
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
	s.setWhoisHeaders(r)
	slog.Info("rewrote request",
		"original", r.In.URL,
		"rewritten", r.Out.URL,
		"destURL", s.DestURL,
		"origin_login", r.Out.Header.Get("X-Tailscale-User-LoginName"),
		"origin_node", r.Out.Header.Get("X-Tailscale-Node-Name"),
	)
}

// Clean up and set user/node identity headers:
func (s *validTailnetSrv) setWhoisHeaders(r *httputil.ProxyRequest) {
	// First, clean out any input we received that looks like TS setting headers:
	for k := range r.Out.Header {
		if strings.HasPrefix(k, "X-Tailscale-") {
			r.Out.Header.Del(k)
		}
	}
	if s.SuppressWhois || s.client == nil {
		return
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
	if len(who.Caps) > 0 {
		h.Set("X-Tailscale-Caps", strings.Join(who.Caps, ", "))
	}

	h.Set("X-Tailscale-Node", who.Node.ID.String())
	h.Set("X-Tailscale-Node-Name", who.Node.ComputedName)
	if len(who.Node.Capabilities) > 0 {
		h.Set("X-Tailscale-Node-Caps", strings.Join(who.Node.Capabilities, ", "))
	}
	if len(who.Node.Tags) > 0 {
		h.Set("X-Tailscale-Node-Tags", strings.Join(who.Node.Tags, ", "))
	}
	return
}

// matchPrefixes acts like the http.StripPrefix middleware, except
// that it checks against several allowed prefixes (an empty list
// means that all prefixes are allowed); if no prefixes match, it
// returns 404.
func matchPrefixes(prefixes []string, strip bool, handler http.Handler) http.Handler {
	if len(prefixes) == 0 {
		return handler
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, prefix := range prefixes {
			p := strings.TrimPrefix(r.URL.Path, prefix)
			rp := strings.TrimPrefix(r.URL.RawPath, prefix)
			if len(p) < len(r.URL.Path) && (r.URL.RawPath == "" || len(rp) < len(r.URL.RawPath)) {
				r2 := new(http.Request)
				*r2 = *r
				if strip {
					r2.URL = new(url.URL)
					*r2.URL = *r.URL
					r2.URL.Path = p
					r2.URL.RawPath = rp
				}
				handler.ServeHTTP(w, r2)
				return
			}
		}
		slog.WarnCtx(r.Context(), "URL prefix not allowed",
			"url", r.URL,
			"prefixes", prefixes,
		)
		http.NotFound(w, r)
	})
}

func (s *validTailnetSrv) mux(transport http.RoundTripper) http.Handler {
	proxy := &httputil.ReverseProxy{
		Rewrite:   s.rewrite,
		Transport: transport,
	}
	mux := http.NewServeMux()
	mux.Handle("/", matchPrefixes(s.AllowedPrefixes, s.StripPrefix, proxy))
	return mux
}
