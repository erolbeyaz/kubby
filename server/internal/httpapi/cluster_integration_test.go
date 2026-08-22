package httpapi_test

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
)

// liveKubeconfig enables the cluster endpoint tests. Without a real cluster these
// endpoints can only be tested for their refusals, which is the less interesting half.
func liveKubeconfig(t *testing.T) string {
	t.Helper()

	path := os.Getenv("KUBBY_TEST_KUBECONFIG")
	if path == "" {
		t.Skip("KUBBY_TEST_KUBECONFIG is not set; skipping live cluster endpoint tests")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read kubeconfig: %v", err)
	}
	return string(raw)
}

func fixtureText(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile("../cluster/testdata/" + name)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(raw)
}

type contextItem struct {
	Name       string `json:"name"`
	Server     string `json:"server"`
	AuthMethod string `json:"authMethod"`
	Blocked    bool   `json:"blocked"`
	Problem    string `json:"problem"`
}

type validateBody struct {
	Contexts       []contextItem `json:"contexts"`
	CurrentContext string        `json:"currentContext"`
	Probe          *struct {
		Status      string   `json:"status"`
		K8sVersion  string   `json:"k8sVersion"`
		NodeCount   *int     `json:"nodeCount"`
		Permissions []string `json:"permissions"`
	} `json:"probe"`
}

type clusterBody struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	CredentialStatus string `json:"credentialStatus"`
	StatusDetail     string `json:"statusDetail"`
	K8sVersion       string `json:"k8sVersion"`
	NodeCount        *int   `json:"nodeCount"`
	ReadOnly         bool   `json:"readOnly"`
	AccessLevel      string `json:"accessLevel"`
}

func signedInAdmin(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t)
	h.completeSetup("admin@example.com")

	login := h.login("admin@example.com", testPassword)
	_ = login.Body.Close()
	return h
}

// Validation must be a true dry run: the user decides after seeing the result.
func TestClusterValidateIsADryRun(t *testing.T) {
	h := signedInAdmin(t)
	kubeconfig := liveKubeconfig(t)

	resp := h.do(http.MethodPost, "/api/v1/clusters/validate", map[string]string{"kubeconfig": kubeconfig})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("validate returned %d: %s", resp.StatusCode, readBody(resp))
	}
	body := decode[validateBody](t, resp)
	_ = resp.Body.Close()

	if len(body.Contexts) == 0 {
		t.Fatal("no contexts were returned")
	}
	if body.Contexts[0].AuthMethod != "token" {
		t.Errorf("AuthMethod = %q, want token", body.Contexts[0].AuthMethod)
	}
	if body.Probe == nil {
		t.Fatal("no probe result; the user would be saving blind")
	}
	if body.Probe.Status != "valid" {
		t.Errorf("probe status = %q", body.Probe.Status)
	}
	if body.Probe.NodeCount == nil || *body.Probe.NodeCount == 0 {
		t.Error("probe reported no nodes")
	}
	if len(body.Probe.Permissions) == 0 {
		t.Error("probe reported no permissions")
	}

	list := h.do(http.MethodGet, "/api/v1/clusters", nil)
	clusters := decode[struct {
		Clusters []clusterBody `json:"clusters"`
	}](t, list)
	_ = list.Body.Close()

	if len(clusters.Clusters) != 0 {
		t.Fatalf("validation created %d clusters; it must store nothing", len(clusters.Clusters))
	}
}

func TestClusterValidateRefusesDangerousKubeconfigs(t *testing.T) {
	h := signedInAdmin(t)

	cases := map[string]struct {
		fixture string
		wants   string
	}{
		"exec plugin":      {"with-exec-plugin.yaml", "exec"},
		"metadata address": {"ssrf-metadata.yaml", "169.254"},
		"broken yaml":      {"broken.yaml", ""},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			resp := h.do(http.MethodPost, "/api/v1/clusters/validate",
				map[string]string{"kubeconfig": fixtureText(t, tc.fixture)})
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != http.StatusUnprocessableEntity {
				t.Fatalf("got %d, want 422", resp.StatusCode)
			}
			if tc.wants != "" && !strings.Contains(readBody(resp), tc.wants) {
				t.Errorf("error does not explain the refusal (expected %q)", tc.wants)
			}
		})
	}
}

