package cluster

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/erolbeyaz/kubby/internal/promql"
	"github.com/erolbeyaz/kubby/internal/store"
)

// A cluster whose credential cannot be resolved, so discovery fails without a database
// behind it. That is the point of these tests: the order the three sources are tried in
// is a decision worth pinning, and it does not need a cluster to check.
func unreachable(metricsURL string) *store.Cluster {
	return &store.Cluster{
		ID: uuid.New(), Name: "somewhere", AuthSource: "in-cluster", MetricsURL: metricsURL,
	}
}

func defaults(url string) MetricsDefaultsFunc {
	return func(context.Context) (MetricsDefaults, error) {
		return MetricsDefaults{Enabled: url != "", URL: url}, nil
	}
}

// The address somebody typed wins. Discovery is a convenience, and quietly preferring
// what Kubby found over what an operator chose would make the setting a lie.
func TestATypedAddressWinsOverDiscovery(t *testing.T) {
	s := (&Service{}).WithMetricsDefaults(defaults("http://central:9090"))

	cfg, source, err := s.metricsConfigFor(context.Background(), unreachable("http://typed:9090"))
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	if cfg.URL != "http://typed:9090" {
		t.Errorf("used %q instead of the address on the cluster", cfg.URL)
	}
	if source.Kind != "manual" {
		t.Errorf("source is %q, not manual", source.Kind)
	}
	if source.Where != "typed:9090" {
		t.Errorf("reported %q", source.Where)
	}
}

// With nothing typed and nothing discoverable, the deployment-wide endpoint is the last
// resort — and it is reported as such, because it may be answering about another cluster.
func TestTheDeploymentEndpointIsLastAndSaysSo(t *testing.T) {
	s := (&Service{}).WithMetricsDefaults(defaults("http://central:9090"))

	cfg, source, err := s.metricsConfigFor(context.Background(), unreachable(""))
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	if cfg.URL != "http://central:9090" {
		t.Errorf("used %q", cfg.URL)
	}
	if source.Kind != "default" {
		t.Errorf("source is %q, not default", source.Kind)
	}
}

// Nothing typed, nothing found, no fallback: a normal cluster, reported as a state.
func TestNoEndpointAnywhereIsNotAnError(t *testing.T) {
	s := &Service{}

	_, _, err := s.metricsConfigFor(context.Background(), unreachable(""))
	if !errors.Is(err, promql.ErrNotConfigured) {
		t.Fatalf("expected ErrNotConfigured, got %v", err)
	}
}

// A cluster Kubby could not reach must not be remembered as having no Prometheus.
//
// Without this, one network blip during a search would blank the dashboard for the next
// five minutes and the reader would have no way to tell that from an empty cluster.
func TestAFailedSearchIsNotRememberedAsAbsence(t *testing.T) {
	s := &Service{}
	c := unreachable("")

	if _, ok := s.discoveredMetricsFor(context.Background(), c); ok {
		t.Fatal("an unreachable cluster reported a discovered endpoint")
	}
	if _, cached := s.metricsDiscovery.get(c.ID.String()); cached {
		t.Error("a failed search was cached; the next request would inherit it")
	}
}

// Clearing the address has to take effect now, not in half an hour.
func TestForgettingMakesTheNextReadSearchAgain(t *testing.T) {
	s := &Service{}
	id := uuid.New().String()

	s.metricsDiscovery.put(id, discoveredMetrics{url: "http://old:9090", found: true})
	if _, ok := s.metricsDiscovery.get(id); !ok {
		t.Fatal("the cache did not keep what it was given")
	}

	s.ForgetDiscoveredMetrics(id)
	if _, ok := s.metricsDiscovery.get(id); ok {
		t.Error("the remembered endpoint survived being forgotten")
	}
}
