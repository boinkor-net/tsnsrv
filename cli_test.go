package tsnsrv

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/exp/slog"
)

func TestFromArgs(t *testing.T) {
	for _, elt := range []struct {
		name string
		args []string
		ok   bool
	}{
		{"basic", []string{"-name", "foo", "http://example.com"}, true},
		{"connect to unix", []string{"-name", "foo", "-upstreamUnixAddr=/tmp/foo.sock", "http://example.com"}, true},
		{"connect to TCP", []string{"-name", "foo", "-upstreamTCPAddr=127.0.0.1:80", "http://example.com"}, true},
		{"funnel", []string{"-name", "foo", "-funnel=true", "http://example.com"}, true},
		{"funnelOnly", []string{"-name", "foo", "-funnel=true", "-funnelOnly", "http://example.com"}, true},
		{"ephemeral", []string{"-name", "foo", "-ephemeral=true", "http://example.com"}, true},

		// Expected to fail:
		{"no args", []string{}, false},
		{"both addrs", []string{"-name", "foo", "-upstreamTCPAddr=127.0.0.1:80", "-upstreamUnixAddr=/tmp/foo.sock", "http://example.com"}, false},
		{"plaintext on funnel", []string{"-name", "foo", "-plaintext=true", "-funnel", "http://example.com"}, false},
		{"invalid funnelOnly", []string{"-name", "foo", "-funnelOnly", "http://example.com"}, false},
		{"invalid destination URL", []string{"-name", "foo", "::--example.com"}, false},
	} {
		test := elt
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			v, _, err := TailnetSrvFromArgs(append([]string{"tsnsrv"}, test.args...))
			if test.ok {
				if err != nil {
					t.Errorf("Unexpected failure to parse %#v: %v", test.args, err)
				}
				if v == nil {
					t.Errorf("Should return a validTsnetSrv")
				}
			}
			if !test.ok && err == nil {
				t.Errorf("Unexpected success parsing %#v", test.args)
			}
		})
	}
}

func TestPrefixServing(t *testing.T) {
	testmux := http.NewServeMux()
	testmux.HandleFunc("/subpath", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("wrong"))
	})
	testmux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
	ts := httptest.NewServer(testmux)
	t.Cleanup(ts.Close)

	s, _, err := TailnetSrvFromArgs([]string{"tsnsrv", "-name", "TestPrefixServing", "-ephemeral",
		"-prefix", "/subpath",
		"-prefix", "/other/subpath",
		"-prefix", "funnel:/funnel-only-subpath",
		"-prefix", "tailnet:/tsnet-only-subpath",
		ts.URL,
	})
	require.NoError(t, err)

	// The routes allowed on the tsnet must succeed for requests on the tailnet:
	mux := s.mux(http.DefaultTransport, false)
	proxy := httptest.NewServer(mux)
	pc := proxy.Client()
	resp404, err := pc.Get(proxy.URL)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, resp404.StatusCode)

	for _, tp := range []string{
		"/subpath",
		"/other/subpath",
		"/tsnet-only-subpath",
	} {
		t.Run(fmt.Sprintf("tailnet:%s", tp), func(t *testing.T) {
			subpath := tp
			t.Parallel()

			// Subpath itself:
			respOk, err := pc.Get(proxy.URL + subpath)
			require.NoError(t, err)
			assert.Equal(t, http.StatusOK, respOk.StatusCode)
			body, err := io.ReadAll(respOk.Body)
			require.NoError(t, err)
			assert.Equal(t, "ok", string(body))

			// Subpaths of subpath:
			respOk, err = pc.Get(proxy.URL + subpath + "/hi")
			require.NoError(t, err)
			assert.Equal(t, http.StatusOK, respOk.StatusCode)
			body, err = io.ReadAll(respOk.Body)
			require.NoError(t, err)
			assert.Equal(t, "ok", string(body))
		})
	}

	// The routes allowed on the funnel must succeed for requests from the funnel:
	mux = s.mux(http.DefaultTransport, true)
	funnelProxy := httptest.NewServer(mux)
	fpc := funnelProxy.Client()
	resp404, err = fpc.Get(funnelProxy.URL)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, resp404.StatusCode)
	for _, tp := range []string{
		"/other/subpath",
		"/funnel-only-subpath",
	} {
		t.Run(fmt.Sprintf("funnel:%s", tp), func(t *testing.T) {
			subpath := tp
			t.Parallel()

			// Subpath itself:
			respOk, err := fpc.Get(funnelProxy.URL + subpath)
			require.NoError(t, err)
			assert.Equal(t, http.StatusOK, respOk.StatusCode)
			body, err := io.ReadAll(respOk.Body)
			require.NoError(t, err)
			assert.Equal(t, "ok", string(body))

			// Subpaths of subpath:
			respOk, err = fpc.Get(funnelProxy.URL + subpath + "/hi")
			require.NoError(t, err)
			assert.Equal(t, http.StatusOK, respOk.StatusCode)
			body, err = io.ReadAll(respOk.Body)
			require.NoError(t, err)
			assert.Equal(t, "ok", string(body))
		})
	}

	// funnel-only routes aren't allowed on the tailnet:
	for _, tp := range []struct {
		path   string
		funnel bool
	}{
		{"/funnel-only-subpath", false},
		{"/tsnet-only-subpath", true},
	} {
		t.Run(fmt.Sprintf("negative:%s funnel:%v", tp.path, tp.funnel), func(t *testing.T) {
			subpath := tp
			t.Parallel()

			// Note: The header value is inverted from above, as we're testing the opposite case.
			var resp *http.Response
			if subpath.funnel {
				resp, err = fpc.Get(funnelProxy.URL + subpath.path)
			} else {
				resp, err = pc.Get(proxy.URL + subpath.path)
			}
			require.NoError(t, err)
			assert.Equal(t, http.StatusNotFound, resp.StatusCode)
		})
	}
}

