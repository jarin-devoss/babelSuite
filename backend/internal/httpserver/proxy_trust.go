package httpserver

import (
	"net"
	"net/http"
	"strings"
)

// ProxyTrust checks whether an incoming request originates from a trusted
// reverse proxy. Only requests from trusted sources should have their
// X-Forwarded-* headers honoured.
type ProxyTrust struct {
	cidrs []*net.IPNet
}

// ParseProxyTrust parses a list of CIDR strings into a ProxyTrust.
// An empty list means no proxy is trusted (default-safe).
func ParseProxyTrust(cidrs []string) (*ProxyTrust, error) {
	parsed := make([]*net.IPNet, 0, len(cidrs))
	for _, cidr := range cidrs {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			return nil, err
		}
		parsed = append(parsed, network)
	}
	return &ProxyTrust{cidrs: parsed}, nil
}

// IsTrustedRequest returns true if the request's remote address falls within
// one of the configured trusted CIDR ranges.
func (p *ProxyTrust) IsTrustedRequest(r *http.Request) bool {
	if p == nil || len(p.cidrs) == 0 {
		return false
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	for _, network := range p.cidrs {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

// IsSecureRequest returns true when the connection is TLS or when the request
// came from a trusted proxy that set X-Forwarded-Proto: https.
func IsSecureRequest(r *http.Request, trust *ProxyTrust) bool {
	if r.TLS != nil {
		return true
	}
	if trust.IsTrustedRequest(r) {
		proto := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto"))
		return strings.EqualFold(proto, "https")
	}
	return false
}
