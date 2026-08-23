package httpapi_test

import (
	"net/http"
	"strings"
	"testing"
)

type settingsBody struct {
	NodeShell struct {
		Image      string `json:"image"`
		Namespace  string `json:"namespace"`
		PullSecret string `json:"pullSecret"`
		Enabled    bool   `json:"enabled"`
	} `json:"nodeShell"`
	Metrics struct {
		Enabled     bool   `json:"enabled"`
		URL         string `json:"url"`
		Username    string `json:"username"`
		HasPassword bool   `json:"hasPassword"`
	} `json:"metrics"`
	AuditSink struct {
		Enabled  bool   `json:"enabled"`
		Kind     string `json:"kind"`
		URL      string `json:"url"`
		HasToken bool   `json:"hasToken"`
	} `json:"auditSink"`
}

func TestSettingsDefaultToSomethingUsable(t *testing.T) {
	h := signedInAdmin(t)

	resp := h.do(http.MethodGet, "/api/v1/settings", nil)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("settings returned %d %s", resp.StatusCode, readBody(resp))
	}
	body := decode[settingsBody](t, resp)

	// A node shell is root on the machine, so it is never on until someone turns it on.
	if body.NodeShell.Enabled {
		t.Error("node shell is enabled by default")
	}
	if body.NodeShell.Image == "" || body.NodeShell.Namespace == "" {
		t.Errorf("node shell has no usable default: %+v", body.NodeShell)
	}
}

// A credential must be reported as present without being sent: knowing one is stored is
// configuration, and the value is not.
func TestASavedPasswordIsNeverReturned(t *testing.T) {
	h := signedInAdmin(t)

	const password = "prom-basic-auth-secret"
	saved := h.do(http.MethodPut, "/api/v1/settings/metrics", map[string]any{
		"enabled": true, "url": "https://prometheus.example.com", "username": "kubby",
		"password": password,
	})
	if saved.StatusCode != http.StatusOK {
		t.Fatalf("save: %d %s", saved.StatusCode, readBody(saved))
	}
	if strings.Contains(readBody(saved), password) {
		t.Fatal("the password came back in the save response")
	}
	_ = saved.Body.Close()

	resp := h.do(http.MethodGet, "/api/v1/settings", nil)
	defer func() { _ = resp.Body.Close() }()

	// Read once: the body is a stream, and checking it twice reads the second half of
	// nothing.
	payload := readBody(resp)
	if strings.Contains(payload, password) {
		t.Fatal("the password came back when reading the settings")
	}
	if !strings.Contains(payload, `"hasPassword":true`) {
		t.Errorf("the settings do not report that a password is stored: %s", payload)
	}
}

// A form saved without retyping the field must not wipe the credential.
func TestSavingWithoutRetypingKeepsThePassword(t *testing.T) {
	h := signedInAdmin(t)

	first := h.do(http.MethodPut, "/api/v1/settings/metrics", map[string]any{
		"enabled": true, "url": "https://prometheus.example.com", "password": "keep-me",
	})
	_ = first.Body.Close()

	again := h.do(http.MethodPut, "/api/v1/settings/metrics", map[string]any{
		"enabled": true, "url": "https://prometheus.example.com", "username": "changed",
	})
	defer func() { _ = again.Body.Close() }()

	if !decode[settingsBody](t, again).Metrics.HasPassword {
		t.Fatal("saving the form without the password field wiped the password")
	}
}

func TestClearingAPasswordRemovesIt(t *testing.T) {
	h := signedInAdmin(t)

	first := h.do(http.MethodPut, "/api/v1/settings/metrics", map[string]any{
		"enabled": true, "url": "https://prometheus.example.com", "password": "remove-me",
	})
	_ = first.Body.Close()

	cleared := h.do(http.MethodPut, "/api/v1/settings/metrics", map[string]any{
		"enabled": true, "url": "https://prometheus.example.com", "clearPassword": true,
	})
	defer func() { _ = cleared.Body.Close() }()

	if decode[settingsBody](t, cleared).Metrics.HasPassword {
		t.Fatal("clearing the password left it stored")
	}
}

func TestSettingsRefuseWhatCannotBeDialled(t *testing.T) {
	h := signedInAdmin(t)

	for _, url := range []string{"", "prometheus.example.com", "ftp://prometheus"} {
		resp := h.do(http.MethodPut, "/api/v1/settings/metrics", map[string]any{
			"enabled": true, "url": url,
		})
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("url %q was accepted: %d %s", url, resp.StatusCode, readBody(resp))
		}
		_ = resp.Body.Close()
	}
}

// Knowing where the audit trail is shipped is worth restricting on its own.
func TestOnlyAnAdminReadsTheSettings(t *testing.T) {
	h := signedInAdmin(t)

	created := h.do(http.MethodPost, "/api/v1/users", map[string]any{
		"email": "viewer@example.com", "displayName": "Viewer",
		"password": testPassword, "role": "user",
	})
	if created.StatusCode != http.StatusCreated {
		t.Fatalf("create user: %d %s", created.StatusCode, readBody(created))
	}
	_ = created.Body.Close()

	login := h.login("viewer@example.com", testPassword)
	_ = login.Body.Close()

	resp := h.do(http.MethodGet, "/api/v1/settings", nil)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("a non-admin read the settings: %d", resp.StatusCode)
	}
}
