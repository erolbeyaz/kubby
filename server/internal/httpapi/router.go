// Package httpapi wires the HTTP surface: middleware chain, API routes and the
// embedded single-page application.
package httpapi

import (
	"context"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/erolbeyaz/kubby/internal/audit"
	"github.com/erolbeyaz/kubby/internal/auth"
	"github.com/erolbeyaz/kubby/internal/cluster"
	"github.com/erolbeyaz/kubby/internal/config"
	"github.com/erolbeyaz/kubby/internal/crypto"
	"github.com/erolbeyaz/kubby/internal/health"
	"github.com/erolbeyaz/kubby/internal/metrics"
	"github.com/erolbeyaz/kubby/internal/rbac"
	"github.com/erolbeyaz/kubby/internal/settings"
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
	// AuditShipper copies the audit trail to a SIEM. Optional: with none configured the
	// database and the log stream are still the audit trail.
	AuditShipper *audit.ShipperManager
	// Keyring seals the credentials settings carry, with the same key as cluster
	// credentials so there is one key to rotate rather than two.
	Keyring *crypto.Keyring
	// Metrics is Kubby's own instrumentation. Optional: with none, /metrics answers 404
	// and nothing is recorded.
	Metrics *metrics.Registry
	WebFS   fs.FS
}

// Server owns the handler and any background resources it needs to release.
type Server struct {
	Handler  http.Handler
	limiters []*rateLimiter
	shipper  *audit.ShipperManager
}

// Close stops the rate limiter sweepers.
func (s *Server) Close() {
	for _, rl := range s.limiters {
		rl.close()
	}
	if s.shipper != nil {
		// Bounded: what is queued belongs at the sink, but shutdown must not hang on a
		// destination that has stopped answering.
		drain, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = s.shipper.Close(drain)
	}
}

// schemaReaderOf converts an absent store into an absent interface.
//
// A typed nil placed in an interface is not nil: `d.Store` may be a nil *store.DB, and
// assigning it directly would leave the handler's `schema != nil` check passing and its
// first call dereferencing nothing.
func schemaReaderOf(db *store.DB) SchemaReader {
	if db == nil {
		return nil
	}
	return db
}

