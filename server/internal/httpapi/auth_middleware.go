package httpapi

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/erolbeyaz/kubby/internal/auth"
	"github.com/erolbeyaz/kubby/internal/rbac"
)

// requireAuth resolves the session cookie and rejects anonymous or half-authenticated
// requests. A session that has not completed MFA can only reach the MFA endpoints.
func requireAuth(svc *auth.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := cookieValue(r, sessionCookieName)
			if token == "" {
				writeError(w, r, http.StatusUnauthorized, "authentication required")
				return
			}

			session, user, err := svc.Resolve(r.Context(), token)
			switch {
			case errors.Is(err, auth.ErrMFARequired):
				writeError(w, r, http.StatusForbidden, "multi-factor authentication is required")
				return
			case errors.Is(err, auth.ErrAccountInactive):
				writeError(w, r, http.StatusForbidden, "account is deactivated")
				return
			case err != nil:
				writeError(w, r, http.StatusUnauthorized, "authentication required")
				return
			}

			next.ServeHTTP(w, r.WithContext(withPrincipal(r.Context(), session, user)))
		})
	}
}

// requirePendingMFA admits only sessions that passed the password step but still owe a
// second factor — the narrow window the MFA endpoints operate in.
func requirePendingMFA(svc *auth.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := cookieValue(r, sessionCookieName)
			if token == "" {
				writeError(w, r, http.StatusUnauthorized, "authentication required")
				return
			}

			session, user, err := svc.Resolve(r.Context(), token)
			if err != nil && !errors.Is(err, auth.ErrMFARequired) {
				writeError(w, r, http.StatusUnauthorized, "authentication required")
				return
			}
			if session == nil || user == nil {
				writeError(w, r, http.StatusUnauthorized, "authentication required")
				return
			}

			next.ServeHTTP(w, r.WithContext(withPrincipal(r.Context(), session, user)))
		})
	}
}

// requirePermission enforces a permission on the server for every request. Hiding a
// control in the UI is never the authorisation mechanism.
func requirePermission(p rbac.Permission) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, user := principal(r)
			if user == nil || !rbac.Role(user.Role).Can(p) {
				writeError(w, r, http.StatusForbidden, "insufficient permissions")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// requireWritable blocks every mutating request while the global read-only switch is
// on (ADR-011).
func requireWritable(readOnly bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if readOnly && isMutating(r.Method) {
				writeError(w, r, http.StatusConflict, "Kubby is running in read-only mode")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ensureCSRFCookie bootstraps the double-submit token.
//
// Without this, the very first mutating request of a session — completing the setup
// wizard or signing in — has no cookie to match and is always rejected. The token is
// only minted on safe methods, so a cross-site POST cannot mint one for itself, and an
// existing token is never rotated mid-session.
func ensureCSRFCookie(secure bool, ttl time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !isMutating(r.Method) && cookieValue(r, csrfCookieName) == "" {
				if token, _, err := auth.NewToken(); err == nil {
					setCSRFCookie(w, token, secure, ttl)
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// csrfProtect implements double-submit cookie validation plus an origin check.
//
// Two independent checks are used because each covers a gap in the other: SameSite=Strict
// already blocks most cross-site requests, the token defeats subdomain-based attacks,
// and the origin check catches clients that ignore SameSite.
func csrfProtect(publicURL *url.URL, allowedOrigins []*url.URL) func(http.Handler) http.Handler {
	trusted := append([]*url.URL{publicURL}, allowedOrigins...)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !isMutating(r.Method) {
				next.ServeHTTP(w, r)
				return
			}

			if origin := r.Header.Get("Origin"); origin != "" && !originAllowed(origin, trusted) {
				writeError(w, r, http.StatusForbidden, "request origin is not allowed")
				return
			}

			cookie := cookieValue(r, csrfCookieName)
			header := r.Header.Get(csrfHeaderName)
			if cookie == "" || header == "" || subtle.ConstantTimeCompare([]byte(cookie), []byte(header)) != 1 {
				writeError(w, r, http.StatusForbidden, "missing or invalid CSRF token")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func isMutating(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	default:
		return true
	}
}

// originAllowed reports whether the request origin matches the public URL or any
// additionally configured origin.
func originAllowed(origin string, trusted []*url.URL) bool {
	for _, candidate := range trusted {
		if candidate != nil && sameOrigin(origin, candidate) {
			return true
		}
	}
	return false
}

// sameOrigin compares scheme and host, ignoring a default port on either side.
func sameOrigin(origin string, publicURL *url.URL) bool {
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return strings.EqualFold(parsed.Scheme, publicURL.Scheme) &&
		strings.EqualFold(canonicalHost(parsed), canonicalHost(publicURL))
}

func canonicalHost(u *url.URL) string {
	host, port := u.Hostname(), u.Port()
	if (u.Scheme == "http" && port == "80") || (u.Scheme == "https" && port == "443") || port == "" {
		return host
	}
	return host + ":" + port
}
