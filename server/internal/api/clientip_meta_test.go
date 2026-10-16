package api

import (
	"net/http"
	"testing"

	"github.com/file4base/file4base-app/server/internal/clientip"
)

func newMetaRequest(remoteAddr string, headers map[string]string) *http.Request {
	r, _ := http.NewRequest(http.MethodGet, "/api/v1/auth/session", nil)
	r.RemoteAddr = remoteAddr
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	return r
}

// A direct (untrusted) client cannot forge the address recorded against it:
// without a trusted-proxy configuration the X-Forwarded-For header is ignored.
func TestExtractClientSessionMeta_IgnoresSpoofedXFFFromUntrustedPeer(t *testing.T) {
	resolver := clientip.NewResolver(nil)
	ip, _, _ := extractClientSessionMeta(newMetaRequest("203.0.113.40:5050", map[string]string{
		"X-Forwarded-For": "10.0.0.1",
		"X-Real-IP":       "127.0.0.1",
	}), resolver)
	if ip != "203.0.113.40" {
		t.Fatalf("spoofed header honoured: got %q, want the peer 203.0.113.40", ip)
	}
}

// Behind a configured trusted proxy the real client address is recovered from
// X-Forwarded-For.
func TestExtractClientSessionMeta_HonoursTrustedProxy(t *testing.T) {
	trusted, err := clientip.ParseTrustedProxies("10.0.0.0/8")
	if err != nil {
		t.Fatalf("ParseTrustedProxies: %v", err)
	}
	resolver := clientip.NewResolver(trusted)
	ip, _, _ := extractClientSessionMeta(newMetaRequest("10.1.1.1:443", map[string]string{
		"X-Forwarded-For": "198.51.100.23, 10.0.0.9",
	}), resolver)
	if ip != "198.51.100.23" {
		t.Fatalf("got %q, want 198.51.100.23", ip)
	}
}

// A nil resolver (handler constructed without wiring) is safe: it records the
// direct peer address rather than panicking or trusting headers.
func TestExtractClientSessionMeta_NilResolverUsesPeer(t *testing.T) {
	ip, webContainer, _ := extractClientSessionMeta(newMetaRequest("192.0.2.7:1000", map[string]string{
		"X-Forwarded-For": "1.2.3.4",
		"User-Agent":      "Dart/3.0",
	}), nil)
	if ip != "192.0.2.7" {
		t.Fatalf("got %q, want 192.0.2.7", ip)
	}
	if webContainer != "desktop-native" {
		t.Fatalf("got webContainer %q, want desktop-native", webContainer)
	}
}
