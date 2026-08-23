package httpapi_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// deployment returns a manifest for the seeded workload, scaled as asked.
func deploymentManifest(replicas int) string {
	return fmt.Sprintf(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: payments-api
  namespace: payments
spec:
  replicas: %d
  selector:
    matchLabels:
      app: payments-api
  template:
    metadata:
      labels:
        app: payments-api
    spec:
      containers:
        - name: api
          image: nginx:1.27-alpine
`, replicas)
}

type applyResponse struct {
	DryRun  bool `json:"dryRun"`
	Results []struct {
		Kind      string `json:"kind"`
		Name      string `json:"name"`
		Unchanged bool   `json:"unchanged"`
		Error     string `json:"error"`
		Diff      []struct {
			Kind string `json:"kind"`
			Text string `json:"text"`
		} `json:"diff"`
	} `json:"results"`
}

// The point of the dry run is not validation — the real write validates too — but that
// the person pressing the button sees the change the server will actually make.
func TestDryRunReportsTheDiffWithoutWriting(t *testing.T) {
	h := signedInAdmin(t)
	id := registerCluster(t, h, "write-dryrun")

	resp := h.do(http.MethodPost, "/api/v1/clusters/"+id+"/apply", map[string]any{
		"manifest": deploymentManifest(4),
		"dryRun":   true,
	})
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("dry run returned %d %s", resp.StatusCode, readBody(resp))
	}
	body := decode[applyResponse](t, resp)

	if len(body.Results) != 1 || body.Results[0].Error != "" {
		t.Fatalf("results = %+v", body.Results)
	}
	if len(body.Results[0].Diff) == 0 {
		t.Fatal("a dry run that changes replicas produced no diff")
	}

	// Nothing was written: the live object still has its original replica count.
	list := h.do(http.MethodGet, "/api/v1/clusters/"+id+"/resources/apps/deployments?namespace=payments", nil)
	defer func() { _ = list.Body.Close() }()
	rows := decode[listBody](t, list)

	for _, row := range rows.Rows {
		if row.Name == "payments-api" && row.Fields["replicas"] == "4" {
			t.Fatal("the dry run wrote to the cluster")
		}
	}
}

// The per-cluster lock binds everyone, admins included: it exists for the window where a
// cluster must not change, not to express who is senior (ADR-029/039).
func TestALockedClusterRefusesEveryWrite(t *testing.T) {
	h := signedInAdmin(t)
	id := registerCluster(t, h, "write-locked")

	lock := h.do(http.MethodPatch, "/api/v1/clusters/"+id, map[string]any{"readOnly": true})
	if lock.StatusCode != http.StatusOK {
		t.Fatalf("could not lock the cluster: %d %s", lock.StatusCode, readBody(lock))
	}
	_ = lock.Body.Close()

	for _, attempt := range []struct {
		name   string
		method string
		path   string
		body   any
	}{
		{"apply", http.MethodPost, "/apply", map[string]any{"manifest": deploymentManifest(9)}},
		{"scale", http.MethodPost, "/scale", map[string]any{
			"typeKey": "apps/deployments", "namespace": "payments", "name": "payments-api", "replicas": 9,
		}},
		{"restart", http.MethodPost, "/restart", map[string]any{
			"typeKey": "apps/deployments", "namespace": "payments", "name": "payments-api",
		}},
		{"delete", http.MethodDelete, "/object/apps/deployments?namespace=payments&name=payments-api", nil},
	} {
		t.Run(attempt.name, func(t *testing.T) {
			resp := h.do(attempt.method, "/api/v1/clusters/"+id+attempt.path, attempt.body)
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != http.StatusForbidden {
				t.Fatalf("%s returned %d, want 403: %s", attempt.name, resp.StatusCode, readBody(resp))
			}
			// The reason matters: "forbidden" alone leaves the reader guessing which of
			// four gates stopped them.
			if !strings.Contains(strings.ToLower(readBody(resp)), "read-only") {
				t.Errorf("%s did not say the cluster is locked: %s", attempt.name, readBody(resp))
			}
		})
	}
}

// A reader may not write, whatever the cluster itself would have allowed.
func TestAReadOnlyRoleCannotWrite(t *testing.T) {
	h := signedInAdmin(t)
	id := registerCluster(t, h, "write-role")

	created := h.do(http.MethodPost, "/api/v1/users", map[string]any{
		"email": "viewer@example.com", "displayName": "Viewer",
		"password": testPassword, "role": "readonly",
	})
	if created.StatusCode != http.StatusCreated {
		t.Fatalf("create user: %d %s", created.StatusCode, readBody(created))
	}
	_ = created.Body.Close()

	// Signing in as the reader replaces the admin session on this client, which is the
	// point: the same request, a different role.
	login := h.login("viewer@example.com", testPassword)
	if login.StatusCode != http.StatusOK {
		t.Fatalf("viewer sign-in: %d %s", login.StatusCode, readBody(login))
	}
	_ = login.Body.Close()

	resp := h.do(http.MethodPost, "/api/v1/clusters/"+id+"/scale", map[string]any{
		"typeKey": "apps/deployments", "namespace": "payments", "name": "payments-api", "replicas": 2,
	})
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("a reader scaled a workload: %d %s", resp.StatusCode, readBody(resp))
	}
}

// The refusals above have to be failing for the right reason, so the same call from an
// admin on an unlocked cluster must succeed.
func TestAnAdminCanScale(t *testing.T) {
	h := signedInAdmin(t)
	id := registerCluster(t, h, "write-scale")

	resp := h.do(http.MethodPost, "/api/v1/clusters/"+id+"/scale", map[string]any{
		"typeKey": "apps/deployments", "namespace": "payments", "name": "payments-api", "replicas": 2,
	})
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("scale returned %d %s", resp.StatusCode, readBody(resp))
	}

	// Put it back, so the next test does not inherit a shrunken workload.
	restore := h.do(http.MethodPost, "/api/v1/clusters/"+id+"/scale", map[string]any{
		"typeKey": "apps/deployments", "namespace": "payments", "name": "payments-api", "replicas": 3,
	})
	_ = restore.Body.Close()
}

// Every document is reported on its own: saying which one failed is the whole value of
// applying a multi-document manifest here.
func TestMultiDocumentManifestReportsEachDocument(t *testing.T) {
	h := signedInAdmin(t)
	id := registerCluster(t, h, "write-multidoc")

	manifest := deploymentManifest(3) + "\n---\n" + `apiVersion: v1
kind: ConfigMap
metadata:
  name: kubby-write-test
  namespace: payments
data:
  hello: world
`

	resp := h.do(http.MethodPost, "/api/v1/clusters/"+id+"/apply", map[string]any{
		"manifest": manifest, "dryRun": true,
	})
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("apply returned %d %s", resp.StatusCode, readBody(resp))
	}
	body := decode[applyResponse](t, resp)

	if len(body.Results) != 2 {
		t.Fatalf("results = %d, want one per document", len(body.Results))
	}
	kinds := []string{body.Results[0].Kind, body.Results[1].Kind}
	if kinds[0] != "Deployment" || kinds[1] != "ConfigMap" {
		t.Fatalf("kinds = %v", kinds)
	}
}

func TestApplyRejectsAManifestWithoutAKind(t *testing.T) {
	h := signedInAdmin(t)
	id := registerCluster(t, h, "write-nokind")

	resp := h.do(http.MethodPost, "/api/v1/clusters/"+id+"/apply", map[string]any{
		"manifest": "replicas: 3\n", "dryRun": true,
	})
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("returned %d, want 400: %s", resp.StatusCode, readBody(resp))
	}
}

// A delete that answers 200 while the object is still there is worse than one that
// fails: the reader is told it worked. This proves the round trip.
func TestDeleteActuallyRemovesTheObject(t *testing.T) {
	h := signedInAdmin(t)
	id := registerCluster(t, h, "write-delete")

	const name = "kubby-delete-probe"
	manifest := `apiVersion: v1
kind: ConfigMap
metadata:
  name: ` + name + `
  namespace: payments
data:
  probe: "1"
`

	created := h.do(http.MethodPost, "/api/v1/clusters/"+id+"/apply", map[string]any{
		"manifest": manifest, "dryRun": false,
	})
	if created.StatusCode != http.StatusOK {
		t.Fatalf("create: %d %s", created.StatusCode, readBody(created))
	}
	_ = created.Body.Close()

	if !configMapExists(t, h, id, name) {
		t.Fatal("the ConfigMap was not created, so the delete proves nothing")
	}

	resp := h.do(http.MethodDelete,
		"/api/v1/clusters/"+id+"/object/configmaps?namespace=payments&name="+name, nil)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete: %d %s", resp.StatusCode, readBody(resp))
	}
	if configMapExists(t, h, id, name) {
		t.Fatal("delete answered 200 but the object is still there")
	}
}

func configMapExists(t *testing.T, h *harness, id, name string) bool {
	t.Helper()

	resp := h.do(http.MethodGet, "/api/v1/clusters/"+id+"/resources/configmaps?namespace=payments", nil)
	defer func() { _ = resp.Body.Close() }()

	for _, row := range decode[listBody](t, resp).Rows {
		if row.Name == name {
			return true
		}
	}
	return false
}

// Draining a node is the most consequential thing in this phase, so what it would do has
// to be visible before it does any of it.
func TestDrainPlanSeparatesWhatMovesFromWhatStays(t *testing.T) {
	h := signedInAdmin(t)
	id := registerCluster(t, h, "drain-plan")

	nodes := h.do(http.MethodGet, "/api/v1/clusters/"+id+"/resources/nodes", nil)
	rows := decode[listBody](t, nodes).Rows
	_ = nodes.Body.Close()
	if len(rows) == 0 {
		t.Skip("no nodes")
	}
	node := rows[0].Name

	resp := h.do(http.MethodGet, "/api/v1/clusters/"+id+"/drain-plan/"+node, nil)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("plan returned %d %s", resp.StatusCode, readBody(resp))
	}
	plan := decode[struct {
		Node  string `json:"node"`
		Evict []struct {
			Name   string `json:"name"`
			Owner  string `json:"owner"`
			Reason string `json:"reason"`
		} `json:"evict"`
		Skip []struct {
			Name   string `json:"name"`
			Reason string `json:"reason"`
		} `json:"skip"`
	}](t, resp)

	if plan.Node != node {
		t.Fatalf("plan is for %q, asked about %q", plan.Node, node)
	}

	// A DaemonSet puts its pod straight back on the same node, so evicting it is a
	// restart dressed as a drain. The seeded cluster runs one on every node.
	var sawDaemonSet bool
	for _, pod := range plan.Skip {
		if pod.Reason == "" {
			t.Errorf("pod %q is skipped without saying why", pod.Name)
		}
		if strings.Contains(pod.Reason, "DaemonSet") {
			sawDaemonSet = true
		}
	}
	if !sawDaemonSet {
		t.Error("no DaemonSet pod was held back; the seeded cluster runs one per node")
	}

	// A pod with no controller is not coming back on its own, and the plan has to say so.
	for _, pod := range plan.Evict {
		if pod.Owner == "" && pod.Reason == "" {
			t.Errorf("pod %q has no controller and no warning", pod.Name)
		}
	}
}