// New builds the fully configured server.
func New(d Deps) *Server {
	secure := d.Config.HTTP.SecureCookies()

	// Built here rather than only by the process that starts the server, for the same
	// reason the audit shipper is: wiring it in one call site meant /metrics was absent
	// from every test that built a router, so nothing checked that it stays private or
	// that its labels stay bounded.
	if d.Metrics == nil {
		d.Metrics = metrics.New()
	}

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
		metrics:    d.Metrics,
	}
	clusterAPI := &clusterHandlers{
		svc:      d.Cluster,
		clusters: d.Store.Clusters(),
		users:    d.Store.Users(),
		audit:    d.Audit,
	}
	settingsService := settings.New(d.Store.Settings(), d.Keyring)

	// The audit shipper is built here rather than only by the process that starts the
	// server, so it exists wherever the router does. Wiring it in one call site meant the
	// feature was absent from every test that built a router, which is exactly where a
	// missing copy of the audit trail would go unnoticed.
	shipper := d.AuditShipper
	if shipper == nil {
		shipper = audit.NewShipperManager(d.Logger)
	}
	d.Audit.WithShipper(shipper)

	if d.Metrics != nil {
		// The one that matters is the drop count: audit records not reaching the SIEM is
		// a compliance failure nothing else in the system notices, because Kubby carries
		// on working perfectly while it happens.
		if err := d.Metrics.RegisterAuditShipper(func() (string, int, uint64, uint64, uint64, uint64, bool) {
			stats, running := shipper.Stats()
			return stats.Sink, stats.Queued, stats.Sent, stats.Failed, stats.Dropped, stats.Retries, running
		}); err != nil {
			d.Logger.Warn("audit shipping metrics could not be registered",
				slog.String("error", err.Error()))
		}
	}

	// Applied in the background rather than while the router is being built.
	//
	// Building a router must not touch the database: /healthz answers whether the process
	// is alive, deliberately without asking Postgres anything, and reading a setting here
	// made even constructing the server depend on the database being up.
	go func() {
		// A background task must not be able to take the process down. Handlers have
		// recoverPanic for the same reason; a goroutine started here has nothing above it
		// to catch a panic, so a bad store or a surprise nil would kill Kubby outright
		// rather than lose one optional copy of the audit trail.
		defer func() {
			if rec := recover(); rec != nil {
				d.Logger.Error("audit shipping setup panicked", slog.Any("panic", rec))
			}
		}()

		startup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		enabled, sinkCfg, err := settingsService.AuditSinkConfig(startup)
		if err != nil {
			d.Logger.Warn("could not read the audit shipping setting",
				slog.String("error", err.Error()))
			return
		}
		if err := shipper.Apply(startup, enabled, sinkCfg); err != nil {
			// Not fatal: the database and the log stream are the audit trail, and
			// refusing to serve would turn a misconfigured copy into an outage.
			d.Logger.Error("audit shipping is configured but could not start",
				slog.String("error", err.Error()))
		}
	}()

	// A cluster that names no Prometheus of its own falls back to the deployment's, which
	// is what a central Prometheus or Thanos looks like. Read per request rather than
	// captured, so an admin's change takes effect without a restart.
	d.Cluster.WithMetricsDefaults(func(ctx context.Context) (cluster.MetricsDefaults, error) {
		all, err := settingsService.All(ctx)
		if err != nil {
			return cluster.MetricsDefaults{}, err
		}
		password, _, err := settingsService.Secret(ctx, settings.SecretMetricsPassword)
		if err != nil {
			return cluster.MetricsDefaults{}, err
		}
		return cluster.MetricsDefaults{
			Enabled:            all.Metrics.Enabled,
			URL:                all.Metrics.URL,
			Username:           all.Metrics.Username,
			Password:           password,
			InsecureSkipVerify: all.Metrics.InsecureSkipVerify,
		}, nil
	})

	resourceAPI := &resourceHandlers{
		svc:            d.Cluster,
		clusters:       d.Store.Clusters(),
		audit:          d.Audit,
		fleet:          &health.Fleet{},
		sidecars:       d.Config.K8s.SidecarContainers,
		eventWindow:    defaultEventWindow,
		allowedOrigins: originHosts(d.Config.HTTP.PublicURL, d.Config.HTTP.AllowedOrigins),
		readOnly:       d.Config.HTTP.ReadOnly,
		settings:       settingsService,
		forwards:       newForwardRegistry(),
	}
	settingsAPI := &settingsHandlers{svc: settingsService, audit: d.Audit, shipper: shipper}
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
	r.Use(observe(d.Metrics))
	r.Use(realIP(d.Config.HTTP.TrustedProxies, d.Logger))
	r.Use(middleware.Compress(5))

	r.Get("/healthz", handleLive())
	r.Get("/readyz", handleReady(d.DB, schemaReaderOf(d.Store)))
	r.Get("/version", handleVersion())

	// Kubby's own metrics. Never unauthenticated: a scraper presents the configured
	// token, a person presents a session with audit.read.
	r.Get("/metrics", guardMetrics(
		d.Config.HTTP.MetricsToken,
		metricsHandler(d.Metrics),
		requireAuth(d.Auth)(requirePermission(rbac.PermAuditRead)(metricsHandler(d.Metrics))),
	))

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

				cl.Get("/clusters/{id}/overview", resourceAPI.overview)
				cl.Get("/clusters/{id}/workloads-overview", resourceAPI.workloadsOverview)
				cl.Get("/clusters/{id}/namespaces", resourceAPI.listNamespaces)
				cl.Get("/clusters/{id}/resource-types", resourceAPI.listTypes)
				// The type key carries a slash for grouped kinds ("apps/deployments"),
				// so these are wildcard routes rather than path parameters.
				cl.Get("/clusters/{id}/resources/*", resourceAPI.list)
				cl.Get("/clusters/{id}/stream/*", resourceAPI.streamResources)
				cl.Get("/clusters/{id}/object/*", resourceAPI.get)
				cl.Get("/clusters/{id}/health", resourceAPI.clusterHealth)
				cl.Get("/clusters/{id}/helm-releases", resourceAPI.listHelmReleases)
				cl.Get("/clusters/{id}/helm-releases/{namespace}/{name}", resourceAPI.helmRelease)
				cl.Get("/clusters/{id}/metrics", resourceAPI.clusterMetrics)
				cl.Get("/clusters/{id}/secret/{namespace}/{name}/keys", resourceAPI.secretKeys)
				// Disclosure is its own route so it is its own audit record; listing a
				// secret's keys is not the same act as reading one of its values.
				cl.Get("/clusters/{id}/secret/{namespace}/{name}/reveal", resourceAPI.revealSecret)
				cl.Get("/fleet/health", resourceAPI.fleetHealth)
				cl.Get("/search", resourceAPI.search)
				cl.Get("/clusters/{id}/pod/{namespace}/{name}/containers", resourceAPI.podContainers)
				cl.Get("/clusters/{id}/pod/{namespace}/{name}/restarts", resourceAPI.podRestarts)
				cl.Get("/clusters/{id}/pod/{namespace}/{name}/logs", resourceAPI.podLogs)
				cl.Get("/clusters/{id}/describe/*", resourceAPI.describe)
				cl.Get("/clusters/{id}/relations/*", resourceAPI.relations)
				cl.Get("/clusters/{id}/rollout/{namespace}/{name}", resourceAPI.rolloutHistory)
				cl.Get("/clusters/{id}/drain-plan/{name}", resourceAPI.planDrain)
			})

			// Writing to a cluster. Every route here runs the same pre-flight chain
			// (ADR-068): global kill switch, role, the cluster's read-only lock, then
			// the cluster's own answer via SelfSubjectAccessReview.
			authed.With(requirePermission(rbac.PermClusterWrite)).Group(func(cl chi.Router) {
				cl.Post("/clusters/{id}/apply", resourceAPI.applyObject)
				cl.Post("/clusters/{id}/apply/*", resourceAPI.applyObject)
				cl.Delete("/clusters/{id}/object/*", resourceAPI.deleteObject)
				cl.Post("/clusters/{id}/scale", resourceAPI.scale)
				cl.Post("/clusters/{id}/restart", resourceAPI.restart)
				cl.Post("/clusters/{id}/evict", resourceAPI.evict)
				cl.Post("/clusters/{id}/cronjob/suspend", resourceAPI.suspendCronJob)
				cl.Post("/clusters/{id}/cronjob/trigger", resourceAPI.triggerCronJob)
				cl.Post("/clusters/{id}/node/cordon", resourceAPI.cordonNode)
				cl.Post("/clusters/{id}/node/drain", resourceAPI.drainNode)
				cl.Post("/clusters/{id}/rollback", resourceAPI.rollback)

				// Interactive sessions. A shell is a write to the cluster in every sense
				// that matters, so it sits behind the same gate as one.
				cl.Get("/clusters/{id}/pod/{namespace}/{name}/shell", resourceAPI.podShell)
				cl.Get("/clusters/{id}/pod/{namespace}/{name}/debug", resourceAPI.debugShell)
				cl.Get("/clusters/{id}/node/{name}/shell", resourceAPI.nodeShell)
				cl.Get("/clusters/{id}/terminal", resourceAPI.clusterTerminal)
				cl.Get("/clusters/{id}/forward/{namespace}/{name}", resourceAPI.portForward)

				// A forward the browser can actually use: Kubby holds the tunnel and
				// serves the pod's own pages under a path of its own.
				cl.Get("/clusters/{id}/ports/{namespace}/{name}", resourceAPI.listPorts)
				cl.Get("/clusters/{id}/forwards", resourceAPI.listForwards)
				cl.Post("/clusters/{id}/forwards", resourceAPI.startForward)
				cl.Delete("/forwards/{forwardId}", resourceAPI.stopForward)
				cl.Handle("/forward/{forwardId}/*", http.HandlerFunc(resourceAPI.serveForward))
				cl.Handle("/forward/{forwardId}", http.HandlerFunc(resourceAPI.serveForward))
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

			// Deployment-wide settings. Only an admin holds PermSettingsWrite, and the
			// read is behind the same gate: knowing where the audit trail is shipped is
			// itself worth restricting.
			authed.With(requirePermission(rbac.PermSettingsWrite)).Group(func(st chi.Router) {
				st.Get("/settings", settingsAPI.read)
				st.Put("/settings/node-shell", settingsAPI.saveNodeShell)
				st.Put("/settings/pod-debug", settingsAPI.savePodDebug)
				st.Put("/settings/metrics", settingsAPI.saveMetrics)
				st.Put("/settings/audit-sink", settingsAPI.saveAuditSink)
			})
		})
	})

	mountSPA(r, d.WebFS, d.Logger)

	return &Server{Handler: r, limiters: []*rateLimiter{loginLimit, apiLimit}, shipper: shipper}
}
