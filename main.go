package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"time"

	"golang.org/x/exp/slog"

	"github.com/peterbourgon/ff/v3/ffcli"
	"tailscale.com/client/tailscale"
	"tailscale.com/tsnet"
	"tailscale.com/types/logger"
)

type prefixes []string

func (p *prefixes) String() string {
	return strings.Join(*p, ", ")
}

func (p *prefixes) Set(value string) error {
	*p = append(*p, value)
	return nil
}

type TailnetSrv struct {
	DownstreamTCPAddr, DownstreamUnixAddr string
	Ephemeral                             bool
	Funnel, FunnelOnly                    bool
	ListenAddr                            string
	Name                                  string
	RecommendedProxyHeaders               bool
	ServePlaintext                        bool
	Timeout                               time.Duration
	AllowedPrefixes                       prefixes
	StripPrefix                           bool
	StateDir                              string
	AuthkeyPath                           string
	InsecureHTTPS                         bool
	WhoisTimeout                          time.Duration
	SuppressWhois                         bool
}

type validTailnetSrv struct {
	TailnetSrv
	DestURL *url.URL
	client  *tailscale.LocalClient
}

func tailnetSrvFromArgs(args []string) (*validTailnetSrv, *ffcli.Command, error) {
	s := &TailnetSrv{}
	var fs = flag.NewFlagSet("tsnsrv", flag.ExitOnError)
	fs.StringVar(&s.DownstreamTCPAddr, "downstreamTCPAddr", "", "Proxy to an HTTP service listening on this TCP address")
	fs.StringVar(&s.DownstreamUnixAddr, "downstreamUnixAddr", "", "Proxy to an HTTP service listening on this UNIX domain socket address")
	fs.BoolVar(&s.Ephemeral, "ephemeral", false, "Declare this service ephemeral")
	fs.BoolVar(&s.Funnel, "funnel", false, "Expose a funnel service.")
	fs.BoolVar(&s.FunnelOnly, "funnelOnly", false, "Expose a funnel service only (not exposed on the tailnet).")
	fs.StringVar(&s.ListenAddr, "listenAddr", ":443", "Address to listen on; note only :443, :8443 and :10000 are supported with -funnel.")
	fs.StringVar(&s.Name, "name", "", "Name of this service")
	fs.BoolVar(&s.RecommendedProxyHeaders, "recommendedProxyHeaders", true, "Set Host, X-Scheme, X-Real-Ip, X-Forwarded-{Proto,Server,Port} headers.")
	fs.BoolVar(&s.ServePlaintext, "plaintext", false, "Serve plaintext HTTP without TLS")
	fs.DurationVar(&s.Timeout, "timeout", 1*time.Minute, "Timeout connecting to the tailnet")
	fs.Var(&s.AllowedPrefixes, "prefix", "Allowed URL prefixes; if none is set, all prefixes are allowed")
	fs.BoolVar(&s.StripPrefix, "stripPrefix", true, "Strip prefixes that matched; best set to false if allowing multiple prefixes")
	fs.StringVar(&s.StateDir, "stateDir", "", "Directory containing the persistent tailscale status files.")
	fs.StringVar(&s.AuthkeyPath, "authkeyPath", "", "File containing a tailscale auth key. Key is assumed to be in $TS_AUTHKEY in absence of this option.")
	fs.BoolVar(&s.InsecureHTTPS, "insecureHTTPS", false, "Disable TLS certificate validation on upstream")
	fs.DurationVar(&s.WhoisTimeout, "whoisTimeout", 1*time.Second, "Maximum amount of time to spend looking up client identities")
	fs.BoolVar(&s.SuppressWhois, "suppressWhois", false, "Do not set X-Tailscale-User-* headers in upstream requests")

	root := &ffcli.Command{
		ShortUsage: "tsnsrv -name <serviceName> [flags] <toURL>",
		FlagSet:    fs,
		Exec:       func(context.Context, []string) error { return nil },
	}
	if err := root.Parse(args); err != nil {
		return nil, root, err
	}
	valid, err := s.validate(root.FlagSet.Args())
	if err != nil {
		return nil, root, err
	}
	return valid, root, nil
}

