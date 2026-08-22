package httpapi

import (
	"context"
	"net/http"
	"net/netip"

	"github.com/erolbeyaz/kubby/internal/store"
)

type ctxKey struct{ name string }

var (
	sessionKey = ctxKey{"session"}
	userKey    = ctxKey{"user"}
)

func withPrincipal(ctx context.Context, s *store.Session, u *store.User) context.Context {
	return context.WithValue(context.WithValue(ctx, sessionKey, s), userKey, u)
}

// principal returns the authenticated user and session, or nil when the request is
// anonymous. Handlers behind requireAuth can rely on both being present.
func principal(r *http.Request) (*store.Session, *store.User) {
	session, _ := r.Context().Value(sessionKey).(*store.Session)
	user, _ := r.Context().Value(userKey).(*store.User)
	return session, user
}

// clientAddr returns the resolved client address, which realIP has already vetted
// against the trusted-proxy list (ADR-032).
func clientAddr(r *http.Request) *netip.Addr {
	host := clientKey(r)
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return nil
	}
	return &addr
}
