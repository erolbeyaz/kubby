package httpapi

import (
	"log/slog"
	"net"
	"net/http"
	"strings"
)

// realIP resolves the client address, honouring forwarding headers only when the
// immediate peer is a configured trusted proxy.
//
// chi's middleware.RealIP is deliberately not used: it rewrites RemoteAddr from
// X-Forwarded-For unconditionally, so any client can forge its own address
// (GHSA-3fxj-6jh8-hvhx). Rate limiting and account lockout in Faz 2 depend on this
// value, so a spoofable source address would defeat both.
func realIP(trustedProxies []string, logger *slog.Logger) func(http.Handler) http.Handler {
	networks := parseCIDRs(trustedProxies, logger)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if len(networks) > 0 {
				if peer := peerIP(r.RemoteAddr); peer != nil && containsIP(networks, peer) {
					if forwarded := clientFromHeaders(r, networks); forwarded != "" {
						r.RemoteAddr = net.JoinHostPort(forwarded, "0")
					}
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// clientFromHeaders walks X-Forwarded-For from right to left and returns the first
// address that is not itself a trusted proxy — the closest untrusted hop.
func clientFromHeaders(r *http.Request, trusted []*net.IPNet) string {
	forwarded := r.Header.Get("X-Forwarded-For")
	if forwarded == "" {
		if candidate := net.ParseIP(strings.TrimSpace(r.Header.Get("X-Real-Ip"))); candidate != nil {
			return candidate.String()
		}
		return ""
	}

	hops := strings.Split(forwarded, ",")
	for i := len(hops) - 1; i >= 0; i-- {
		candidate := net.ParseIP(strings.TrimSpace(hops[i]))
		if candidate == nil {
			continue
		}
		if !containsIP(trusted, candidate) {
			return candidate.String()
		}
	}
	return ""
}

func peerIP(remoteAddr string) net.IP {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	return net.ParseIP(host)
}

func containsIP(networks []*net.IPNet, ip net.IP) bool {
	for _, n := range networks {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// parseCIDRs accepts both CIDR blocks and bare addresses. An unparseable entry is
// dropped with a warning rather than silently widening trust.
func parseCIDRs(entries []string, logger *slog.Logger) []*net.IPNet {
	networks := make([]*net.IPNet, 0, len(entries))

	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if _, network, err := net.ParseCIDR(entry); err == nil {
			networks = append(networks, network)
			continue
		}
		if ip := net.ParseIP(entry); ip != nil {
			bits := 32
			if ip.To4() == nil {
				bits = 128
			}
			networks = append(networks, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
			continue
		}
		logger.Warn("ignoring unparseable trusted proxy entry", slog.String("entry", entry))
	}
	return networks
}
