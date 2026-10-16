package clientip

import (
	"net"
	"net/http"
	"testing"
)

func mustParse(t *testing.T, spec string) []*net.IPNet {
	t.Helper()
	nets, err := ParseTrustedProxies(spec)
	if err != nil {
		t.Fatalf("ParseTrustedProxies(%q) error: %v", spec, err)
	}
	return nets
}

func req(remoteAddr string, headers map[string]string) *http.Request {
	r, _ := http.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = remoteAddr
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	return r
}

func TestParseTrustedProxies(t *testing.T) {
	nets, err := ParseTrustedProxies("10.0.0.0/8, 192.168.1.5 , ::1\n2001:db8::/32")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(nets) != 4 {
		t.Fatalf("expected 4 networks, got %d", len(nets))
	}

	// An empty spec yields no networks and no error.
	empty, err := ParseTrustedProxies("")
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty spec: got %d nets, err %v", len(empty), err)
	}

	// Bad entries are reported but the good ones are still returned.
	mixed, err := ParseTrustedProxies("10.0.0.0/8, not-an-ip, 1.2.3.4, 10.0.0.0/999")
	if err == nil {
		t.Fatalf("expected an error for the malformed entries")
	}
	if len(mixed) != 2 {
		t.Fatalf("expected 2 valid networks, got %d", len(mixed))
	}
}

func TestClientIP_NoTrustedProxies(t *testing.T) {
	r := NewResolver(nil)
	// Forwarding headers must be ignored when no proxy is trusted.
	got := r.ClientIP(req("203.0.113.9:4444", map[string]string{
		"X-Forwarded-For": "1.2.3.4",
		"X-Real-IP":       "5.6.7.8",
	}))
	if got != "203.0.113.9" {
		t.Fatalf("got %q, want the peer address 203.0.113.9", got)
	}
}

func TestClientIP_NilResolver(t *testing.T) {
	var r *Resolver
	got := r.ClientIP(req("198.51.100.2:9", map[string]string{"X-Forwarded-For": "1.2.3.4"}))
	if got != "198.51.100.2" {
		t.Fatalf("got %q, want 198.51.100.2", got)
	}
}

func TestClientIP_UntrustedPeerIgnoresSpoofedHeader(t *testing.T) {
	// The direct peer is not in the trusted set, so its forged header is ignored.
	r := NewResolver(mustParse(t, "10.0.0.0/8"))
	got := r.ClientIP(req("203.0.113.9:1234", map[string]string{
		"X-Forwarded-For": "127.0.0.1",
	}))
	if got != "203.0.113.9" {
		t.Fatalf("spoofed header honoured: got %q, want 203.0.113.9", got)
	}
}

func TestClientIP_TrustedProxyChain(t *testing.T) {
	r := NewResolver(mustParse(t, "10.0.0.0/8"))

	cases := []struct {
		name       string
		remoteAddr string
		xff        string
		realIP     string
		want       string
	}{
		{"single client", "10.1.2.3:80", "203.0.113.5", "", "203.0.113.5"},
		{"client plus one trusted hop", "10.1.2.3:80", "203.0.113.5, 10.0.0.9", "", "203.0.113.5"},
		{"client plus several trusted hops", "10.1.2.3:80", "203.0.113.5, 10.0.0.9, 10.0.0.10", "", "203.0.113.5"},
		{"all hops trusted falls back to left-most", "10.1.2.3:80", "10.0.0.8, 10.0.0.9", "", "10.0.0.8"},
		{"no xff uses x-real-ip", "10.1.2.3:80", "", "198.51.100.7", "198.51.100.7"},
		{"garbage xff falls back to real-ip", "10.1.2.3:80", "garbage", "198.51.100.7", "198.51.100.7"},
		{"inner spoof before trusted hops is still skipped", "10.1.2.3:80", "6.6.6.6, 203.0.113.5, 10.0.0.9", "", "203.0.113.5"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := map[string]string{}
			if tc.xff != "" {
				h["X-Forwarded-For"] = tc.xff
			}
			if tc.realIP != "" {
				h["X-Real-IP"] = tc.realIP
			}
			got := r.ClientIP(req(tc.remoteAddr, h))
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestClientIP_IPv6TrustedPeer(t *testing.T) {
	r := NewResolver(mustParse(t, "::1/128"))
	got := r.ClientIP(req("[::1]:5000", map[string]string{"X-Forwarded-For": "203.0.113.5"}))
	if got != "203.0.113.5" {
		t.Fatalf("got %q, want 203.0.113.5", got)
	}
}

func TestClientIP_StripsPortWithoutTrust(t *testing.T) {
	r := NewResolver(nil)
	if got := r.ClientIP(req("192.0.2.55:65535", nil)); got != "192.0.2.55" {
		t.Fatalf("got %q, want 192.0.2.55", got)
	}
	// A bare address without a port is returned unchanged.
	if got := r.ClientIP(req("192.0.2.55", nil)); got != "192.0.2.55" {
		t.Fatalf("got %q, want 192.0.2.55", got)
	}
}
