package cluster_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/erolbeyaz/kubby/internal/cluster"
)

// seededCluster registers the local test cluster and returns it ready to list from.
func seededCluster(t *testing.T) (*cluster.Service, *storeCluster) {
	t.Helper()

	svc, db, owner := serviceHarness(t)
	raw := liveKubeconfig(t)

	created, err := svc.Create(context.Background(), cluster.CreateInput{
		Name: "resources-" + uuid.NewString()[:8], Environment: "test",
		Kubeconfig: raw, CreatedBy: owner,
	})
	if err != nil {
		t.Fatalf("Create cluster: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Pool().Exec(context.Background(), `DELETE FROM clusters WHERE id = $1`, created.ID)
	})
	return svc, created
}

func listOf(t *testing.T, svc *cluster.Service, c *storeCluster, key, namespace string) *cluster.ListResult {
	t.Helper()

	resourceType, err := cluster.LookupType(key)
	if err != nil {
		t.Fatalf("LookupType(%q): %v", key, err)
	}
	request := cluster.ListRequest{Type: resourceType}
	if namespace != "" {
		request.Namespaces = []string{namespace}
	}
	result, err := svc.List(context.Background(), c, request, nil)
	if err != nil {
		t.Fatalf("List %s: %v", key, err)
	}
	return result
}

func TestListReturnsProjectedRowsFromARealCluster(t *testing.T) {
	svc, c := seededCluster(t)

	t.Run("pods carry the fields a list needs", func(t *testing.T) {
		result := listOf(t, svc, c, "pods", "payments")
		if len(result.Rows) == 0 {
			t.Fatal("no pods were returned; is the test cluster seeded?")
		}

		row := result.Rows[0]
		for _, field := range []string{"ready", "status", "restarts", "node"} {
			if _, ok := row.Fields[field]; !ok {
				t.Errorf("pod row is missing %q: %+v", field, row.Fields)
			}
		}
		if row.Age == "" || row.Namespace != "payments" {
			t.Errorf("unexpected row: %+v", row)
		}
	})

	t.Run("deployments report readiness", func(t *testing.T) {
		result := listOf(t, svc, c, "apps/deployments", "payments")

		var found bool
		for _, row := range result.Rows {
			if row.Name == "payments-api" {
				found = true
				if row.Fields["ready"] != "3/3" {
					t.Errorf("ready = %q, want 3/3", row.Fields["ready"])
				}
			}
		}
		if !found {
			t.Error("payments-api was not listed")
		}
	})

	t.Run("namespace filtering is applied", func(t *testing.T) {
		result := listOf(t, svc, c, "pods", "storefront")
		for _, row := range result.Rows {
			if row.Namespace != "storefront" {
				t.Fatalf("a pod from %q leaked into a storefront listing", row.Namespace)
			}
		}
	})

	t.Run("cluster-scoped kinds list without a namespace", func(t *testing.T) {
		result := listOf(t, svc, c, "nodes", "")
		if len(result.Rows) != 3 {
			t.Errorf("got %d nodes, want 3", len(result.Rows))
		}
		if result.Rows[0].Fields["status"] != "Ready" {
			t.Errorf("node status = %q", result.Rows[0].Fields["status"])
		}
	})

	t.Run("a kind the cluster does not serve is reported clearly", func(t *testing.T) {
		httpRoutes, err := cluster.LookupType("gateway.networking.k8s.io/httproutes")
		if err != nil {
			t.Fatalf("LookupType: %v", err)
		}
		_, err = svc.List(context.Background(), c, cluster.ListRequest{Type: httpRoutes}, nil)
		if err == nil {
			t.Skip("this cluster serves Gateway API; nothing to assert")
		}
		if !strings.Contains(err.Error(), "not") {
			t.Errorf("unhelpful error for an unavailable kind: %v", err)
		}
	})
}