func (s *TailnetSrv) validate(args []string) (*validTailnetSrv, error) {
	var errs []error
	if s.Name == "" {
		errs = append(errs, errors.New("tsnsrv needs a -name."))
	}
	if s.ServePlaintext && s.Funnel {
		errs = append(errs, errors.New("can not serve plaintext on a funnel service."))
	}
	if s.DownstreamTCPAddr != "" && s.DownstreamUnixAddr != "" {
		errs = append(errs, errors.New("can only proxy to one address at a time, pass either -downstreamUnixAddr or -downstreamTCPAddr"))
	}
	if !s.Funnel && s.FunnelOnly {
		errs = append(errs, errors.New("-funnel is required if -funnelOnly is set."))
	}

	if len(args) != 1 {
		errs = append(errs, errors.New("tsnsrv requires a destination URL."))
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}

	destURL, err := url.Parse(args[0])
	if err != nil {
		return nil, fmt.Errorf("invalid destination URL %#v: %w", args[0], err)
	}

	valid := validTailnetSrv{TailnetSrv: *s, DestURL: destURL}
	return &valid, nil
}

func authkeyFromFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	key, err := io.ReadAll(f)
	return string(key), err
}

func (s *validTailnetSrv) run(ctx context.Context) error {
	srv := &tsnet.Server{
		Hostname:   s.Name,
		Dir:        s.StateDir,
		Logf:       logger.Discard,
		Ephemeral:  s.Ephemeral,
		ControlURL: os.Getenv("TS_URL"),
	}
	if s.AuthkeyPath != "" {
		var err error
		srv.AuthKey, err = authkeyFromFile(s.AuthkeyPath)
		if err != nil {
			slog.Warn("Could not read authkey from file",
				"path", s.AuthkeyPath,
				"error", err)
		}
	}
	ctx, cancel := context.WithTimeout(ctx, s.Timeout)
	defer cancel()
	status, err := srv.Up(ctx)
	if err != nil {
		return fmt.Errorf("could not connect to tailnet: %w", err)
	}
	s.client, err = srv.LocalClient()
	if err != nil {
		slog.Warn("could not get a local tailscale client. Whois headers will not work.",
			"error", err,
		)
	}
	l, err := s.listen(srv)
	if err != nil {
		return fmt.Errorf("could not listen: %w", err)
	}

	dial := srv.Dial
	if s.DownstreamTCPAddr != "" {
		dial = func(ctx context.Context, network, address string) (net.Conn, error) {
			return srv.Dial(ctx, "tcp", s.DownstreamTCPAddr)
		}
	} else if s.DownstreamUnixAddr != "" {
		dial = func(ctx context.Context, network, address string) (net.Conn, error) {
			d := net.Dialer{}
			return d.DialContext(ctx, "unix", s.DownstreamUnixAddr)
		}
	}
	transport := &http.Transport{DialContext: dial}
	if s.InsecureHTTPS {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	mux := s.mux(transport)
	if err != nil {
		return err
	}
	slog.Info("Serving",
		"name", s.Name,
		"tailscaleIPs", status.TailscaleIPs,
		"listenAddr", s.ListenAddr,
		"prexifes", s.AllowedPrefixes,
		"destURL", s.DestURL,
		"plaintext", s.ServePlaintext,
		"funnel", s.Funnel,
		"funnelOnly", s.FunnelOnly,
	)
	return fmt.Errorf("while serving: %w", http.Serve(l, mux))
}

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
		"whois_login_name", r.Out.Header.Get("X-Tailscale-User-LoginName"),
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

func (s *TailnetSrv) listen(srv *tsnet.Server) (net.Listener, error) {
	if s.Funnel {
		opts := []tsnet.FunnelOption{}
		if s.FunnelOnly {
			opts = append(opts, tsnet.FunnelOnly())
		}
		return srv.ListenFunnel("tcp", s.ListenAddr, opts...)
	} else if !s.ServePlaintext {
		return srv.ListenTLS("tcp", s.ListenAddr)
	} else {
		return srv.Listen("tcp", s.ListenAddr)
	}
}

func main() {
	s, cmd, err := tailnetSrvFromArgs(os.Args[1:])
	if err != nil {
		log.Fatalf("Invalid CLI usage. Errors:\n%v\n\n%v", err, ffcli.DefaultUsageFunc(cmd))
	}
	if err := s.run(context.Background()); err != nil {
		log.Fatal(err)
	}
}
