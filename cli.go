package tsnsrv

import (
	"cmp"
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/peterbourgon/ff/v3/ffcli"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/exp/slog"
	"golang.org/x/oauth2/clientcredentials"
	"tailscale.com/client/tailscale"
	"tailscale.com/tsnet"
	"tailscale.com/types/logger"
)

type prefixMatch int

const (
	matchEither prefixMatch = iota
	matchFunnelOnly
	matchTsnetOnly
)

type prefix struct {
	path    string
	matchIf prefixMatch
}

type strippedPrefixes struct {
	path    string
	rawPath string
}

// matches returns whether an allowlist entry matches a request's URL and circumstance.
func (pref *prefix) matches(reqURL *url.URL, isFunnel bool) (bool, strippedPrefixes) {
	if isFunnel && pref.matchIf == matchTsnetOnly {
		return false, strippedPrefixes{}
	}
	if !isFunnel && pref.matchIf == matchFunnelOnly {
		return false, strippedPrefixes{}
	}

	p := strings.TrimPrefix(reqURL.Path, pref.path)
	rp := strings.TrimPrefix(reqURL.RawPath, pref.path)
	return len(p) < len(reqURL.Path) && (reqURL.RawPath == "" || len(rp) < len(reqURL.RawPath)), strippedPrefixes{p, rp}
}

type prefixes []prefix

func (p *prefixes) String() string {
	seq := func(yield func(string) bool) {
		for _, pref := range *p {
			var p string
			switch pref.matchIf {
			case matchEither:
				p = pref.path
			case matchFunnelOnly:
				p = fmt.Sprintf("funnel:%s", pref.path)
			case matchTsnetOnly:
				p = fmt.Sprintf("tailnet:%s", pref.path)
			}
			if !yield(p) {
				return
			}
		}
	}
	serialized := slices.Collect(seq)
	return strings.Join(serialized, ", ")
}

func (p *prefixes) Set(value string) error {
	var pref prefix
	switch {
	case strings.HasPrefix(value, "tailnet:"):
		pref.matchIf = matchTsnetOnly
		pref.path = strings.TrimPrefix(value, "tailnet:")
	case strings.HasPrefix(value, "funnel:"):
		pref.matchIf = matchFunnelOnly
		pref.path = strings.TrimPrefix(value, "funnel:")
	default:
		pref.path = value
	}

	*p = append(*p, pref)
	return nil
}

type headers http.Header

func (h *headers) String() string {
	var coll []string
	for name, vals := range *h {
		for _, val := range vals {
			coll = append(coll, fmt.Sprintf("%s: %s", name, val))
		}
	}
	return strings.Join(coll, ", ")
}

var errHeaderFormat = errors.New("header format must be 'Header-Name: value'")

func (h *headers) Set(value string) error {
	name, val, ok := strings.Cut(value, ": ")
	if !ok {
		return fmt.Errorf("%w: Invalid header format %#v", errHeaderFormat, value)
	}
	if *h == nil {
		*h = headers{}
	}
	http.Header((*h)).Add(name, val)
	return nil
}

type tags []string

func (t *tags) String() string {
	return strings.Join(*t, ", ")
}

var errTagFormat = errors.New("tags must start with 'tag:'")

func (t *tags) Set(value string) error {
	if !strings.HasPrefix(value, "tag:") {
		return errTagFormat
	}
	*t = append(*t, value)
	return nil
}

type TailnetSrv struct {
	UpstreamTCPAddr, UpstreamUnixAddr string
	Ephemeral                         bool
	Funnel, FunnelOnly                bool
	ListenAddr                        string
	certificateFile                   string
	keyFile                           string
	Name                              string
	RecommendedProxyHeaders           bool
	ServePlaintext                    bool
	Timeout                           time.Duration
	AllowedPrefixes                   prefixes
	StripPrefix                       bool
	StateDir                          string
	AuthkeyPath                       string
	Tags                              tags
	InsecureHTTPS                     bool
	WhoisTimeout                      time.Duration
	SuppressWhois                     bool
	PrometheusAddr                    string
	UpstreamHeaders                   headers
	SuppressTailnetDialer             bool
	ReadHeaderTimeout                 time.Duration
	TsnetVerbose                      bool
	UpstreamAllowInsecureCiphers      bool
}

// ValidTailnetSrv is a TailnetSrv that has been constructed from validated CLI arguments.
//
// Use TailnetSrvFromArgs to get an instance of it.
type ValidTailnetSrv struct {
	TailnetSrv
	DestURL *url.URL
	client  *tailscale.LocalClient
}

