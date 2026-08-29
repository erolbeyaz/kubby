// Command kubby runs the Kubby server.
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/erolbeyaz/kubby/internal/audit"
	"github.com/erolbeyaz/kubby/internal/auth"
	"github.com/erolbeyaz/kubby/internal/cluster"
	"github.com/erolbeyaz/kubby/internal/config"
	"github.com/erolbeyaz/kubby/internal/crypto"
	"github.com/erolbeyaz/kubby/internal/httpapi"
	"github.com/erolbeyaz/kubby/internal/logging"
	"github.com/erolbeyaz/kubby/internal/metrics"
	"github.com/erolbeyaz/kubby/internal/settings"
	"github.com/erolbeyaz/kubby/internal/store"
	"github.com/erolbeyaz/kubby/internal/webassets"
)

const shutdownTimeout = 15 * time.Second

func main() {
	healthcheck := flag.Bool("healthcheck", false, "probe the local /healthz endpoint and exit")
	flag.Parse()

	// The distroless runtime image has no shell or curl, so the container healthcheck
	// re-executes this binary instead.
	if *healthcheck {
		if err := probeHealth(); err != nil {
			fmt.Fprintf(os.Stderr, "kubby: healthcheck failed: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if err := run(); err != nil {
		// Configuration and startup failures happen before the logger exists, so they
		// go to stderr in plain text where an operator will actually see them.
		fmt.Fprintf(os.Stderr, "kubby: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logger := logging.New(cfg.Logging.Level, os.Stdout)
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if cfg.HTTP.ExtraCABundle != "" {
		if err := installExtraCABundle(cfg.HTTP.ExtraCABundle); err != nil {
			return err
		}
		logger.Info("corporate CA bundle installed", slog.String("path", cfg.HTTP.ExtraCABundle))
	}

	db, err := store.Open(ctx, cfg.DB)
	if err != nil {
		return err
	}
	defer db.Close()
	logger.Info("database connected", slog.String("target", cfg.DB.Redacted()))

	// Before anything is served. A server that starts on an unmigrated database answers
	// its health probes and then fails every real request with a missing table, which
	// looks like a Kubby bug rather than a schema that was never applied.
	if err := store.Migrate(ctx, cfg.DB.DSN(), logger); err != nil {
		return err
	}

	webFS, err := webassets.FS()
	if err != nil {
		return fmt.Errorf("load embedded frontend: %w", err)
	}

	keyring, err := crypto.NewKeyring(cfg.Crypto.EncryptionKeyVersion, cfg.Crypto.EncryptionKey)
	if err != nil {
		return fmt.Errorf("initialise encryption keyring: %w", err)
	}

	authService := auth.NewService(db, keyring, auth.Settings{
		SessionTTL:         cfg.Auth.SessionTTL,
		RefreshTTL:         cfg.Auth.RefreshTTL,
		LoginMaxAttempts:   cfg.Auth.LoginMaxAttempts,
		LockoutDurations:   cfg.Auth.LockoutDurations,
		MaxLockouts:        cfg.Auth.MaxLockouts,
		RequireMFAForAdmin: cfg.Auth.RequireMFAForAdmin,
		Argon2:             auth.DefaultArgon2Params(cfg.Auth.Argon2MemoryMiB),
		Issuer:             cfg.Auth.MFAIssuer,
	})

	informerPool := cluster.NewInformerPool(cfg.K8s.InformerIdleTTL, logger)
	defer informerPool.Close()

	clusterService := cluster.NewService(db, keyring, cluster.Settings{
		DefaultQPS:     cfg.K8s.QPS,
		DefaultBurst:   cfg.K8s.Burst,
		Timeout:        cfg.K8s.Timeout,
		AllowLoopback:  cfg.K8s.AllowLoopbackClusters,
		AllowInCluster: cfg.K8s.AllowInCluster,
	}).WithInformerPool(informerPool)

	registry := metrics.New()

	settingsService := settings.New(db.Settings(), keyring)

	// The SIEM copy is started and closed by the server, so it exists wherever a router
	// does rather than only in this one call site.
	auditLog := audit.New(db.Audit(), logger)

	monitor := cluster.NewMonitor(clusterService, db, auditLog, logger, cfg.K8s.HealthCheckInterval)
	monitorDone := make(chan struct{})
	go func() {
		defer close(monitorDone)
		monitor.Run(ctx)
	}()

	// A privileged shell pod that outlived its session is a hole this tool opened, so a
	// sweep runs regardless of whether any session ended politely.
	shellSweeper := cluster.NewNodeShellSweeper(clusterService, db.Clusters(), settingsService, logger)
	go shellSweeper.Run(ctx)

	// What the pods are saying about themselves. On a schedule so the answer is already
	// on the row when a list is drawn, rather than a query in front of every reader.
	logSweeper := cluster.NewLogSweeper(clusterService, db, logger, time.Minute,
		settingsService.LogAnalysisConfig)
	go logSweeper.Run(ctx)

	server := httpapi.New(httpapi.Deps{
		Config:  cfg,
		Logger:  logger,
		DB:      db,
		Store:   db,
		Auth:    authService,
		Cluster: clusterService,
		Audit:   auditLog,
		Metrics: registry,
		Keyring: keyring,
		WebFS:   webFS,
		Logs:    logSweeper,
	})
	defer server.Close()

	handler := server.Handler

	srv := &http.Server{
		Addr:              cfg.HTTP.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		// No WriteTimeout: log streaming and exec sessions are long-lived (Faz 5, Faz 8).
	}

	if cfg.HTTP.TLSEnabled() {
		srv.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("server listening",
			slog.String("addr", cfg.HTTP.Addr),
			slog.String("public_url", cfg.HTTP.PublicURL.String()),
			slog.Bool("tls", cfg.HTTP.TLSEnabled()),
			slog.Bool("read_only", cfg.HTTP.ReadOnly),
			slog.String("version", httpapi.Version),
			slog.String("commit", httpapi.CommitSHA),
		)

		var serveErr error
		if cfg.HTTP.TLSEnabled() {
			serveErr = srv.ListenAndServeTLS(cfg.HTTP.TLSCertFile, cfg.HTTP.TLSKeyFile)
		} else {
			serveErr = srv.ListenAndServe()
		}
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			errCh <- serveErr
		}
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("http server: %w", err)
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}

	// The monitor watches the same context, so it is already unwinding; waiting keeps
	// an in-flight probe from being cut off mid-write.
	select {
	case <-monitorDone:
	case <-shutdownCtx.Done():
		logger.Warn("cluster monitor did not stop in time")
	}

	logger.Info("shutdown complete")
	return nil
}

// installExtraCABundle appends the corporate root CA to the default TLS trust store so
// every outbound client (Kubernetes, Elasticsearch, OIDC) trusts internally signed
// endpoints. The system pool is extended, never replaced.
func installExtraCABundle(path string) error {
	pem, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read CA bundle %q: %w", path, err)
	}

	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	if !pool.AppendCertsFromPEM(pem) {
		return fmt.Errorf("CA bundle %q contains no valid PEM certificate", path)
	}

	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return errors.New("default HTTP transport has an unexpected type; cannot install CA bundle")
	}
	if transport.TLSClientConfig == nil {
		transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	transport.TLSClientConfig.RootCAs = pool
	return nil
}

// probeHealth performs the container healthcheck against the local listener.
func probeHealth() error {
	addr := os.Getenv("KUBBY_HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("parse KUBBY_HTTP_ADDR %q: %w", addr, err)
	}

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://127.0.0.1:" + port + "/healthz")
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return nil
}
