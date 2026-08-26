package cluster

import (
	"context"
	"os"
	"testing"
	"time"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/erolbeyaz/kubby/internal/promql"
)

// Discovery can only be proved against a real cluster. Name a context to enable it:
//
//	KUBBY_TEST_CONTEXT=kubby-mini go test -run TestDiscovery ./internal/cluster
//
// A fake client would show the label selector matches a fixture, which is not the claim.
// The claim is that a Prometheus nobody configured can be found and queried through the
// API server with the credential Kubby already holds.
func liveRESTConfig(t *testing.T) *rest.Config {
	t.Helper()

	name := os.Getenv("KUBBY_TEST_CONTEXT")
	if name == "" {
		t.Skip("KUBBY_TEST_CONTEXT is not set; skipping live discovery tests")
	}

	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(),
		&clientcmd.ConfigOverrides{CurrentContext: name}).ClientConfig()
	if err != nil {
		t.Fatalf("read the kubeconfig for context %q: %v", name, err)
	}
	return cfg
}

func TestDiscoveryFindsPrometheusNobodyConfigured(t *testing.T) {
	cfg := liveRESTConfig(t)

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()

	found, err := discoverPrometheus(ctx, cfg)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if !found.found {
		t.Fatal("no Prometheus was found; install one or point KUBBY_TEST_CONTEXT elsewhere")
	}
	t.Logf("found %s/%s at %s", found.namespace, found.service, found.url)

	// Found is not enough: the endpoint has to answer the queries the dashboard runs.
	client, err := promqlThroughAPIServer(cfg, found.url)
	if err != nil {
		t.Fatalf("build a client for the discovered endpoint: %v", err)
	}

	health := promql.ReadClusterHealth(ctx, client, time.Hour)
	if health.Nodes.Total == 0 {
		t.Error("the discovered endpoint reports no nodes at all, so it is answering about nothing")
	}
	if len(health.CPU) == 0 {
		t.Error("no CPU history came back, so the range queries are not working through the proxy")
	}
	t.Logf("pods=%+v nodes=%+v cpuPoints=%d", health.Pods, health.Nodes, len(health.CPU))
}

// A cluster with no Prometheus must be an ordinary answer, not an error: that is what
// lets the panel step aside quietly rather than showing a failure.
func TestDiscoveryReportsAbsenceWithoutFailing(t *testing.T) {
	cfg := liveRESTConfig(t)

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()

	// A namespace label nothing carries, standing in for a cluster without Prometheus.
	saved := prometheusSelectors
	prometheusSelectors = []string{"app.kubernetes.io/name=not-a-real-prometheus"}
	defer func() { prometheusSelectors = saved }()

	found, err := discoverPrometheus(ctx, cfg)
	if err != nil {
		t.Fatalf("absence should not be an error: %v", err)
	}
	if found.found {
		t.Fatal("something matched a selector that should match nothing")
	}
}