func TestPrefixDenied(t *testing.T) {
	testmux := http.NewServeMux()
	testmux.HandleFunc("/subpath", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("wrong"))
	})
	testmux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
	ts := httptest.NewServer(testmux)
	t.Cleanup(ts.Close)
	s, _, err := TailnetSrvFromArgs([]string{"tsnsrv", "-name", "TestPrefixDenied", "-ephemeral",
		"-denyPrefix", "/subpath",
		"-denyPrefix", "/other/subpath",
		"-denyPrefix", "funnel:/funnel-only-subpath",
		"-denyPrefix", "tailnet:/tsnet-only-subpath",
		ts.URL,
	})
	require.NoError(t, err)

	// The routes denied on the tsnet must fail for requests on the tailnet:
	mux := s.mux(http.DefaultTransport, false)
	proxy := httptest.NewServer(mux)
	pc := proxy.Client()
	respOk, err := pc.Get(proxy.URL)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, respOk.StatusCode)
	body, err := io.ReadAll(respOk.Body)
	require.NoError(t, err)
	assert.Equal(t, "ok", string(body))
	for _, tp := range []string{
		"/subpath",
		"/other/subpath",
		"/tsnet-only-subpath",
	} {
		t.Run(fmt.Sprintf("tailnet:%s", tp), func(t *testing.T) {
			subpath := tp
			t.Parallel()
			// Subpath itself:
			resp404, err := pc.Get(proxy.URL + subpath)
			require.NoError(t, err)
			assert.Equal(t, http.StatusNotFound, resp404.StatusCode)
			// Subpaths of subpath:
			resp404, err = pc.Get(proxy.URL + subpath + "/hi")
			require.NoError(t, err)
			assert.Equal(t, http.StatusNotFound, resp404.StatusCode)
		})
	}

	// The routes denied on the funnel must fail for requests from the funnel:
	mux = s.mux(http.DefaultTransport, true)
	funnelProxy := httptest.NewServer(mux)
	fpc := funnelProxy.Client()
	respOk, err = fpc.Get(funnelProxy.URL)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, respOk.StatusCode)
	for _, tp := range []string{
		"/other/subpath",
		"/funnel-only-subpath",
	} {
		t.Run(fmt.Sprintf("funnel:%s", tp), func(t *testing.T) {
			subpath := tp
			t.Parallel()
			// Subpath itself:
			resp404, err := fpc.Get(funnelProxy.URL + subpath)
			require.NoError(t, err)
			assert.Equal(t, http.StatusNotFound, resp404.StatusCode)
			// Subpaths of subpath:
			resp404, err = fpc.Get(funnelProxy.URL + subpath + "/hi")
			require.NoError(t, err)
			assert.Equal(t, http.StatusNotFound, resp404.StatusCode)
		})
	}

	// funnel-only routes are denied on the tailnet:
	for _, tp := range []struct {
		path   string
		funnel bool
	}{
		{"/funnel-only-subpath", true},
		{"/tsnet-only-subpath", false},
	} {
		t.Run(fmt.Sprintf("positive:%s funnel:%v", tp.path, tp.funnel), func(t *testing.T) {
			subpath := tp
			t.Parallel()
			var resp *http.Response
			if subpath.funnel {
				resp, err = fpc.Get(funnelProxy.URL + subpath.path)
			} else {
				resp, err = pc.Get(proxy.URL + subpath.path)
			}
			require.NoError(t, err)
			assert.Equal(t, http.StatusNotFound, resp.StatusCode)
			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			assert.Equal(t, "404 page not found\n", string(body))
			// Subpaths of subpath:
			if subpath.funnel {
				resp, err = fpc.Get(funnelProxy.URL + subpath.path + "/hi")
			} else {
				resp, err = pc.Get(proxy.URL + subpath.path + "/hi")
			}
			require.NoError(t, err)
			assert.Equal(t, http.StatusNotFound, resp.StatusCode)
			body, err = io.ReadAll(resp.Body)
			require.NoError(t, err)
			assert.Equal(t, "404 page not found\n", string(body))
		})
	}
}