// TailnetSrvFromArgs constructs a validated tailnet service from commandline arguments.
func TailnetSrvFromArgs(args []string) (*ValidTailnetSrv, *ffcli.Command, error) {
	s := &TailnetSrv{}
	fs := flag.NewFlagSet("tsnsrv", flag.ExitOnError)
	fs.StringVar(&s.UpstreamTCPAddr, "upstreamTCPAddr", "", "Proxy to an HTTP service listening on this TCP address")
	fs.StringVar(&s.UpstreamUnixAddr, "upstreamUnixAddr", "", "Proxy to an HTTP service listening on this UNIX domain socket address")
	fs.BoolVar(&s.Ephemeral, "ephemeral", false, "Declare this service ephemeral")
	fs.BoolVar(&s.Funnel, "funnel", false, "Expose a funnel service.")
	fs.BoolVar(&s.FunnelOnly, "funnelOnly", false, "Expose a funnel service only (not exposed on the tailnet).")
	fs.StringVar(&s.ListenAddr, "listenAddr", ":443", "Address to listen on; note only :443, :8443 and :10000 are supported with -funnel.")
	fs.StringVar(&s.certificateFile, "certificateFile", "", "Custom certificate file to use for TLS listening instead of Tailscale's builtin way.")
	fs.StringVar(&s.keyFile, "keyFile", "", "Custom key file to use for TLS listening instead of Tailscale's builtin way.")
	fs.StringVar(&s.Name, "name", "", "Name of this service")
	fs.BoolVar(&s.RecommendedProxyHeaders, "recommendedProxyHeaders", true, "Set Host, X-Scheme, X-Real-Ip, X-Forwarded-{Proto,Server,Port} headers.")
	fs.BoolVar(&s.ServePlaintext, "plaintext", false, "Serve plaintext HTTP without TLS")
	fs.DurationVar(&s.Timeout, "timeout", 1*time.Minute, "Timeout connecting to the tailnet")
	fs.Var(&s.AllowedPrefixes, "prefix", "Allowed URL prefixes; if none is set, all prefixes are allowed")
	fs.BoolVar(&s.StripPrefix, "stripPrefix", true, "Strip prefixes that matched; best set to false if allowing multiple prefixes")
	fs.StringVar(&s.StateDir, "stateDir", os.Getenv("TS_STATE_DIR"), "Directory containing the persistent tailscale status files. Can also be set by $TS_STATE_DIR; this option takes precedence.")
	fs.StringVar(&s.AuthkeyPath, "authkeyPath", "", "File containing a tailscale auth key. Key is assumed to be in $TS_AUTHKEY in absence of this option.")
	fs.Var(&s.Tags, "tag", "Tags to advertise to tailscale. Mandatory if using OAuth clients.")
	fs.BoolVar(&s.InsecureHTTPS, "insecureHTTPS", false, "Disable TLS certificate validation on upstream")
	fs.DurationVar(&s.WhoisTimeout, "whoisTimeout", 1*time.Second, "Maximum amount of time to spend looking up client identities")
	fs.BoolVar(&s.SuppressWhois, "suppressWhois", false, "Do not set X-Tailscale-User-* headers in upstream requests")
	fs.StringVar(&s.PrometheusAddr, "prometheusAddr", ":9099", "Serve prometheus metrics from this address. Empty string to disable.")
	fs.Var(&s.UpstreamHeaders, "upstreamHeader", "Additional headers (separated by ': ') on requests to upstream.")
	fs.BoolVar(&s.SuppressTailnetDialer, "suppressTailnetDialer", false, "Whether to use the stdlib net.Dialer instead of a tailnet-enabled one")
	fs.DurationVar(&s.ReadHeaderTimeout, "readHeaderTimeout", 0, "Amount of time to allow for reading HTTP request headers. 0 will disable the timeout but expose the service to the slowloris attack.")
	fs.BoolVar(&s.TsnetVerbose, "tsnetVerbose", false, "Whether to output tsnet logs.")
	fs.BoolVar(&s.UpstreamAllowInsecureCiphers, "upstreamAllowInsecureCiphers", false, "Don't require Perfect Forward Secrecy from the upstream https server.")

	root := &ffcli.Command{
		ShortUsage: fmt.Sprintf("%s -name <serviceName> [flags] <toURL>", path.Base(args[0])),
		FlagSet:    fs,
		Exec:       func(context.Context, []string) error { return nil },
	}
	if err := root.Parse(args[1:]); err != nil {
		return nil, root, fmt.Errorf("could not parse args: %w", err)
	}
	valid, err := s.validate(root.FlagSet.Args())
	if err != nil {
		return nil, root, fmt.Errorf("failed to validate args: %w", err)
	}
	return valid, root, nil
}

