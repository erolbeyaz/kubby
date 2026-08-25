package cluster

import (
	"context"
	"log/slog"
	"time"

	"github.com/erolbeyaz/kubby/internal/store"
)

// NodeShellSweeper deletes privileged shell pods nothing is attached to.
//
// The session's own deferred cleanup handles the ordinary case. This is for the ones it
// cannot reach: the process was killed, the node went away mid-session, the API server
// refused the delete once. A privileged pod with the host's namespaces is a hole this
// tool opened, and leaving it open because a cleanup path failed is not acceptable.
type NodeShellSweeper struct {
	svc      *Service
	clusters *store.ClusterRepo
	settings NodeShellNamespaceSource
	logger   *slog.Logger
	interval time.Duration
	// maxAge is how long a shell may live before it is considered abandoned. It is well
	// past any real session: a reader at a prompt is not the target here.
	maxAge time.Duration
}

// NodeShellNamespaceSource supplies the namespace shells are started in, which an admin
// can change while the process is running.
type NodeShellNamespaceSource interface {
	NodeShellNamespace(ctx context.Context) (string, bool, error)
}

func NewNodeShellSweeper(svc *Service, clusters *store.ClusterRepo, settings NodeShellNamespaceSource, logger *slog.Logger) *NodeShellSweeper {
	return &NodeShellSweeper{
		svc:      svc,
		clusters: clusters,
		settings: settings,
		logger:   logger,
		interval: 5 * time.Minute,
		maxAge:   4 * time.Hour,
	}
}

func (s *NodeShellSweeper) Run(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.sweep(ctx)
		}
	}
}

func (s *NodeShellSweeper) sweep(ctx context.Context) {
	namespace, enabled, err := s.settings.NodeShellNamespace(ctx)
	if err != nil {
		s.logger.Warn("node shell sweep skipped", slog.String("error", err.Error()))
		return
	}
	// Turned off now does not mean nothing was left behind while it was on, so the sweep
	// runs either way; only a missing namespace makes it meaningless.
	_ = enabled
	if namespace == "" {
		return
	}

	clusters, err := s.clusters.List(ctx)
	if err != nil {
		s.logger.Warn("node shell sweep skipped", slog.String("error", err.Error()))
		return
	}

	for _, c := range clusters {
		removed, err := s.svc.SweepNodeShells(ctx, c, namespace, s.maxAge, nil)
		if err != nil {
			// An unreachable cluster is the monitor's problem to report, not this one's.
			s.logger.Debug("node shell sweep failed",
				slog.String("cluster", c.Name), slog.String("error", err.Error()))
			continue
		}
		if removed > 0 {
			s.logger.Info("removed abandoned node shells",
				slog.String("cluster", c.Name), slog.Int("count", removed))
		}
	}
}
