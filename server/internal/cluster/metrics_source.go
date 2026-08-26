package cluster

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"k8s.io/client-go/rest"

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
// MetricsSource says where the endpoint came from.
//
// Worth reporting because the three are not interchangeable to whoever is reading the
// panel: an address somebody typed is theirs to fix, one Kubby found is a fact about the
// cluster, and the deployment-wide fallback may well be another cluster's numbers.
type MetricsSource struct {
	// Kind is "manual", "auto" or "default".
	Kind string
	// Where names it in the terms that make sense for the kind: namespace/service for a
	// discovered one, the host for an address that was configured.
	Where string
}

// MetricsClient builds a reader for this cluster's history, and says where it points.
func (s *Service) MetricsClient(ctx context.Context, cluster *store.Cluster) (*promql.Client, MetricsSource, error) {
	cfg, source, err := s.metricsConfigFor(ctx, cluster)
	if err != nil {
		return nil, source, err
	}
	client, err := promql.New(cfg)
	return client, source, err
}

// The order is deliberate. A typed address wins because somebody chose it; discovery
// comes next because it is about this cluster specifically; the deployment-wide endpoint
// is last because it is the only one that can be about a different cluster entirely.
func (s *Service) metricsConfigFor(ctx context.Context, cluster *store.Cluster) (promql.Config, MetricsSource, error) {
	if cluster.MetricsURL != "" {
		password, err := s.metricsPasswordFor(ctx, cluster)
		if err != nil {
			return promql.Config{}, MetricsSource{}, err
		}
		return promql.Config{
			URL:                cluster.MetricsURL,
			Username:           cluster.MetricsUsername,
			Password:           password,
			InsecureSkipVerify: cluster.MetricsInsecureSkipVerify,
		}, MetricsSource{Kind: "manual", Where: hostOf(cluster.MetricsURL)}, nil
	}

	if found, ok := s.discoveredMetricsFor(ctx, cluster); ok {
		cfg, err := s.RESTConfigFor(ctx, cluster, nil)
		if err != nil {
			return promql.Config{}, MetricsSource{}, err
		}
		httpClient, err := rest.HTTPClientFor(cfg)
		if err != nil {
			return promql.Config{}, MetricsSource{}, fmt.Errorf("build an authenticated client: %w", err)
		}
		return promql.Config{URL: found.url, HTTPClient: httpClient},
			MetricsSource{Kind: "auto", Where: found.namespace + "/" + found.service}, nil
	}

	if s.metricsDefaults == nil {
		return promql.Config{}, MetricsSource{}, promql.ErrNotConfigured
	}
	defaults, err := s.metricsDefaults(ctx)
	if err != nil {
		return promql.Config{}, MetricsSource{}, err
	}
	if !defaults.Enabled || defaults.URL == "" {
		return promql.Config{}, MetricsSource{}, promql.ErrNotConfigured
	}
	return promql.Config{
		URL:                defaults.URL,
		Username:           defaults.Username,
		Password:           defaults.Password,
		InsecureSkipVerify: defaults.InsecureSkipVerify,
	}, MetricsSource{Kind: "default", Where: hostOf(defaults.URL)}, nil
}

// discoveredMetricsFor answers from the cache, searching the cluster only when it has to.
//
// A failed search is remembered as "none", not dropped: without that, every dashboard
// refresh on a cluster without Prometheus would list every Service in it again.
func (s *Service) discoveredMetricsFor(ctx context.Context, cluster *store.Cluster) (discoveredMetrics, bool) {
	id := cluster.ID.String()
	if entry, ok := s.metricsDiscovery.get(id); ok {
		return entry, entry.found
	}

	found, err := s.discoverMetrics(ctx, cluster)
	if err != nil {
		// Unreachable clusters are not remembered as having no Prometheus: the next
		// request should try again rather than inherit an answer about a network blip.
		return discoveredMetrics{}, false
	}

	s.metricsDiscovery.put(id, found)
	return found, found.found
}

func hostOf(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return raw
	}
	return parsed.Host
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
func (s *Service) ClusterHealthMetrics(ctx context.Context, cluster *store.Cluster, window time.Duration) (*promql.ClusterHealth, MetricsSource, error) {
	client, source, err := s.MetricsClient(ctx, cluster)
	if err != nil {
		return nil, source, err
	}
	return promql.ReadClusterHealth(ctx, client, window), source, nil
}
