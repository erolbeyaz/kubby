package cluster

import (
	"context"
	"fmt"
	"time"

	"github.com/erolbeyaz/kubby/internal/promql"
	"github.com/erolbeyaz/kubby/internal/store"
)

// MetricsDefaults is the deployment-wide endpoint, used by any cluster that names none of
// its own. That is what a central Prometheus or a Thanos query layer looks like.
type MetricsDefaults struct {
	Enabled            bool
	URL                string
	Username           string
	Password           string
	InsecureSkipVerify bool
}

// MetricsDefaultsFunc supplies them at the moment they are needed, so an admin's change
// takes effect without a restart.
type MetricsDefaultsFunc func(ctx context.Context) (MetricsDefaults, error)

// WithMetricsDefaults wires the deployment-wide fallback.
//
// A nil receiver is accepted: the router is built without a cluster service in tests that
// only exercise the health endpoints, and wiring an optional fallback is not a reason for
// those to crash.
func (s *Service) WithMetricsDefaults(fn MetricsDefaultsFunc) *Service {
	if s == nil {
		return nil
	}
	s.metricsDefaults = fn
	return s
}

// MetricsClient builds a reader for this cluster's history.
//
// The cluster's own endpoint wins over the deployment's. Prometheus is normally deployed
// into the cluster it observes, so a fleet means one endpoint per cluster; answering
// every cluster from one address would show one cluster's numbers under every name,
// which is worse than showing nothing.
func (s *Service) MetricsClient(ctx context.Context, cluster *store.Cluster) (*promql.Client, error) {
	cfg, err := s.metricsConfigFor(ctx, cluster)
	if err != nil {
		return nil, err
	}
	return promql.New(cfg)
}

func (s *Service) metricsConfigFor(ctx context.Context, cluster *store.Cluster) (promql.Config, error) {
	if cluster.MetricsURL != "" {
		password, err := s.metricsPasswordFor(ctx, cluster)
		if err != nil {
			return promql.Config{}, err
		}
		return promql.Config{
			URL:                cluster.MetricsURL,
			Username:           cluster.MetricsUsername,
			Password:           password,
			InsecureSkipVerify: cluster.MetricsInsecureSkipVerify,
		}, nil
	}

	if s.metricsDefaults == nil {
		return promql.Config{}, promql.ErrNotConfigured
	}
	defaults, err := s.metricsDefaults(ctx)
	if err != nil {
		return promql.Config{}, err
	}
	if !defaults.Enabled || defaults.URL == "" {
		return promql.Config{}, promql.ErrNotConfigured
	}
	return promql.Config{
		URL:                defaults.URL,
		Username:           defaults.Username,
		Password:           defaults.Password,
		InsecureSkipVerify: defaults.InsecureSkipVerify,
	}, nil
}

func (s *Service) metricsPasswordFor(ctx context.Context, cluster *store.Cluster) (string, error) {
	if cluster.MetricsUsername == "" {
		return "", nil
	}

	sealed, err := s.clusters.MetricsPassword(ctx, cluster.ID)
	if err != nil {
		return "", err
	}
	if len(sealed) == 0 {
		return "", nil
	}

	// Bound to the row exactly as the kubeconfig is, so a ciphertext moved between rows
	// does not decrypt.
	plain, err := s.keyring.Open(sealed, metricsAAD(cluster.ID.String()))
	if err != nil {
		return "", fmt.Errorf("decrypt the metrics password: %w", err)
	}
	return string(plain), nil
}

// SealMetricsPassword prepares one for storage.
func (s *Service) SealMetricsPassword(clusterID, password string) ([]byte, error) {
	if password == "" {
		return nil, nil
	}
	return s.keyring.Seal([]byte(password), metricsAAD(clusterID))
}

func metricsAAD(clusterID string) []byte {
	return []byte("cluster:" + clusterID + ":metrics-password")
}

// ClusterHealthMetrics reads the panel's whole set for one cluster.
func (s *Service) ClusterHealthMetrics(ctx context.Context, cluster *store.Cluster, window time.Duration) (*promql.ClusterHealth, error) {
	client, err := s.MetricsClient(ctx, cluster)
	if err != nil {
		return nil, err
	}
	return promql.ReadClusterHealth(ctx, client, window), nil
}