var errNameRequired = errors.New("tsnsrv needs a -name")
var errNoPlaintextOnFunnel = errors.New("can not serve plaintext on a funnel service")
var errBothCertificateFileKeyFile = errors.New("when providing either a certificate or key file, the other must be provided")
var errNoPlaintextWithCustomCert = errors.New("can not serve plaintext when using custom certificate and key")
var errOnlyOneAddrType = errors.New("can only proxy to one address at a time, pass either -upstreamUnixAddr or -upstreamTCPAddr")
var errFunnelRequired = errors.New("-funnel is required if -funnelOnly is set")
var errNoDestURL = errors.New("tsnsrv requires a destination URL")

func (s *TailnetSrv) validate(args []string) (*ValidTailnetSrv, error) {
	var errs []error
	if s.Name == "" {
		errs = append(errs, errNameRequired)
	}
	if s.ServePlaintext && s.Funnel {
		errs = append(errs, errNoPlaintextOnFunnel)
	}
	if s.certificateFile != "" && s.keyFile == "" || s.certificateFile == "" && s.keyFile != "" {
		errs = append(errs, errBothCertificateFileKeyFile)
	}
	if s.ServePlaintext && s.certificateFile != "" && s.keyFile != "" {
		errs = append(errs, errNoPlaintextWithCustomCert)
	}
	if s.UpstreamTCPAddr != "" && s.UpstreamUnixAddr != "" {
		errs = append(errs, errOnlyOneAddrType)
	}
	if !s.Funnel && s.FunnelOnly {
		errs = append(errs, errFunnelRequired)
	}

	if len(args) != 1 {
		return nil, errors.Join(append(errs, errNoDestURL)...)
	}
	destURL, err := url.Parse(args[0])
	if err != nil {
		errs = append(errs, fmt.Errorf("invalid destination URL %#v: %w", args[0], err))
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}

	valid := ValidTailnetSrv{TailnetSrv: *s, DestURL: destURL}
	return &valid, nil
}

func (s *ValidTailnetSrv) authkeyFromFile(ctx context.Context, path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("opening authkey %#v: %w", path, err)
	}
	defer f.Close()
	key, err := io.ReadAll(f)
	if err != nil {
		return "", fmt.Errorf("reading authkey %#v: %w", path, err)
	}
	trimmed := strings.TrimSpace(string(key))
	if strings.HasPrefix(trimmed, "tskey-client-") {
		return s.mintAuthKey(ctx, trimmed)
	}
	return trimmed, nil
}

func (s *ValidTailnetSrv) mintAuthKey(ctx context.Context, authkey string) (string, error) {
	tailscale.I_Acknowledge_This_API_Is_Unstable = true // needed in order to use API clients.

	baseURL := cmp.Or(os.Getenv("TS_BASE_URL"), "https://api.tailscale.com")
	tsClient := tailscale.NewClient("-", nil)
	tsClient.BaseURL = baseURL
	credentials := clientcredentials.Config{
		ClientID:     "some-client-id", // ignored
		ClientSecret: authkey,
		TokenURL:     tsClient.BaseURL + "/api/v2/oauth/token",
	}

	tsClient.HTTPClient = credentials.Client(ctx)
	caps := tailscale.KeyCapabilities{
		Devices: tailscale.KeyDeviceCapabilities{
			Create: tailscale.KeyDeviceCreateCapabilities{
				Reusable:  false,
				Tags:      s.Tags,
				Ephemeral: s.Ephemeral,
			},
		},
	}

	authkey, _, err := tsClient.CreateKey(ctx, caps)
	if err != nil {
		return "", fmt.Errorf("minting a tailscale pre-authenticated key for tags %v: %w", s.Tags, err)
	}
	return authkey, nil
}

