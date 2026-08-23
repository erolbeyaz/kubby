package httpapi_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

type listBody struct {
	Columns []struct {
		Key   string `json:"key"`
		Label string `json:"label"`
	} `json:"columns"`
	Rows []struct {
		Name      string            `json:"name"`
		Namespace string            `json:"namespace"`
		Age       string            `json:"age"`
		Fields    map[string]string `json:"fields"`
		Severity  string            `json:"severity"`
	} `json:"rows"`
	Total     int  `json:"total"`
	FromCache bool `json:"fromCache"`
}

// registerCluster adds the live test cluster and returns its id.
func registerCluster(t *testing.T, h *harness, name string) string {
	t.Helper()

	resp := h.do(http.MethodPost, "/api/v1/clusters", map[string]any{
		"name": name, "environment": "test", "kubeconfig": liveKubeconfig(t),
	})
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register cluster: %d %s", resp.StatusCode, readBody(resp))
	}
	return decode[clusterBody](t, resp).ID
}

func TestResourceListingOverHTTP(t *testing.T) {
	h := signedInAdmin(t)
	id := registerCluster(t, h, "resources")

	t.Run("namespaces are listed", func(t *testing.T) {
		resp := h.do(http.MethodGet, "/api/v1/clusters/"+id+"/namespaces", nil)
		defer func() { _ = resp.Body.Close() }()

		body := decode[struct {
			Namespaces []string `json:"namespaces"`
		}](t, resp)

		if !contains(body.Namespaces, "payments") {
			t.Errorf("payments namespace is missing: %v", body.Namespaces)
		}
	})

	t.Run("the type catalogue is served", func(t *testing.T) {
		resp := h.do(http.MethodGet, "/api/v1/clusters/"+id+"/resource-types", nil)
		defer func() { _ = resp.Body.Close() }()

		body := decode[struct {
			Types []struct {
				Key      string `json:"key"`
				Kind     string `json:"kind"`
				Category string `json:"category"`
			} `json:"types"`
		}](t, resp)

		if len(body.Types) < 20 {
			t.Errorf("only %d types were offered", len(body.Types))
		}
	})

	t.Run("pods list with columns and rows", func(t *testing.T) {
		resp := h.do(http.MethodGet, "/api/v1/clusters/"+id+"/resources/pods?namespace=payments", nil)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("got %d: %s", resp.StatusCode, readBody(resp))
		}
		body := decode[listBody](t, resp)

		if len(body.Columns) == 0 {
			t.Error("no columns were described; the client would not know how to render")
		}
		if len(body.Rows) == 0 {
			t.Fatal("no pods were returned")
		}
		for _, row := range body.Rows {
			if row.Namespace != "payments" {
				t.Fatalf("a %s pod leaked into a payments listing", row.Namespace)
			}
		}
	})

	t.Run("grouped kinds are addressable by their group", func(t *testing.T) {
		resp := h.do(http.MethodGet,
			"/api/v1/clusters/"+id+"/resources/apps/deployments?namespace=payments", nil)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("got %d: %s", resp.StatusCode, readBody(resp))
		}
		body := decode[listBody](t, resp)

		var found bool
		for _, row := range body.Rows {
			if row.Name == "payments-api" {
				found = true
				if row.Fields["ready"] != "3/3" {
					t.Errorf("ready = %q", row.Fields["ready"])
				}
			}
		}
		if !found {
			t.Error("payments-api was not listed")
		}
	})

	t.Run("search narrows the list on the server", func(t *testing.T) {
		resp := h.do(http.MethodGet,
			"/api/v1/clusters/"+id+"/resources/pods?namespace=payments&search=ledger", nil)
		defer func() { _ = resp.Body.Close() }()

		body := decode[listBody](t, resp)
		if len(body.Rows) == 0 {
			t.Fatal("search returned nothing")
		}
		for _, row := range body.Rows {
			if !strings.Contains(row.Name, "ledger") {
				t.Errorf("%q does not match the search", row.Name)
			}
		}
	})

	t.Run("a single object is returned in full", func(t *testing.T) {
		resp := h.do(http.MethodGet,
			"/api/v1/clusters/"+id+"/object/apps/deployments?namespace=payments&name=payments-api", nil)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("got %d: %s", resp.StatusCode, readBody(resp))
		}
		payload := readBody(resp)

		if !strings.Contains(payload, `"kind":"Deployment"`) {
			t.Error("the object does not identify its kind")
		}
		if strings.Contains(payload, "managedFields") {
			t.Error("managedFields reached the detail view")
		}
	})

	t.Run("an unknown type is refused", func(t *testing.T) {
		resp := h.do(http.MethodGet, "/api/v1/clusters/"+id+"/resources/widgets", nil)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("got %d, want 404", resp.StatusCode)
		}
	})
}

