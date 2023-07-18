package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"time"

	"golang.org/x/exp/slog"

	"github.com/peterbourgon/ff/v3/ffcli"
	"tailscale.com/tsnet"
	"tailscale.com/types/logger"
)

type TailnetSrv struct {
	DownstreamTCPAddr, DownstreamUnixAddr string
	Ephemeral                             bool
	Funnel, FunnelOnly                    bool
	ListenAddr                            string
	Name                                  string
	RecommendedProxyHeaders               bool
	ServePlaintext                        bool
	Timeout                               time.Duration
}

type validTailnetSrv struct {
	TailnetSrv
	SourcePath string
	DestURL    *url.URL
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

	root := &ffcli.Command{
		ShortUsage: "tsnsrv -name <serviceName> [flags] <fromPath> <toURL>",
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

	if len(args) != 2 {
		errs = append(errs, errors.New("tsnsrv requires a source path and a destination URL."))
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	sourcePath := args[0]
	if sourcePath == "" {
		sourcePath = "/"
	}

	destURL, err := url.Parse(args[1])
	if err != nil {
		return nil, fmt.Errorf("invalid destination URL %#v: %w", args[1], err)
	}

	valid := validTailnetSrv{TailnetSrv: *s, DestURL: destURL, SourcePath: sourcePath}
	return &valid, nil
}

func (s *validTailnetSrv) run(ctx context.Context) error {
	srv := &tsnet.Server{
		Hostname:   s.Name,
		Logf:       logger.Discard,
		Ephemeral:  s.Ephemeral,
		ControlURL: os.Getenv("TS_URL"),
	}
	ctx, cancel := context.WithTimeout(ctx, s.Timeout)
	defer cancel()
	status, err := srv.Up(ctx)
	if err != nil {
		return fmt.Errorf("could not connect to tailnet: %w", err)
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
	mux := s.mux(&http.Transport{DialContext: dial})
	if err != nil {
		return err
	}
	slog.Info("Serving",
		"name", s.Name,
		"tailscaleIPs", status.TailscaleIPs,
		"listenAddr", s.ListenAddr,
		"sourcePath", s.SourcePath,
		"destURL", s.DestURL,
		"plaintext", s.ServePlaintext,
		"funnel", s.Funnel,
		"funnelOnly", s.FunnelOnly,
	)
	return fmt.Errorf("while serving: %w", http.Serve(l, mux))
}

func (s *validTailnetSrv) rewrite(r *httputil.ProxyRequest) {
	r.SetXForwarded()
	r.SetURL(s.DestURL)
	// Prevent excessive final `/` being appended to the URL (thanks, StripPrefix):
	if r.In.URL.Path == "" {
		r.Out.URL.Path = s.DestURL.Path
	}
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
	slog.Info("rewrote request",
		"original", r.In.URL,
		"rewritten", r.Out.URL,
		"destURL", s.DestURL,
	)
}

func (s *validTailnetSrv) mux(transport http.RoundTripper) *http.ServeMux {
	proxy := &httputil.ReverseProxy{
		Rewrite:   s.rewrite,
		Transport: transport,
	}
	mux := http.NewServeMux()
	var handler http.Handler = proxy
	if s.SourcePath != "/" {
		handler = http.StripPrefix(s.SourcePath, proxy)
	}
	mux.Handle("/", handler)
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
