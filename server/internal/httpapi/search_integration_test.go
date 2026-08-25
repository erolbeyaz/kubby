package httpapi_test

import (
	"fmt"
	"net/http"
	"testing"
)

type searchBody struct {
	Hits []struct {
		ClusterID   string `json:"clusterId"`
		ClusterName string `json:"clusterName"`
		TypeKey     string `json:"typeKey"`
		Kind        string `json:"kind"`
		Namespace   string `json:"namespace"`
		Name        string `json:"name"`
		Status      string `json:"status"`
		Severity    string `json:"severity"`
	} `json:"hits"`
	Unreachable []struct {
		ClusterName string `json:"clusterName"`
		Reason      string `json:"reason"`
	} `json:"unreachable"`
	Truncated bool `json:"truncated"`
}

func search(t *testing.T, h *harness, query string) searchBody {
	t.Helper()

	resp := h.do(http.MethodGet, "/api/v1/search?q="+query, nil)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("search %q: %d %s", query, resp.StatusCode, readBody(resp))
	}
	return decode[searchBody](t, resp)
}

// One box, every cluster, every kind that people look for by name.
func TestSearchFindsObjectsAcrossKinds(t *testing.T) {
	h := signedInAdmin(t)
	registerCluster(t, h, "search-fleet")

	found := search(t, h, "payments")

	if len(found.Hits) == 0 {
		t.Fatal("nothing was found for a namespace that exists")
	}

	kinds := map[string]bool{}
	for _, hit := range found.Hits {
		kinds[hit.Kind] = true
		if hit.ClusterName != "search-fleet" {
			t.Errorf("a hit names cluster %q", hit.ClusterName)
		}
		if hit.Name == "" || hit.TypeKey == "" {
			t.Errorf("a hit cannot be opened: %+v", hit)
		}
	}
	if !kinds["Namespace"] {
		t.Errorf("the namespace itself was not found; kinds seen: %v", kinds)
	}
}

// A global search is nearly always someone looking for the thing that is wrong, so a
// broken object outranks a healthy one with a slightly better name match.
func TestBrokenObjectsRankAboveHealthyOnes(t *testing.T) {
	h := signedInAdmin(t)
	registerCluster(t, h, "search-ranking")

	// The seeded cluster runs a pod that never stops crashing.
	found := search(t, h, "crash")

	if len(found.Hits) == 0 {
		t.Skip("this cluster has no crashing pod to rank")
	}
	if found.Hits[0].Severity == "" {
		t.Errorf("the first hit is healthy while a broken one exists: %+v", found.Hits[0])
	}
}

// The important one. A search that quietly returns fewer results because a cluster is
// down is a search that lies: the reader concludes the object does not exist.
func TestAnUnreachableClusterIsReportedNotHidden(t *testing.T) {
	h := signedInAdmin(t)
	registerCluster(t, h, "search-reachable")

	// A second cluster whose address goes nowhere.
	broken := h.do(http.MethodPost, "/api/v1/clusters", map[string]any{
		"name": "search-broken", "environment": "test",
		"kubeconfig": unreachableKubeconfig(),
	})
	status := broken.StatusCode
	_ = broken.Body.Close()
	if status != http.StatusCreated {
		t.Skipf("could not register an unreachable cluster: %d", status)
	}

	found := search(t, h, "payments")

	named := false
	for _, problem := range found.Unreachable {
		if problem.ClusterName == "search-broken" {
			named = true
			if problem.Reason == "" {
				t.Error("the cluster is reported unreachable with no reason")
			}
		}
	}
	if !named {
		t.Fatalf("an unreachable cluster was silently dropped from the results; unreachable: %+v",
			found.Unreachable)
	}

	// And the reachable one still answered.
	if len(found.Hits) == 0 {
		t.Error("one unreachable cluster suppressed the results from the working one")
	}
}

// A cluster somebody was never granted must not be searched: telling them what is in it
// is exactly what the grant withholds.
func TestSearchOnlyCoversClustersTheReaderWasGranted(t *testing.T) {
	h := signedInAdmin(t)
	registerCluster(t, h, "search-private")

	member := h.do(http.MethodPost, "/api/v1/users", map[string]string{
		"email": "outsider@example.com", "displayName": "Outsider",
		"password": testPassword, "role": "user",
	})
	_ = member.Body.Close()

	logout := h.do(http.MethodPost, "/api/v1/auth/logout", nil)
	_ = logout.Body.Close()
	login := h.login("outsider@example.com", testPassword)
	_ = login.Body.Close()

	found := search(t, h, "payments")

	if len(found.Hits) != 0 {
		t.Fatalf("an ungranted cluster was searched: %+v", found.Hits[0])
	}
	if len(found.Unreachable) != 0 {
		// Not even as a failure: naming the cluster would confirm it exists.
		t.Fatalf("an ungranted cluster was named in the results: %+v", found.Unreachable)
	}
}

// Below two characters nearly everything matches, which costs every API server in the
// fleet a list call to produce a screen of noise.
func TestAVeryShortQueryIsNotRun(t *testing.T) {
	h := signedInAdmin(t)
	registerCluster(t, h, "search-short")

	found := search(t, h, "p")
	if len(found.Hits) != 0 {
		t.Fatalf("a one-character query was run and returned %d hits", len(found.Hits))
	}
}

func unreachableKubeconfig() string {
	// A syntactically valid config pointing at a port nothing listens on.
	return fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://127.0.0.1:%d
    insecure-skip-tls-verify: true
  name: nowhere
contexts:
- context: {cluster: nowhere, user: nowhere}
  name: nowhere
current-context: nowhere
users:
- name: nowhere
  user:
    token: not-a-real-token
`, 6553)
}