func (s *ValidTailnetSrv) Run(ctx context.Context) error {
	srv := &tsnet.Server{
		Hostname:   s.Name,
		Dir:        s.StateDir,
		Logf:       logger.Discard,
		Ephemeral:  s.Ephemeral,
		ControlURL: os.Getenv("TS_URL"),
	}
	if s.TsnetVerbose {
		slog.SetDefault(slog.Default())
		srv.Logf = log.Printf
	}
	if s.AuthkeyPath != "" {
		var err error
		srv.AuthKey, err = s.authkeyFromFile(ctx, s.AuthkeyPath)
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
		if slices.ContainsFunc(s.AllowedPrefixes, func(p prefix) bool { return p.matchIf != matchEither }) {
			return fmt.Errorf("-prefix rules with a provenance (tsnet: or funnel:) require that a local tailscale client is available: %w", err)
		}
		slog.Warn("could not get a local tailscale client. Whois headers will not work.",
			"error", err,
		)
	}
	l, err := s.listen(srv)
	if err != nil {
		return fmt.Errorf("could not listen: %w", err)
	}

	dial := srv.Dial
	if s.SuppressTailnetDialer {
		d := net.Dialer{}
		dial = d.DialContext
	}
	if s.UpstreamTCPAddr != "" {
		dialOrig := dial
		dial = func(ctx context.Context, _, _ string) (net.Conn, error) {
			conn, err := dialOrig(ctx, "tcp", s.UpstreamTCPAddr)
			if err != nil {
				return nil, fmt.Errorf("connecting to tcp %v: %w", s.UpstreamTCPAddr, err)
			}
			return conn, nil
		}
	} else if s.UpstreamUnixAddr != "" {
		dial = func(ctx context.Context, _, _ string) (net.Conn, error) {
			d := net.Dialer{}
			conn, err := d.DialContext(ctx, "unix", s.UpstreamUnixAddr)
			if err != nil {
				return nil, fmt.Errorf("connecting to unix %v: %w", s.UpstreamUnixAddr, err)
			}
			return conn, nil
		}
	}
	transport := &http.Transport{DialContext: dial}
	transport.TLSClientConfig = &tls.Config{
		MinVersion: tls.VersionTLS12,
	}
	if s.InsecureHTTPS {
		transport.TLSClientConfig.InsecureSkipVerify = true // #nosec This is explicitly requested by the user
	}
	if s.UpstreamAllowInsecureCiphers {
		for _, suite := range append(tls.CipherSuites(), tls.InsecureCipherSuites()...) {
			transport.TLSClientConfig.CipherSuites = append(transport.TLSClientConfig.CipherSuites, suite.ID)
		}
	}
	mux := s.mux(transport, nil)

	err = s.setupPrometheus(srv)
	if err != nil {
		slog.Error("Could not setup prometheus listener", "error", err)
	}

	slog.Info("Serving",
		"name", s.Name,
		"tailscaleIPs", status.TailscaleIPs,
		"listenAddr", s.ListenAddr,
		"tags", s.Tags,
		"prefixes", s.AllowedPrefixes,
		"destURL", s.DestURL,
		"plaintext", s.ServePlaintext,
		"funnel", s.Funnel,
		"funnelOnly", s.FunnelOnly,
	)
	server := http.Server{
		Handler:           mux,
		ReadHeaderTimeout: s.ReadHeaderTimeout,
	}

	if !s.ServePlaintext && (s.certificateFile != "" || s.keyFile != "") {
		return fmt.Errorf("while serving: %w", server.ServeTLS(l, s.certificateFile, s.keyFile))
	}

	return fmt.Errorf("while serving: %w", server.Serve(l))
}

func (s *TailnetSrv) listen(srv *tsnet.Server) (net.Listener, error) {
	var l net.Listener
	var err error
	switch {
	case s.Funnel:
		opts := []tsnet.FunnelOption{}
		if s.FunnelOnly {
			opts = append(opts, tsnet.FunnelOnly())
		}
		l, err = srv.ListenFunnel("tcp", s.ListenAddr, opts...)
	case !s.ServePlaintext && (s.certificateFile == "" || s.keyFile == ""):
		l, err = srv.ListenTLS("tcp", s.ListenAddr)
	default:
		l, err = srv.Listen("tcp", s.ListenAddr)
	}
	if err != nil {
		return nil, fmt.Errorf("listener for %v: %w", srv, err)
	}
	return l, nil
}

func (s *ValidTailnetSrv) setupPrometheus(srv *tsnet.Server) error {
	if s.PrometheusAddr == "" {
		return nil
	}
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	listener, err := srv.Listen("tcp", s.PrometheusAddr)
	if err != nil {
		return fmt.Errorf("could not listen on prometheus address %v: %w", s.PrometheusAddr, err)
	}
	go func() {
		server := http.Server{
			Handler:           mux,
			ReadHeaderTimeout: 1 * time.Second,
		}
		slog.Error("failed to listen on prometheus address", "error", server.Serve(listener))
		os.Exit(20)
	}()
	return nil
}
