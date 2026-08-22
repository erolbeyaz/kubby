package cluster

import (
	"context"
	"log/slog"
	"time"

	"github.com/erolbeyaz/kubby/internal/audit"
	"github.com/erolbeyaz/kubby/internal/store"
)

// Monitor re-probes registered clusters on a schedule.
//
// Without it a revoked token is only discovered when someone opens the cluster, which
// is the worst moment to find out. Probing in the background means the list already
// says what is wrong before anyone goes looking (ADR-018).
type Monitor struct {
	svc      *Service
	clusters *store.ClusterRepo
	audit    *audit.Emitter
	logger   *slog.Logger
	interval time.Duration
}

func NewMonitor(svc *Service, db *store.DB, auditLog *audit.Emitter, logger *slog.Logger, interval time.Duration) *Monitor {
	return &Monitor{
		svc:      svc,
		clusters: db.Clusters(),
		audit:    auditLog,
		logger:   logger.With(slog.String("component", "cluster-monitor")),
		interval: interval,
	}
}

// Run probes every cluster until the context is cancelled. It returns only on
// cancellation, so the caller can wait for a clean stop during shutdown.
func (m *Monitor) Run(ctx context.Context) {
	if m.interval <= 0 {
		m.logger.Info("cluster monitoring is disabled")
		return
	}
	m.logger.Info("cluster monitoring started", slog.Duration("interval", m.interval))

	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	// A first sweep on startup: after a restart the stored status may be stale.
	m.sweep(ctx)

	for {
		select {
		case <-ctx.Done():
			m.logger.Info("cluster monitoring stopped")
			return
		case <-ticker.C:
			m.sweep(ctx)
		}
	}
}

// sweep probes each cluster in turn. Clusters are checked sequentially: a handful of
// clusters is the expected scale, and probing them in parallel would produce a burst of
// connections for no meaningful gain (ADR-019).
func (m *Monitor) sweep(ctx context.Context) {
	clusters, err := m.clusters.List(ctx)
	if err != nil {
		m.logger.ErrorContext(ctx, "could not list clusters to probe", slog.String("error", err.Error()))
		return
	}

	for _, cluster := range clusters {
		if ctx.Err() != nil {
			return
		}
		m.probe(ctx, cluster)
	}
}

func (m *Monitor) probe(ctx context.Context, cluster *store.Cluster) {
	previous := cluster.CredentialStatus

	updated, err := m.svc.Refresh(ctx, cluster)
	if err != nil {
		m.logger.ErrorContext(ctx, "cluster probe failed",
			slog.String("cluster", cluster.Name), slog.String("error", err.Error()))
		return
	}
	if updated.CredentialStatus == previous {
		return
	}

	m.logger.InfoContext(ctx, "cluster status changed",
		slog.String("cluster", updated.Name),
		slog.String("from", previous),
		slog.String("to", updated.CredentialStatus),
		slog.String("detail", updated.StatusDetail),
	)

	// A credential that stops working is a security-relevant event: it may be an
	// expiry, but it may equally be a revocation someone should know about.
	if updated.CredentialStatus == store.CredentialInvalid {
		m.audit.Record(ctx, audit.Event{
			Action:       audit.ActionClusterCredentialInvalid,
			Result:       audit.ResultError,
			ClusterID:    &updated.ID,
			ResourceKind: "Cluster",
			ResourceName: updated.Name,
			Details:      map[string]any{"detail": updated.StatusDetail, "previous": previous},
		})
	}
}