// The whole point of projecting server-side: none of the bulk ever reaches the client.
func TestListProjectionCarriesNoSecretsOrServerNoise(t *testing.T) {
	svc, c := seededCluster(t)

	t.Run("secret values never appear in a list", func(t *testing.T) {
		result := listOf(t, svc, c, "secrets", "payments")

		encoded, err := json.Marshal(result)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		payload := string(encoded)

		// The seeded secret's value, and its base64 form, must both be absent.
		for _, forbidden := range []string{"not-a-real-password-for-testing", "bm90LWEtcmVhbC1wYXNzd29yZC1mb3ItdGVzdGluZw=="} {
			if strings.Contains(payload, forbidden) {
				t.Fatal("a secret value was included in a resource listing")
			}
		}

		var found bool
		for _, row := range result.Rows {
			if row.Name == "api-credentials" {
				found = true
				// Key names are useful and not sensitive; values are not sent.
				if !strings.Contains(row.Fields["keys"], "password") {
					t.Errorf("secret row does not list its keys: %+v", row.Fields)
				}
			}
		}
		if !found {
			t.Error("the seeded secret was not listed at all")
		}
	})

	t.Run("server-side bookkeeping is stripped", func(t *testing.T) {
		result := listOf(t, svc, c, "apps/deployments", "payments")

		encoded, err := json.Marshal(result)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		payload := string(encoded)

		for _, noise := range []string{"managedFields", "last-applied-configuration", "fieldsV1"} {
			if strings.Contains(payload, noise) {
				t.Errorf("%q reached the client; the projection is not doing its job", noise)
			}
		}
	})
}

func TestGetReturnsOneObjectWithoutServerNoise(t *testing.T) {
	svc, c := seededCluster(t)

	deployments, err := cluster.LookupType("apps/deployments")
	if err != nil {
		t.Fatalf("LookupType: %v", err)
	}

	obj, err := svc.Get(context.Background(), c, deployments, "payments", "payments-api", nil)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if obj.GetName() != "payments-api" {
		t.Errorf("name = %q", obj.GetName())
	}
	if len(obj.GetManagedFields()) != 0 {
		t.Error("managedFields survived into the detail view")
	}
	if _, present := obj.GetAnnotations()["kubectl.kubernetes.io/last-applied-configuration"]; present {
		t.Error("the last-applied annotation survived into the detail view")
	}

	// The detail view is the one place the full spec is expected.
	if _, found, _ := unstructuredNested(obj.Object, "spec", "replicas"); !found {
		t.Error("the object does not carry its spec")
	}
}

