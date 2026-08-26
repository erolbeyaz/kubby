package cluster

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"

	"github.com/erolbeyaz/kubby/internal/promql"
	"github.com/erolbeyaz/kubby/internal/store"
)

// Finding Prometheus without being told where it is.
//
// Asking an operator to type an address per cluster does not scale past the first one:
// every cluster registered later arrives with an empty field and a blank dashboard, and
// the address they would type is usually a NodePort that only resolves from wherever they
// happened to be standing. Kubby already holds a credential for the cluster, and
// Prometheus is nearly always running inside it, so it can look.
//
// Reached through the API server's service proxy rather than dialled directly. That is
// the one route guaranteed to work everywhere Kubby works: no NodePort, no ingress, no
// route from Kubby's network to the cluster's pod network, and it is authenticated by the
// same credential the rest of the cluster's traffic uses.

// The label every Prometheus chart puts on the server's Service. kube-state-metrics and
// node-exporter carry their own names, so this does not match them.
var prometheusSelectors = []string{
	"app.kubernetes.io/name=prometheus",
	// Charts predating the recommended labels, still widely deployed.
	"app=prometheus",
}

// A Service can expose several ports; these are the ones that answer PromQL.
func promPort(svc *corev1.Service) (name string, port int32, ok bool) {
	for _, p := range svc.Spec.Ports {
		switch {
		case p.Name == "http-web", p.Name == "web", p.Name == "http", p.Port == 9090, p.Port == 80:
			return p.Name, p.Port, true
		}
	}
	return "", 0, false
}

// discoveredMetrics is what a successful search found, kept so the next request does not
// repeat it.
type discoveredMetrics struct {
	url       string
	namespace string
	service   string
	found     bool
	at        time.Time
}

type metricsDiscoveryCache struct {
	mu sync.Mutex
	by map[string]discoveredMetrics
}

// A found endpoint is re-checked rarely; a cluster with no Prometheus is re-checked often
// enough that installing one shows up without a restart, and rarely enough that a fleet
// of clusters without Prometheus does not list Services every thirty seconds.
const (
	discoveryTTLFound    = 30 * time.Minute
	discoveryTTLNotFound = 5 * time.Minute
)

func (c *metricsDiscoveryCache) get(id string) (discoveredMetrics, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.by[id]
	if !ok {
		return discoveredMetrics{}, false
	}
	ttl := discoveryTTLNotFound
	if entry.found {
		ttl = discoveryTTLFound
	}
	if time.Since(entry.at) > ttl {
		return discoveredMetrics{}, false
	}
	return entry, true
}

func (c *metricsDiscoveryCache) put(id string, entry discoveredMetrics) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.by == nil {
		c.by = make(map[string]discoveredMetrics)
	}
	entry.at = time.Now()
	c.by[id] = entry
}

// Forget drops what was found for a cluster, so the next read looks again.
//
// Called when the cluster's metrics settings change: an operator who has just cleared a
// manual address is asking Kubby to go and look, and being told to wait half an hour
// would read as the change not having worked.
func (c *metricsDiscoveryCache) Forget(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.by, id)
}

// discoverMetrics looks for a Prometheus inside the cluster and proves it is one.
//
// Every candidate is queried before it is accepted. A Service named prometheus is not
// evidence: it may be a stale record, an operator's headless Service with no endpoints,
// or something else entirely wearing the label. Answering `vector(1)` is evidence, and it
// costs one request.
func (s *Service) discoverMetrics(ctx context.Context, cluster *store.Cluster) (discoveredMetrics, error) {
	cfg, err := s.RESTConfigFor(ctx, cluster, nil)
	if err != nil {
		return discoveredMetrics{}, fmt.Errorf("connect to the cluster: %w", err)
	}
	return discoverPrometheus(ctx, cfg)
}

// discoverPrometheus is the search itself, separated from resolving the credential so it
// can be run against a real cluster in a test without a database behind it.
func discoverPrometheus(ctx context.Context, cfg *rest.Config) (discoveredMetrics, error) {
	clientset, err := Clientset(cfg)
	if err != nil {
		return discoveredMetrics{}, fmt.Errorf("build a client: %w", err)
	}

	// Bounded: discovery is a background convenience and must never be the reason a
	// dashboard hangs.
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	seen := make(map[string]bool)
	for _, selector := range prometheusSelectors {
		// Across all namespaces in one call rather than guessing at namespace names:
		// `monitoring` is a convention, not a rule.
		services, err := clientset.CoreV1().Services("").List(ctx, metav1.ListOptions{
			LabelSelector: selector,
			Limit:         100,
		})
		if err != nil {
			// A cluster that refuses to list Services cannot be searched. That is a
			// normal outcome for a tightly scoped credential, not a failure to report.
			continue
		}

		for i := range services.Items {
			svc := &services.Items[i]
			key := svc.Namespace + "/" + svc.Name
			if seen[key] {
				continue
			}
			seen[key] = true

			portName, port, ok := promPort(svc)
			if !ok {
				continue
			}

			candidate := proxyURL(cfg.Host, svc.Namespace, svc.Name, portName, port)
			if answersPromQL(ctx, cfg, candidate) {
				return discoveredMetrics{
					url: candidate, namespace: svc.Namespace, service: svc.Name, found: true,
				}, nil
			}
		}
	}

	return discoveredMetrics{found: false}, nil
}

// proxyURL builds the API server's route to a Service port.
//
// The port is named where the Service names it: the API server accepts a number, but a
// name survives a chart that renumbers its ports.
func proxyURL(apiServer, namespace, service, portName string, port int32) string {
	target := portName
	if target == "" {
		target = fmt.Sprint(port)
	}

	// `http:` is the scheme the API server should use to reach the Service, not the
	// scheme Kubby uses to reach the API server.
	return strings.TrimSuffix(apiServer, "/") +
		"/api/v1/namespaces/" + url.PathEscape(namespace) +
		"/services/" + url.PathEscape("http:"+service+":"+target) + "/proxy"
}

// answersPromQL is the whole test: does a PromQL query come back from there.
func answersPromQL(ctx context.Context, cfg *rest.Config, endpoint string) bool {
	client, err := promqlThroughAPIServer(cfg, endpoint)
	if err != nil {
		return false
	}

	probe, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	return client.Ping(probe) == nil
}

// promqlThroughAPIServer builds a reader that speaks to Prometheus over the cluster's own
// authenticated connection.
func promqlThroughAPIServer(cfg *rest.Config, endpoint string) (*promql.Client, error) {
	httpClient, err := rest.HTTPClientFor(cfg)
	if err != nil {
		return nil, fmt.Errorf("build an authenticated client: %w", err)
	}
	return promql.New(promql.Config{URL: endpoint, HTTPClient: httpClient})
}

// ForgetDiscoveredMetrics drops what was found for a cluster so the next read looks again.
func (s *Service) ForgetDiscoveredMetrics(clusterID string) {
	if s == nil {
		return
	}
	s.metricsDiscovery.Forget(clusterID)
}
