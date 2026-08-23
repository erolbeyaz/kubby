package httpapi_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/erolbeyaz/kubby/internal/audit"
)

type secretKeysBody struct {
	Keys []struct {
		Key   string `json:"key"`
		Bytes int    `json:"bytes"`
	} `json:"keys"`
}

// The default for a secret is masked: listing its keys says what is in it and how big,
// and never what it says.
func TestSecretKeysDoNotCarryValues(t *testing.T) {
	h := signedInAdmin(t)
	id := registerCluster(t, h, "kubby-test")

	resp := h.do(http.MethodGet, "/api/v1/clusters/"+id+"/secret/payments/api-credentials/keys", nil)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("keys returned %d %s", resp.StatusCode, readBody(resp))
	}
	body := decode[secretKeysBody](t, resp)

	if len(body.Keys) == 0 {
		t.Fatal("no keys returned")
	}
	for _, key := range body.Keys {
		if key.Bytes <= 0 {
			t.Errorf("key %q reports no size, so the panel cannot say whether a value is set", key.Key)
		}
	}
}

// A tool holding cluster-wide credentials that let secrets be read without a trace would
// be worse than no tool. The record says who read which key, and never what it said.
func TestRevealingASecretIsAudited(t *testing.T) {
	h := signedInAdmin(t)
	id := registerCluster(t, h, "kubby-test")

	keys := h.do(http.MethodGet, "/api/v1/clusters/"+id+"/secret/payments/api-credentials/keys", nil)
	body := decode[secretKeysBody](t, keys)
	_ = keys.Body.Close()
	if len(body.Keys) == 0 {
		t.Fatal("no keys to reveal")
	}
	wanted := body.Keys[0].Key

	reveal := h.do(http.MethodGet,
		"/api/v1/clusters/"+id+"/secret/payments/api-credentials/reveal?key="+wanted, nil)
	defer func() { _ = reveal.Body.Close() }()

	if reveal.StatusCode != http.StatusOK {
		t.Fatalf("reveal returned %d %s", reveal.StatusCode, readBody(reveal))
	}
	revealed := decode[struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}](t, reveal)

	if revealed.Key != wanted || revealed.Value == "" {
		t.Fatalf("reveal returned %+v", revealed)
	}

	events := h.do(http.MethodGet, "/api/v1/audit", nil)
	defer func() { _ = events.Body.Close() }()
	trail := decode[struct {
		Events []struct {
			Action       string         `json:"action"`
			ResourceKind string         `json:"resourceKind"`
			ResourceName string         `json:"resourceName"`
			Namespace    string         `json:"namespace"`
			Details      map[string]any `json:"details"`
		} `json:"events"`
	}](t, events)

	var found bool
	for _, event := range trail.Events {
		if event.Action != audit.ActionSecretRevealed {
			continue
		}
		found = true

		if event.ResourceName != "api-credentials" || event.Namespace != "payments" {
			t.Errorf("audit event does not identify the secret: %+v", event)
		}
		if event.Details["key"] != wanted {
			t.Errorf("audit event does not record which key was read: %+v", event.Details)
		}
		// Audit records access, not content.
		for field, value := range event.Details {
			if text, ok := value.(string); ok && text == revealed.Value {
				t.Errorf("the secret's value leaked into the audit record under %q", field)
			}
		}
	}
	if !found {
		t.Fatalf("no %s event was recorded", audit.ActionSecretRevealed)
	}
}

// There is no "show everything": each disclosure is its own decision and its own record.
func TestRevealRequiresAKey(t *testing.T) {
	h := signedInAdmin(t)
	id := registerCluster(t, h, "kubby-test")

	resp := h.do(http.MethodGet, "/api/v1/clusters/"+id+"/secret/payments/api-credentials/reveal", nil)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusOK {
		t.Fatalf("a reveal without a key succeeded: %s", readBody(resp))
	}
	if !strings.Contains(strings.ToLower(readBody(resp)), "key") {
		t.Errorf("the error should say a key is required")
	}
}