func TestInformerCacheServesListsAndCanBeReleased(t *testing.T) {
	svc, c := seededCluster(t)

	pool := cluster.NewInformerPool(time.Minute, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	t.Cleanup(pool.Close)
	svc = svc.WithInformerPool(pool)

	pods, err := cluster.LookupType("pods")
	if err != nil {
		t.Fatalf("LookupType: %v", err)
	}

	// The first call warms the cache; the second should be served from it.
	if _, err := svc.List(context.Background(), c, cluster.ListRequest{Type: pods}, nil); err != nil {
		t.Fatalf("first list: %v", err)
	}

	var cached *cluster.ListResult
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		cached, err = svc.List(context.Background(), c, cluster.ListRequest{Type: pods}, nil)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if cached.FromCache {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	if !cached.FromCache {
		t.Fatal("the informer cache never served a listing")
	}
	if len(cached.Rows) == 0 {
		t.Fatal("the cached listing is empty")
	}

	t.Run("the pool reports what it holds", func(t *testing.T) {
		stats := svc.CacheStats()
		if len(stats) != 1 {
			t.Fatalf("got %d cached clusters, want 1", len(stats))
		}
		if stats[0].Objects == 0 || stats[0].Kinds == 0 {
			t.Errorf("cache stats look empty: %+v", stats[0])
		}
	})

	t.Run("releasing frees the cache", func(t *testing.T) {
		svc.ReleaseCache(c.ID)

		if stats := svc.CacheStats(); len(stats) != 0 {
			t.Errorf("cache still holds %d clusters after release", len(stats))
		}
	})
}

// Pods and nodes carry live usage when metrics-server is present, and degrade to an em
// dash rather than a zero when it is not (ADR-007).
func TestListIncludesUsageWhenMetricsAreAvailable(t *testing.T) {
	svc, c := seededCluster(t)

	if !c.MetricsAvailable {
		t.Skip("this cluster has no metrics API")
	}

	for _, tc := range []struct{ key, namespace string }{
		{"pods", "payments"},
		{"nodes", ""},
	} {
		t.Run(tc.key, func(t *testing.T) {
			result := listOf(t, svc, c, tc.key, tc.namespace)

			var hasColumn bool
			for _, column := range result.Columns {
				if column.Key == "cpu" {
					hasColumn = true
				}
			}
			if !hasColumn {
				t.Fatal("no CPU column was offered")
			}
			if len(result.Rows) == 0 {
				t.Fatal("nothing was listed")
			}

			row := result.Rows[0]
			if row.Fields["cpu"] == "" || row.Fields["memory"] == "" {
				t.Errorf("usage fields are absent: %+v", row.Fields)
			}
		})
	}
}

// A pod list has to answer "what made this and where is it" without opening anything.
func TestPodRowsCarryOwnershipAndPlacement(t *testing.T) {
	svc, c := seededCluster(t)

	result := listOf(t, svc, c, "pods", "payments")
	if len(result.Rows) == 0 {
		t.Fatal("no pods were listed")
	}

	byName := map[string]map[string]string{}
	for _, row := range result.Rows {
		byName[row.Name] = row.Fields
	}

	var checkedDeployment, checkedStatefulSet bool
	for name, fields := range byName {
		// A pod that was never scheduled has no node, and saying it does would be the
		// projection inventing one. Only what is running is asserted.
		if fields["status"] == "Running" && fields["node"] == "" {
			t.Errorf("%s is running but does not report the node it runs on", name)
		}
		switch {
		case strings.HasPrefix(name, "payments-api-"):
			checkedDeployment = true
			// A deployment's pods are controlled by its ReplicaSet, not the deployment.
			if fields["controlledByKind"] != "ReplicaSet" {
				t.Errorf("%s controlledByKind = %q, want ReplicaSet", name, fields["controlledByKind"])
			}
			if fields["controlledBy"] == "" {
				t.Errorf("%s has no controlling owner", name)
			}
		case strings.HasPrefix(name, "ledger-"):
			checkedStatefulSet = true
			if fields["controlledByKind"] != "StatefulSet" {
				t.Errorf("%s controlledByKind = %q, want StatefulSet", name, fields["controlledByKind"])
			}
		}
	}

	if !checkedDeployment || !checkedStatefulSet {
		t.Error("the seeded workloads were not all present; ownership went unchecked")
	}

	t.Run("columns describe how to render each value", func(t *testing.T) {
		byKey := map[string]cluster.Column{}
		for _, column := range result.Columns {
			byKey[column.Key] = column
		}

		if byKey["controlledBy"].Link != cluster.LinkOwner {
			t.Error("Controlled By is not marked as a link")
		}
		if byKey["node"].Link != cluster.LinkNode {
			t.Error("Node is not marked as a link")
		}
		if !byKey["status"].Status {
			t.Error("Status is not marked for status colouring")
		}
	})
}

// A service often spans a few namespaces, and watching only one hides half the picture.
func TestListSpansMultipleNamespaces(t *testing.T) {
	svc, c := seededCluster(t)
	pods, err := cluster.LookupType("pods")
	if err != nil {
		t.Fatalf("LookupType: %v", err)
	}

	result, err := svc.List(context.Background(), c, cluster.ListRequest{
		Type:       pods,
		Namespaces: []string{"payments", "storefront"},
	}, nil)
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	seen := map[string]bool{}
	for _, row := range result.Rows {
		seen[row.Namespace] = true
		if row.Namespace != "payments" && row.Namespace != "storefront" {
			t.Fatalf("a pod from %q leaked into a two-namespace listing", row.Namespace)
		}
	}

	if !seen["payments"] || !seen["storefront"] {
		t.Errorf("both namespaces should be represented, saw %v", seen)
	}
}

// Offering a kind the cluster does not serve turns a missing feature into an error the
// user cannot act on.
func TestAvailableTypesExcludeWhatTheClusterDoesNotServe(t *testing.T) {
	svc, c := seededCluster(t)

	available := svc.AvailableTypes(context.Background(), c, nil)
	if len(available) == 0 {
		t.Fatal("no types were reported as available")
	}

	byKey := map[string]bool{}
	for _, resourceType := range available {
		byKey[resourceType.Key()] = true
	}

	for _, expected := range []string{"pods", "apps/deployments", "nodes"} {
		if !byKey[expected] {
			t.Errorf("%s should be available on any cluster", expected)
		}
	}

	// This cluster has no Gateway API CRDs, so the kind must not be offered at all.
	if byKey["gateway.networking.k8s.io/httproutes"] {
		t.Skip("this cluster serves Gateway API; nothing to assert")
	}
}