func TestRouting(t *testing.T) {
	for _, elt := range []struct {
		name, fromPath, toURLPath, requestPath, expectedPath string
		strip                                                bool
	}{
		{"simple", "/", "/", "/", "/", true},
		{"rewriting an exact path", "/api/push-github", "/api/push-github", "/api/push-github", "/api/push-github", true},
		{"rewriting a subpath", "/api", "/api", "/api/push-github", "/api/push-github", true},
		{"rewriting root", "/", "/api/push-github", "/", "/api/push-github", true},

		{"not rewriting subpath", "/_matrix", "/", "/_matrix/client/versions", "/_matrix/client/versions", false},
	} {
		test := elt
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			testmux := http.NewServeMux()
			testmux.HandleFunc("/subpath", func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte("wrong"))
			})
			testmux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				assert.Equal(t, test.expectedPath, r.URL.Path)
			})
			ts := httptest.NewServer(testmux)
			defer ts.Close()

			s, _, err := TailnetSrvFromArgs([]string{"tsnsrv", "-name", "TestRouting", "-ephemeral", "-prefix", test.fromPath, fmt.Sprintf("-stripPrefix=%v", test.strip),
				ts.URL + test.toURLPath,
			})
			require.NoError(t, err)
			mux := s.mux(http.DefaultTransport, false)
			proxy := httptest.NewServer(mux)
			pc := proxy.Client()
			resp, err := pc.Get(proxy.URL + test.requestPath)
			require.NoError(t, err)
			assert.Equal(t, http.StatusOK, resp.StatusCode)
		})
	}
}

func TestHeaderSanitization(t *testing.T) {
	testmux := http.NewServeMux()
	testmux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		for k := range r.Header {
			slog.Info("", "header", k)
			assert.NotRegexp(t, "(?i)^X-Tailscale-", k)
		}
	})
	ts := httptest.NewServer(testmux)
	defer ts.Close()

	s, _, err := TailnetSrvFromArgs([]string{"tsnsrv", "-name", "TestPrefixServing", "-ephemeral",
		ts.URL,
	})
	require.NoError(t, err)
	mux := s.mux(http.DefaultTransport, false)
	proxy := httptest.NewServer(mux)
	pc := proxy.Client()
	req, err := http.NewRequest("GET", proxy.URL, nil)
	require.NoError(t, err)
	req.Header.Set("X-Tailscale-Evil", "true")
	req.Header.Set("x-tailscale-evil", "true")
	req.Header.Set("x-tAILSCALE-LoginName", "fake")
	res, err := pc.Do(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, res.StatusCode)
}

func TestCustomHeaders(t *testing.T) {
	for _, elt := range []struct {
		name, hn, hv string
	}{
		{"custom", "X-Something-Custom", "hi there"},
		{"X-Forwarded-Server", "X-Forwarded-Server", "something-made-up.example.com"},
	} {
		test := elt
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			testmux := http.NewServeMux()
			testmux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				for k := range r.Header {
					slog.Info("", "header", k)
				}
				assert.Equal(t, test.hv, r.Header.Get(test.hn))
			})
			ts := httptest.NewServer(testmux)
			defer ts.Close()

			s, _, err := TailnetSrvFromArgs([]string{"tsnsrv", "-name", "TestPrefixServing",
				"-upstreamHeader", fmt.Sprintf("%v: %v", test.hn, test.hv),
				ts.URL,
			})
			require.NoError(t, err)
			mux := s.mux(http.DefaultTransport, false)
			proxy := httptest.NewServer(mux)
			pc := proxy.Client()
			req, err := http.NewRequest("GET", proxy.URL, nil)
			require.NoError(t, err)
			res, err := pc.Do(req)
			require.NoError(t, err)
			assert.Equal(t, http.StatusOK, res.StatusCode)
		})
	}
}
