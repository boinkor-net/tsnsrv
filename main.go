package main

import (
	"context"
	"flag"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"time"

	"tailscale.com/tsnet"
	"tailscale.com/types/logger"
)

var listenAddr = flag.String("listenAddr", ":443", "Address to listen on; note only :443, :8443 and :10000 are supported with -funnel.")
var servePlaintext = flag.Bool("plaintext", false, "Whether to serve plaintext HTTP")
var funnel = flag.Bool("funnel", false, "Whether to expose a funnel service.")
var funnelOnly = flag.Bool("funnelOnly", false, "Whether to expose a funnel service only (not exposed on the tailnet).")
var downstreamUnixAddr = flag.String("downstreamUnixAddr", "", "Proxy to an HTTP service listening on a UNIX domain socket address")
var downstreamTCPAddr = flag.String("downstreamTCPAddr", "", "Proxy to an HTTP service listening on a TCP address")
var downstreamURL = flag.String("downstreamURL", "", "Prefix proxied requests with this destination URL")
var name = flag.String("name", "", "Name of this service")
var ephemeral = flag.Bool("ephemeral", false, "Declare this service ephemeral")
var timeout = flag.Duration("timeout", 1*time.Minute, "Timeout connecting to the tailnet")

func main() {
	flag.Parse()
	if *name == "" {
		log.Fatal("The service needs a -name.")
	}
	if *servePlaintext && *funnel {
		log.Fatal("Can not serve plaintext on a funnel service.")
	}
	if *downstreamTCPAddr != "" && *downstreamUnixAddr != "" {
		log.Fatal("Can only proxy to one address at a time.")
	}
	if *downstreamTCPAddr == "" && *downstreamUnixAddr == "" {
		log.Fatal("Need exactly one -downstreamUnixAddr or -downstreamTCPAddr arg.")
	}
	if !*funnel && *funnelOnly {
		log.Fatal("-funnel is required if -funnelOnly is set.")
	}
	destURL, err := url.Parse(*downstreamURL)
	if err != nil {
		log.Fatalf("Invalid destination URL %v: %v", *downstreamURL, err)
	}
	if destURL.Path != "/" {
		log.Fatal("Can't handle subpath destination URLs yet")
	}

	srv := &tsnet.Server{
		Hostname:   *name,
		Logf:       logger.Discard,
		Ephemeral:  *ephemeral,
		ControlURL: os.Getenv("TS_URL"),
	}
	ctx := context.Background()
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

	dial := func(ctx context.Context, network, address string) (net.Conn, error) {
		return srv.Dial(ctx, "tcp", *downstreamTCPAddr)
	}
	if *downstreamUnixAddr != "" {
		dial = func(ctx context.Context, network, address string) (net.Conn, error) {
			d := net.Dialer{}
			return d.DialContext(ctx, "unix", *downstreamUnixAddr)
		}
	}
	transport := &http.Transport{DialContext: dial}

	proxy := &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetXForwarded()
			r.Out.URL = destURL.ResolveReference(r.Out.URL)
			log.Printf("Rewrote to: %#v at %v", r.Out, r.Out.URL)
		},
		Transport: transport,
	}
	log.Printf("%s serving on %v, %v (plaintext:%v, funnel:%v, funnelOnly:%v)", *name, status.TailscaleIPs, *listenAddr, *servePlaintext, *funnel, *funnelOnly)
	log.Fatal(http.Serve(l, proxy))
}

func listen(srv *tsnet.Server) (net.Listener, error) {
	if *funnel {
		opts := []tsnet.FunnelOption{}
		if *funnelOnly {
			opts = append(opts, tsnet.FunnelOnly())
		}
		return srv.ListenFunnel("tcp", *listenAddr, opts...)
	} else if *servePlaintext {
		return srv.ListenTLS("tcp", *listenAddr)
	} else {
		return srv.Listen("tcp", *listenAddr)
	}
}
