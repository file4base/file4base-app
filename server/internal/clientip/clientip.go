// Package clientip resolves the client IP address of an HTTP request.
//
// X-Forwarded-For and X-Real-IP are set by whoever made the request, so they
// are only trustworthy when the request reached the server through a known
// reverse proxy. A Resolver therefore honours those headers only when the
// immediate TCP peer (RemoteAddr) is one of a configured set of trusted
// proxies; otherwise the headers are attacker-controlled and are ignored in
// favour of the real peer address. With no trusted proxies configured (the
// default), the forwarding headers are never trusted.
//
// Resolving the client IP this way keeps the audit log, the request telemetry
// and the login-throttle key honest: a direct client cannot forge the address
// recorded against it or attributed to another account.
package clientip

import (
	"fmt"
	"net"
	"net/http"
	"strings"
)

// Resolver derives the client IP to attribute a request to.
type Resolver struct {
	trusted []*net.IPNet
}

// NewResolver builds a Resolver that trusts the forwarding headers of requests
// coming from the given proxy networks. A nil or empty list means no proxy is
// trusted, so forwarding headers are always ignored.
func NewResolver(trusted []*net.IPNet) *Resolver {
	return &Resolver{trusted: trusted}
}

// ParseTrustedProxies parses a comma- or whitespace-separated list of IP
// addresses and CIDR ranges (for example "10.0.0.0/8, 192.168.1.5, ::1").
// A bare address is treated as a single-host network (/32 or /128).
//
// It returns every entry it could parse, together with an error naming the
// entries it skipped. Callers should log the error but may still use the
// returned networks, so that one malformed entry neither disables proxy trust
// entirely nor aborts startup.
func ParseTrustedProxies(spec string) ([]*net.IPNet, error) {
	fields := strings.FieldsFunc(spec, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
	})
	nets := make([]*net.IPNet, 0, len(fields))
	var bad []string
	for _, f := range fields {
		if strings.Contains(f, "/") {
			_, n, err := net.ParseCIDR(f)
			if err != nil {
				bad = append(bad, f)
				continue
			}
			nets = append(nets, n)
			continue
		}
		ip := net.ParseIP(f)
		if ip == nil {
			bad = append(bad, f)
			continue
		}
		nets = append(nets, singleHostNet(ip))
	}
	if len(bad) > 0 {
		return nets, fmt.Errorf("ignored unparseable trusted-proxy entries: %s", strings.Join(bad, ", "))
	}
	return nets, nil
}

func singleHostNet(ip net.IP) *net.IPNet {
	if v4 := ip.To4(); v4 != nil {
		return &net.IPNet{IP: v4, Mask: net.CIDRMask(32, 32)}
	}
	return &net.IPNet{IP: ip, Mask: net.CIDRMask(128, 128)}
}

// ClientIP returns the IP address to attribute the request to. A nil Resolver,
// or one with no trusted proxies, always returns the direct peer address.
func (r *Resolver) ClientIP(req *http.Request) string {
	host := hostOnly(req.RemoteAddr)
	if r == nil || len(r.trusted) == 0 {
		return host
	}
	peer := net.ParseIP(host)
	if peer == nil || !r.isTrusted(peer) {
		// The direct peer is not a trusted proxy: ignore forwarding headers.
		return host
	}
	// The peer is a trusted proxy. X-Forwarded-For is ordered
	// "client, proxy1, proxy2, ..." left-to-right, each hop appending the
	// address it received the request from. Walk right-to-left past the hops
	// that are themselves trusted proxies and return the first external one.
	if xff := req.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		for i := len(parts) - 1; i >= 0; i-- {
			cand := net.ParseIP(strings.TrimSpace(parts[i]))
			if cand == nil {
				continue
			}
			if !r.isTrusted(cand) {
				return cand.String()
			}
		}
		// Every listed hop was itself trusted: fall back to the left-most
		// (original) parseable entry.
		for i := 0; i < len(parts); i++ {
			if cand := net.ParseIP(strings.TrimSpace(parts[i])); cand != nil {
				return cand.String()
			}
		}
	}
	if real := strings.TrimSpace(req.Header.Get("X-Real-IP")); real != "" {
		if cand := net.ParseIP(real); cand != nil {
			return cand.String()
		}
	}
	return host
}

func (r *Resolver) isTrusted(ip net.IP) bool {
	for _, n := range r.trusted {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// hostOnly strips the port (and any IPv6 brackets) from a host:port address,
// returning just the host. It tolerates an address that carries no port.
func hostOnly(remoteAddr string) string {
	remoteAddr = strings.TrimSpace(remoteAddr)
	if remoteAddr == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		return host
	}
	// No port present: it may be a bare IP or a bracketed IPv6 literal.
	return strings.TrimPrefix(strings.TrimSuffix(remoteAddr, "]"), "[")
}
