package main

import (
	"context"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFromArgs(t *testing.T) {
	for _, elt := range []struct {
		name string
		args []string
		ok   bool
	}{
		{"basic", []string{"-name", "foo", "/", "http://example.com"}, true},
		{"connect to unix", []string{"-name", "foo", "-downstreamUnixAddr=/tmp/foo.sock", "/", "http://example.com"}, true},
		{"connect to TCP", []string{"-name", "foo", "-downstreamTCPAddr=127.0.0.1:80", "/", "http://example.com"}, true},
		{"funnel", []string{"-name", "foo", "-funnel=true", "/", "http://example.com"}, true},
		{"funnelOnly", []string{"-name", "foo", "-funnel=true", "-funnelOnly", "/", "http://example.com"}, true},
		{"ephemeral", []string{"-name", "foo", "-ephemeral=true", "/", "http://example.com"}, true},

		// Expected to fail:
		{"no args", []string{}, false},
		{"both addrs", []string{"-name", "foo", "-downstreamTCPAddr=127.0.0.1:80", "-downstreamUnixAddr=/tmp/foo.sock", "/", "http://example.com"}, false},
		{"plaintext on funnel", []string{"-name", "foo", "-plaintext=true", "-funnel", "/", "http://example.com"}, false},
		{"invalid funnelOnly", []string{"-name", "foo", "-funnelOnly", "/", "http://example.com"}, false},
		{"invalid destination URL", []string{"-name", "foo", "/", "::--example.com"}, false},
	} {
		test := elt
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			v, _, err := tailnetSrvFromArgs(test.args)
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
	apiKey := os.Getenv("TS_AUTHKEY")
	if apiKey == "" {
		t.Skip("Serving on the tailnet requires an API key to be set")
	}

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
	defer ts.Close()

	s, _, err := tailnetSrvFromArgs([]string{"-name", "TestPrefixServing", "-ephemeral",
		"/subpath", ts.URL,
	})
	require.NoError(t, err)
	// TODO: It would be relatively doable here to just create the mux & test the reverseproxy logic alone.
	_, mux, _, err := s.listenerAndMux(context.Background())
	require.NoError(t, err)
	proxy := httptest.NewServer(mux)
	pc := proxy.Client()
	resp404, err := pc.Get(proxy.URL)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, resp404.StatusCode)

	// Subpath itself:
	respOk, err := pc.Get(proxy.URL + "/subpath")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, respOk.StatusCode)
	body, err := ioutil.ReadAll(respOk.Body)
	require.NoError(t, err)
	assert.Equal(t, []byte("ok"), body)

	// Subpaths of /subpath:
	respOk, err = pc.Get(proxy.URL + "/subpath/hi")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, respOk.StatusCode)
	body, err = ioutil.ReadAll(respOk.Body)
	require.NoError(t, err)
	assert.Equal(t, []byte("ok"), body)
}
