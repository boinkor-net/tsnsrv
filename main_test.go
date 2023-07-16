package main

import (
	"testing"
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
