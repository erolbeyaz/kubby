package httpapi

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Rate limiting and account lockout (Faz 2) key off the client address, so a client
// must not be able to choose its own.
func TestRealIPIgnoresForwardedHeadersFromUntrustedPeers(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))

	tests := []struct {
		name           string
		trustedProxies []string
		remoteAddr     string
		forwardedFor   string
		wantPrefix     string
	}{
		{
			name:           "no trusted proxies configured",
			trustedProxies: nil,
			remoteAddr:     "203.0.113.9:5555",
			forwardedFor:   "1.2.3.4",
			wantPrefix:     "203.0.113.9",
		},
		{
			name:           "peer is not trusted",
			trustedProxies: []string{"10.0.0.0/8"},
			remoteAddr:     "203.0.113.9:5555",
			forwardedFor:   "1.2.3.4",
			wantPrefix:     "203.0.113.9",
		},
		{
			name:           "peer is trusted",
			trustedProxies: []string{"10.0.0.0/8"},
			remoteAddr:     "10.1.2.3:5555",
			forwardedFor:   "198.51.100.7",
			wantPrefix:     "198.51.100.7",
		},
		{
			name:           "chained proxies return the closest untrusted hop",
			trustedProxies: []string{"10.0.0.0/8"},
			remoteAddr:     "10.1.2.3:5555",
			forwardedFor:   "198.51.100.7, 10.4.4.4, 10.5.5.5",
			wantPrefix:     "198.51.100.7",
		},
		{
			name:           "forged header behind a trusted proxy still yields a real hop",
			trustedProxies: []string{"10.0.0.0/8"},
			remoteAddr:     "10.1.2.3:5555",
			forwardedFor:   "not-an-ip, 198.51.100.7",
			wantPrefix:     "198.51.100.7",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var seen string
			handler := realIP(tc.trustedProxies, logger)(http.HandlerFunc(
				func(_ http.ResponseWriter, r *http.Request) { seen = r.RemoteAddr },
			))

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tc.remoteAddr
			req.Header.Set("X-Forwarded-For", tc.forwardedFor)

			handler.ServeHTTP(httptest.NewRecorder(), req)

			if got := hostOf(seen); got != tc.wantPrefix {
				t.Errorf("client address = %q, want %q (from RemoteAddr %q, XFF %q)",
					got, tc.wantPrefix, tc.remoteAddr, tc.forwardedFor)
			}
		})
	}
}

func hostOf(addr string) string {
	for i := len(addr) - 1; i >= 0; i-- {
		if addr[i] == ':' {
			return addr[:i]
		}
	}
	return addr
}
