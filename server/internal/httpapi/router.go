// Package httpapi wires the HTTP surface: middleware chain, API routes and the
// embedded single-page application.
package httpapi

import (
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/erolbeyaz/kubby/internal/audit"
	"github.com/erolbeyaz/kubby/internal/auth"
	"github.com/erolbeyaz/kubby/internal/cluster"
	"github.com/erolbeyaz/kubby/internal/config"
	"github.com/erolbeyaz/kubby/internal/rbac"
	"github.com/erolbeyaz/kubby/internal/store"
)

// Deps are the collaborators the HTTP layer needs.
type Deps struct {
	Config  *config.Config
	Logger  *slog.Logger
	DB      Pinger
	Store   *store.DB
	Auth    *auth.Service
	Cluster *cluster.Service
	Audit   *audit.Emitter
	WebFS   fs.FS
}

// Server owns the handler and any background resources it needs to release.
type Server struct {
	Handler  http.Handler
	limiters []*rateLimiter
}

// Close stops the rate limiter sweepers.
func (s *Server) Close() {
	for _, rl := range s.limiters {
		rl.close()
	}
}

// New builds the fully configured server.
func New(d Deps) *Server {
	secure := d.Config.HTTP.SecureCookies()

	loginLimit := newRateLimiter(d.Config.Auth.LoginRatePerMinute, d.Config.Auth.LoginRateBurst)
	apiLimit := newRateLimiter(d.Config.Auth.APIRatePerMinute, d.Config.Auth.APIRateBurst)

	authAPI := &authHandlers{
		svc:        d.Auth,
		audit:      d.Audit,
		users:      d.Store.Users(),
		sessions:   d.Store.Sessions(),
		recovery:   d.Store.RecoveryCodes(),
		secure:     secure,
		refreshTTL: d.Config.Auth.RefreshTTL,
		loginLimit: loginLimit,
	}
	clusterAPI := &clusterHandlers{
		svc:      d.Cluster,
		clusters: d.Store.Clusters(),
		users:    d.Store.Users(),
		audit:    d.Audit,
	}
	userAPI := &userHandlers{
		users:     d.Store.Users(),
		sessions:  d.Store.Sessions(),
		auditLog:  d.Audit,
		auditRepo: d.Store.Audit(),
		argon2:    auth.DefaultArgon2Params(d.Config.Auth.Argon2MemoryMiB),
	}

	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(requestID)
	r.Use(recoverPanic(d.Logger))
	r.Use(securityHeaders(secure))
	r.Use(accessLog(d.Logger))
	r.Use(realIP(d.Config.HTTP.TrustedProxies, d.Logger))
	r.Use(middleware.Compress(5))

	r.Get("/healthz", handleLive())
	r.Get("/readyz", handleReady(d.DB))
	r.Get("/version", handleVersion())

	r.Route("/api/v1", func(api chi.Router) {
		api.Use(rateLimit(apiLimit))
		api.Use(ensureCSRFCookie(secure, d.Config.Auth.RefreshTTL))
		api.Use(csrfProtect(d.Config.HTTP.PublicURL, d.Config.HTTP.AllowedOrigins))
		api.Use(requireWritable(d.Config.HTTP.ReadOnly))

		api.NotFound(func(w http.ResponseWriter, r *http.Request) {
			writeError(w, r, http.StatusNotFound, "endpoint not found")
		})

		// First-run wizard. The handlers themselves refuse to run once a user exists,
		// so these routes cannot be used to create a second account.
		api.Get("/setup/status", authAPI.setupStatus)
		api.With(rateLimit(loginLimit)).Post("/setup/admin", authAPI.createFirstAdmin)

		api.With(rateLimit(loginLimit)).Post("/auth/login", authAPI.login)
		api.With(rateLimit(loginLimit)).Post("/auth/refresh", authAPI.refresh)

		// MFA endpoints accept a session that has cleared the password step only.
		api.Group(func(mfa chi.Router) {
			mfa.Use(requirePendingMFA(d.Auth))
			mfa.Use(rateLimit(loginLimit))
			mfa.Post("/auth/mfa/verify", authAPI.verifyMFA)

			// Enrolment must be reachable by a session that still owes a second
			// factor. Otherwise a user whose policy requires MFA but who has not
			// enrolled yet can never reach any endpoint at all.
			mfa.Post("/me/mfa/enroll", authAPI.enrollTOTP)
			mfa.Post("/me/mfa/confirm", authAPI.confirmTOTP)
		})

		api.Group(func(authed chi.Router) {
			authed.Use(requireAuth(d.Auth))

			authed.Post("/auth/logout", authAPI.logout)
			authed.Get("/me", authAPI.me(d.Config.HTTP.ReadOnly))
			authed.Post("/me/password", authAPI.changePassword)
			authed.Get("/me/sessions", authAPI.listSessions)
			authed.Delete("/me/sessions", authAPI.revokeOtherSessions)

			// Cluster reads are open to any authenticated user; the handler narrows the
			// result to what they were granted.
			authed.With(requirePermission(rbac.PermClusterRead)).Group(func(cl chi.Router) {
				cl.Get("/clusters", clusterAPI.list)
				cl.Get("/clusters/{id}", clusterAPI.get)
				cl.Post("/clusters/{id}/test", clusterAPI.test)
			})

			authed.With(requirePermission(rbac.PermClusterManage)).Group(func(cl chi.Router) {
				cl.Post("/clusters/validate", clusterAPI.validate)
				cl.Post("/clusters", clusterAPI.create)
				cl.Patch("/clusters/{id}", clusterAPI.update)
				cl.Delete("/clusters/{id}", clusterAPI.remove)
				cl.Put("/clusters/{id}/credentials", clusterAPI.replaceCredential)
				cl.Get("/clusters/{id}/grants", clusterAPI.listGrants)
				cl.Put("/clusters/{id}/grants", clusterAPI.setGrant)
			})

			authed.With(requirePermission(rbac.PermUserManage)).Group(func(admin chi.Router) {
				admin.Get("/users", userAPI.list)
				admin.Post("/users", userAPI.create)
				admin.Patch("/users/{id}", userAPI.update)
			})
			authed.With(requirePermission(rbac.PermAuditRead)).Get("/audit", userAPI.listAudit)
		})
	})

	mountSPA(r, d.WebFS, d.Logger)

	return &Server{Handler: r, limiters: []*rateLimiter{loginLimit, apiLimit}}
}