func TestClusterCreateStoresHealthAndCredential(t *testing.T) {
	h := signedInAdmin(t)
	kubeconfig := liveKubeconfig(t)

	resp := h.do(http.MethodPost, "/api/v1/clusters", map[string]any{
		"name": "prod-app", "environment": "prod", "environmentLabel": "Production",
		"color": "#f43f5e", "kubeconfig": kubeconfig,
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create returned %d: %s", resp.StatusCode, readBody(resp))
	}
	created := decode[clusterBody](t, resp)
	_ = resp.Body.Close()

	if created.CredentialStatus != "valid" {
		t.Errorf("CredentialStatus = %q (%s)", created.CredentialStatus, created.StatusDetail)
	}
	if created.K8sVersion == "" || created.NodeCount == nil {
		t.Errorf("health was not recorded: %+v", created)
	}

	// The credential is write-only from the API's point of view: it goes in, and the
	// only way to change it is to paste a new one (ADR-018).
	t.Run("the stored credential is never returned", func(t *testing.T) {
		get := h.do(http.MethodGet, "/api/v1/clusters/"+created.ID, nil)
		defer func() { _ = get.Body.Close() }()

		body := readBody(get)
		for _, secret := range secretsIn(t, kubeconfig) {
			if strings.Contains(body, secret) {
				t.Errorf("the cluster response leaks a credential value")
			}
		}
		for _, marker := range []string{"apiVersion: v1", "kind: Config", "certificate-authority-data"} {
			if strings.Contains(body, marker) {
				t.Errorf("the cluster response leaks kubeconfig content (%q)", marker)
			}
		}
	})

	t.Run("a duplicate name is refused", func(t *testing.T) {
		dup := h.do(http.MethodPost, "/api/v1/clusters", map[string]any{
			"name": "PROD-APP", "environment": "test", "kubeconfig": kubeconfig,
		})
		defer func() { _ = dup.Body.Close() }()
		if dup.StatusCode != http.StatusConflict {
			t.Errorf("duplicate name returned %d, want 409", dup.StatusCode)
		}
	})

	t.Run("connection can be retested on demand", func(t *testing.T) {
		test := h.do(http.MethodPost, "/api/v1/clusters/"+created.ID+"/test", nil)
		defer func() { _ = test.Body.Close() }()
		if test.StatusCode != http.StatusOK {
			t.Fatalf("test returned %d", test.StatusCode)
		}
		if decode[clusterBody](t, test).CredentialStatus != "valid" {
			t.Error("retest did not report a valid credential")
		}
	})
}

// The lock guards the cluster's own resources, not Kubby's record of it: a locked
// registration must stay repairable and removable (ADR-039).
func TestLockedClusterRemainsAdministrable(t *testing.T) {
	h := signedInAdmin(t)
	kubeconfig := liveKubeconfig(t)

	resp := h.do(http.MethodPost, "/api/v1/clusters", map[string]any{
		"name": "locked", "environment": "prod", "kubeconfig": kubeconfig,
	})
	created := decode[clusterBody](t, resp)
	_ = resp.Body.Close()

	lock := h.do(http.MethodPatch, "/api/v1/clusters/"+created.ID, map[string]bool{"readOnly": true})
	if lock.StatusCode != http.StatusOK {
		t.Fatalf("locking returned %d: %s", lock.StatusCode, readBody(lock))
	}
	if !decode[clusterBody](t, lock).ReadOnly {
		t.Fatal("the cluster does not report itself as locked")
	}
	_ = lock.Body.Close()

	t.Run("reading still works", func(t *testing.T) {
		get := h.do(http.MethodGet, "/api/v1/clusters/"+created.ID, nil)
		defer func() { _ = get.Body.Close() }()
		if get.StatusCode != http.StatusOK {
			t.Errorf("reading a locked cluster returned %d", get.StatusCode)
		}
	})

	// An expired token on a locked cluster must be fixable without unlocking first;
	// otherwise the lock would force an operator to weaken it to repair it.
	t.Run("the credential can still be replaced", func(t *testing.T) {
		put := h.do(http.MethodPut, "/api/v1/clusters/"+created.ID+"/credentials",
			map[string]string{"kubeconfig": kubeconfig})
		defer func() { _ = put.Body.Close() }()

		if put.StatusCode != http.StatusOK {
			t.Fatalf("replacing the credential on a locked cluster returned %d: %s",
				put.StatusCode, readBody(put))
		}
	})

	t.Run("settings can still be changed", func(t *testing.T) {
		patch := h.do(http.MethodPatch, "/api/v1/clusters/"+created.ID,
			map[string]string{"environmentLabel": "Production"})
		defer func() { _ = patch.Body.Close() }()

		if patch.StatusCode != http.StatusOK {
			t.Errorf("changing settings on a locked cluster returned %d", patch.StatusCode)
		}
	})

	// The lock must never make a registration permanently undeletable.
	t.Run("an administrator can remove it", func(t *testing.T) {
		del := h.do(http.MethodDelete, "/api/v1/clusters/"+created.ID, nil)
		defer func() { _ = del.Body.Close() }()

		if del.StatusCode != http.StatusNoContent {
			t.Fatalf("deleting a locked cluster returned %d: %s", del.StatusCode, readBody(del))
		}

		get := h.do(http.MethodGet, "/api/v1/clusters/"+created.ID, nil)
		defer func() { _ = get.Body.Close() }()
		if get.StatusCode != http.StatusNotFound {
			t.Errorf("the cluster still exists after deletion: %d", get.StatusCode)
		}
	})
}

// A user must never learn that a cluster they were not granted even exists.
func TestUngrantedClustersAreInvisible(t *testing.T) {
	h := signedInAdmin(t)
	kubeconfig := liveKubeconfig(t)

	resp := h.do(http.MethodPost, "/api/v1/clusters", map[string]any{
		"name": "secret-cluster", "environment": "prod", "kubeconfig": kubeconfig,
	})
	created := decode[clusterBody](t, resp)
	_ = resp.Body.Close()

	member := h.do(http.MethodPost, "/api/v1/users", map[string]string{
		"email": "member@example.com", "displayName": "Member",
		"password": testPassword, "role": "user",
	})
	memberBody := decode[map[string]any](t, member)
	_ = member.Body.Close()

	logout := h.do(http.MethodPost, "/api/v1/auth/logout", nil)
	_ = logout.Body.Close()

	memberLogin := h.login("member@example.com", testPassword)
	_ = memberLogin.Body.Close()

	t.Run("it is absent from the list", func(t *testing.T) {
		list := h.do(http.MethodGet, "/api/v1/clusters", nil)
		defer func() { _ = list.Body.Close() }()

		body := decode[struct {
			Clusters []clusterBody `json:"clusters"`
		}](t, list)
		if len(body.Clusters) != 0 {
			t.Fatalf("an ungranted user sees %d clusters", len(body.Clusters))
		}
	})

	t.Run("fetching it reports not found, not forbidden", func(t *testing.T) {
		get := h.do(http.MethodGet, "/api/v1/clusters/"+created.ID, nil)
		defer func() { _ = get.Body.Close() }()

		if get.StatusCode != http.StatusNotFound {
			t.Errorf("got %d, want 404: existence itself must not leak", get.StatusCode)
		}
	})

	t.Run("creating a cluster is forbidden", func(t *testing.T) {
		create := h.do(http.MethodPost, "/api/v1/clusters", map[string]any{
			"name": "mine", "environment": "test", "kubeconfig": kubeconfig,
		})
		defer func() { _ = create.Body.Close() }()

		if create.StatusCode != http.StatusForbidden {
			t.Errorf("got %d, want 403", create.StatusCode)
		}
	})

	t.Run("a granted cluster becomes visible", func(t *testing.T) {
		back := h.do(http.MethodPost, "/api/v1/auth/logout", nil)
		_ = back.Body.Close()
		adminLogin := h.login("admin@example.com", testPassword)
		_ = adminLogin.Body.Close()

		grant := h.do(http.MethodPut, "/api/v1/clusters/"+created.ID+"/grants", map[string]string{
			"userId": fmt.Sprint(memberBody["id"]), "accessLevel": "read",
		})
		if grant.StatusCode != http.StatusOK {
			t.Fatalf("granting returned %d: %s", grant.StatusCode, readBody(grant))
		}
		_ = grant.Body.Close()

		out := h.do(http.MethodPost, "/api/v1/auth/logout", nil)
		_ = out.Body.Close()
		again := h.login("member@example.com", testPassword)
		_ = again.Body.Close()

		list := h.do(http.MethodGet, "/api/v1/clusters", nil)
		defer func() { _ = list.Body.Close() }()

		body := decode[struct {
			Clusters []clusterBody `json:"clusters"`
		}](t, list)
		if len(body.Clusters) != 1 {
			t.Fatalf("granted user sees %d clusters, want 1", len(body.Clusters))
		}
		if body.Clusters[0].AccessLevel != "read" {
			t.Errorf("AccessLevel = %q, want read", body.Clusters[0].AccessLevel)
		}
	})
}

// Read access must not be enough to change a cluster.
func TestReadGrantCannotWrite(t *testing.T) {
	h := signedInAdmin(t)
	kubeconfig := liveKubeconfig(t)

	resp := h.do(http.MethodPost, "/api/v1/clusters", map[string]any{
		"name": "read-only-grant", "environment": "test", "kubeconfig": kubeconfig,
	})
	created := decode[clusterBody](t, resp)
	_ = resp.Body.Close()

	member := h.do(http.MethodPost, "/api/v1/users", map[string]string{
		"email": "viewer@example.com", "displayName": "Viewer",
		"password": testPassword, "role": "user",
	})
	memberBody := decode[map[string]any](t, member)
	_ = member.Body.Close()

	grant := h.do(http.MethodPut, "/api/v1/clusters/"+created.ID+"/grants", map[string]string{
		"userId": fmt.Sprint(memberBody["id"]), "accessLevel": "read",
	})
	_ = grant.Body.Close()

	logout := h.do(http.MethodPost, "/api/v1/auth/logout", nil)
	_ = logout.Body.Close()
	login := h.login("viewer@example.com", testPassword)
	_ = login.Body.Close()

	// PATCH requires cluster.manage, which a plain user does not hold at all.
	patch := h.do(http.MethodPatch, "/api/v1/clusters/"+created.ID, map[string]bool{"readOnly": true})
	defer func() { _ = patch.Body.Close() }()

	if patch.StatusCode != http.StatusForbidden {
		t.Errorf("a read grant could reach a write endpoint: %d", patch.StatusCode)
	}

	get := h.do(http.MethodGet, "/api/v1/clusters/"+created.ID, nil)
	defer func() { _ = get.Body.Close() }()
	if get.StatusCode != http.StatusOK {
		t.Errorf("a read grant could not read: %d", get.StatusCode)
	}
}

// secretsIn extracts the values that must never appear in any API response: the bearer
// token and the CA payload from the kubeconfig that was submitted.
func secretsIn(t *testing.T, kubeconfig string) []string {
	t.Helper()

	var secrets []string
	for _, line := range strings.Split(kubeconfig, "\n") {
		trimmed := strings.TrimSpace(line)
		for _, key := range []string{"token:", "certificate-authority-data:", "client-key-data:"} {
			if strings.HasPrefix(trimmed, key) {
				value := strings.TrimSpace(strings.TrimPrefix(trimmed, key))
				if len(value) > 12 {
					secrets = append(secrets, value)
				}
			}
		}
	}
	if len(secrets) == 0 {
		t.Fatal("the test kubeconfig carries no secret to check for; the assertion would be vacuous")
	}
	return secrets
}
