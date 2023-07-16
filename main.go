package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"time"

	"github.com/peterbourgon/ff/v3/ffcli"
	"tailscale.com/tsnet"
	"tailscale.com/types/logger"
)

var fs = flag.NewFlagSet("tsnsrv", flag.ExitOnError)
var downstreamTCPAddr = fs.String("downstreamTCPAddr", "", "Proxy to an HTTP service listening on a TCP address")
var downstreamUnixAddr = fs.String("downstreamUnixAddr", "", "Proxy to an HTTP service listening on a UNIX domain socket address")
var ephemeral = fs.Bool("ephemeral", false, "Declare this service ephemeral")
var funnel = fs.Bool("funnel", false, "Whether to expose a funnel service.")
var funnelOnly = fs.Bool("funnelOnly", false, "Whether to expose a funnel service only (not exposed on the tailnet).")
var listenAddr = fs.String("listenAddr", ":443", "Address to listen on; note only :443, :8443 and :10000 are supported with -funnel.")
var name = fs.String("name", "", "Name of this service")
var servePlaintext = fs.Bool("plaintext", false, "Whether to serve plaintext HTTP")
var timeout = fs.Duration("timeout", 1*time.Minute, "Timeout connecting to the tailnet")

func main() {
	root := ffcli.Command{
		ShortUsage: "tsnsrv -name <serviceName> [args] <fromPath> <toURL>",
		FlagSet:    fs,
		Exec: func(ctx context.Context, args []string) error {
			err := validateFlags()
			if err != nil {
				return err
			}
			return run(ctx, args)
		},
	}
	if err := root.ParseAndRun(context.Background(), os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) != 2 {
		return errors.New("tsnsrv requires a source path and a destination URL.")
	}
	sourcePath := args[0]
	destURL, err := url.Parse(args[1])
	if err != nil {
		log.Fatalf("Invalid destination URL %#v: %v", args[1], err)
	}

	srv := &tsnet.Server{
		Hostname:   *name,
		Logf:       logger.Discard,
		Ephemeral:  *ephemeral,
		ControlURL: os.Getenv("TS_URL"),
	}
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	status, err := srv.Up(ctx)
	if err != nil {
		log.Fatalf("Could not connect to tailnet: %v", err)
	}

	l, err := listen(srv)
	if err != nil {
		log.Fatalf("Could not listen: %v", err)
	}

	dial := srv.Dial
	if *downstreamTCPAddr != "" {
		dial = func(ctx context.Context, network, address string) (net.Conn, error) {
			return srv.Dial(ctx, "tcp", *downstreamTCPAddr)
		}
	} else if *downstreamUnixAddr != "" {
		dial = func(ctx context.Context, network, address string) (net.Conn, error) {
			d := net.Dialer{}
			return d.DialContext(ctx, "unix", *downstreamUnixAddr)
		}
	}

	proxy := &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetXForwarded()
			r.SetURL(destURL)
			log.Printf("Rewrote %v with %v to %v", r.In.URL, destURL, r.Out.URL)
		},
		Transport: &http.Transport{DialContext: dial},
	}
	mux := http.NewServeMux()
	var handler http.Handler = proxy
	if sourcePath != "/" {
		handler = http.StripPrefix(sourcePath, proxy)
	}
	mux.Handle(sourcePath, handler)
	log.Printf("%s serving on %v, %v%v (plaintext:%v, funnel:%v, funnelOnly:%v)",
		*name, status.TailscaleIPs, *listenAddr, sourcePath, *servePlaintext, *funnel, *funnelOnly)
	return http.Serve(l, mux)
}

func validateFlags() error {
	if *name == "" {
		return errors.New("The service needs a -name.")
	}
	if *servePlaintext && *funnel {
		return errors.New("Can not serve plaintext on a funnel service.")
	}
	if *downstreamTCPAddr != "" && *downstreamUnixAddr != "" {
		return errors.New("Can only proxy to one address at a time.")
	}
	if !*funnel && *funnelOnly {
		return errors.New("-funnel is required if -funnelOnly is set.")
	}
	return nil
}

func listen(srv *tsnet.Server) (net.Listener, error) {
	if *funnel {
		opts := []tsnet.FunnelOption{}
		if *funnelOnly {
			opts = append(opts, tsnet.FunnelOnly())
		}
		return srv.ListenFunnel("tcp", *listenAddr, opts...)
	} else if !*servePlaintext {
		return srv.ListenTLS("tcp", *listenAddr)
	} else {
		return srv.Listen("tcp", *listenAddr)
	}
}