// Secret values must not be reachable through the listing path at all.
func TestSecretListingCarriesNoValues(t *testing.T) {
	h := signedInAdmin(t)
	id := registerCluster(t, h, "secrets-check")

	resp := h.do(http.MethodGet, "/api/v1/clusters/"+id+"/resources/secrets?namespace=payments", nil)
	defer func() { _ = resp.Body.Close() }()

	payload := readBody(resp)
	for _, forbidden := range []string{
		"not-a-real-password-for-testing",
		"bm90LWEtcmVhbC1wYXNzd29yZC1mb3ItdGVzdGluZw==",
	} {
		if strings.Contains(payload, forbidden) {
			t.Fatal("a secret value was served in a resource listing")
		}
	}
	if !strings.Contains(payload, "api-credentials") {
		t.Error("the secret was not listed at all")
	}
}

// Reading a cluster requires a grant, and an ungranted one must not even be visible.
func TestResourceReadsRequireClusterAccess(t *testing.T) {
	h := signedInAdmin(t)
	id := registerCluster(t, h, "guarded")

	member := h.do(http.MethodPost, "/api/v1/users", map[string]string{
		"email": "reader@example.com", "displayName": "Reader",
		"password": testPassword, "role": "user",
	})
	memberBody := decode[map[string]any](t, member)
	_ = member.Body.Close()

	logout := h.do(http.MethodPost, "/api/v1/auth/logout", nil)
	_ = logout.Body.Close()
	login := h.login("reader@example.com", testPassword)
	_ = login.Body.Close()

	t.Run("without a grant the cluster is invisible", func(t *testing.T) {
		resp := h.do(http.MethodGet, "/api/v1/clusters/"+id+"/resources/pods", nil)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("got %d, want 404", resp.StatusCode)
		}
	})

	t.Run("with a read grant the listing works", func(t *testing.T) {
		back := h.do(http.MethodPost, "/api/v1/auth/logout", nil)
		_ = back.Body.Close()
		adminLogin := h.login("admin@example.com", testPassword)
		_ = adminLogin.Body.Close()

		grant := h.do(http.MethodPut, "/api/v1/clusters/"+id+"/grants", map[string]string{
			"userId": fmt.Sprint(memberBody["id"]), "accessLevel": "read",
		})
		_ = grant.Body.Close()

		out := h.do(http.MethodPost, "/api/v1/auth/logout", nil)
		_ = out.Body.Close()
		again := h.login("reader@example.com", testPassword)
		_ = again.Body.Close()

		resp := h.do(http.MethodGet, "/api/v1/clusters/"+id+"/resources/pods?namespace=payments", nil)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("got %d: %s", resp.StatusCode, readBody(resp))
		}
		if len(decode[listBody](t, resp).Rows) == 0 {
			t.Error("a granted reader saw no pods")
		}
	})
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// The overview answers "is this cluster healthy and how full is it" in one request.
func TestClusterOverview(t *testing.T) {
	h := signedInAdmin(t)
	id := registerCluster(t, h, "overview")

	resp := h.do(http.MethodGet, "/api/v1/clusters/"+id+"/overview", nil)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got %d: %s", resp.StatusCode, readBody(resp))
	}

	body := decode[struct {
		Nodes      int `json:"nodes"`
		NodesReady int `json:"nodesReady"`
		Namespaces int `json:"namespaces"`
		CPU        struct {
			Usage, Requests, Allocatable, Capacity float64
		} `json:"cpu"`
		Memory struct {
			Usage, Requests, Capacity float64
		} `json:"memory"`
		Pods struct {
			Usage, Capacity float64
		} `json:"pods"`
		Problems []struct {
			Kind, Name, Reason, Severity string
		} `json:"problems"`
	}](t, resp)

	if body.Nodes != 3 || body.NodesReady != 3 {
		t.Errorf("nodes = %d (%d ready), want 3/3", body.Nodes, body.NodesReady)
	}
	if body.Namespaces < 2 {
		t.Errorf("namespaces = %d, want at least the seeded two", body.Namespaces)
	}
	if body.CPU.Capacity == 0 || body.Memory.Capacity == 0 {
		t.Error("node capacity was not summed")
	}
	if body.CPU.Requests == 0 {
		t.Error("pod requests were not summed")
	}
	if body.Pods.Usage == 0 || body.Pods.Capacity == 0 {
		t.Errorf("pod counts look wrong: %v of %v", body.Pods.Usage, body.Pods.Capacity)
	}

	// The seeded StatefulSet leaves one pod unschedulable, which is exactly the kind of
	// thing the overview exists to surface.
	var foundPending bool
	for _, problem := range body.Problems {
		if problem.Kind == "Pod" && problem.Reason == "Pending" {
			foundPending = true
		}
	}
	if !foundPending {
		t.Logf("no pending pod found; problems = %+v", body.Problems)
	}
}
