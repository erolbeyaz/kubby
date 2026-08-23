package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

type containersBody struct {
	Containers []struct {
		Name string `json:"name"`
		Role string `json:"role"`
	} `json:"containers"`
}

// firstPod returns a pod name from the seeded namespace.
func firstPod(t *testing.T, h *harness, id string) string {
	t.Helper()

	resp := h.do(http.MethodGet, "/api/v1/clusters/"+id+"/resources/pods?namespace=payments", nil)
	defer func() { _ = resp.Body.Close() }()

	body := decode[listBody](t, resp)
	if len(body.Rows) == 0 {
		t.Skip("no pods in the payments namespace")
	}
	return body.Rows[0].Name
}

// ADR-030: the application container comes first, so a log view opening on the default
// shows what the workload wrote rather than what its mesh proxy did.
func TestPodContainersAreOrderedWithTheWorkloadFirst(t *testing.T) {
	h := signedInAdmin(t)
	id := registerCluster(t, h, "logs-containers")
	pod := firstPod(t, h, id)

	resp := h.do(http.MethodGet, "/api/v1/clusters/"+id+"/pod/payments/"+pod+"/containers", nil)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("containers returned %d %s", resp.StatusCode, readBody(resp))
	}
	body := decode[containersBody](t, resp)

	if len(body.Containers) == 0 {
		t.Fatal("no containers returned")
	}
	seenSidecar := false
	for _, container := range body.Containers {
		switch container.Role {
		case "sidecar":
			seenSidecar = true
		case "app":
			if seenSidecar {
				t.Errorf("application container %q is listed after a sidecar", container.Name)
			}
		case "init":
		default:
			t.Errorf("container %q has no role", container.Name)
		}
	}
}

// A restart count raises a question it does not answer. This is where the answer lives.
func TestPodRestartsSeparateTheWorkloadFromItsPlatform(t *testing.T) {
	h := signedInAdmin(t)
	id := registerCluster(t, h, "logs-restarts")
	pod := firstPod(t, h, id)

	resp := h.do(http.MethodGet, "/api/v1/clusters/"+id+"/pod/payments/"+pod+"/restarts", nil)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("restarts returned %d %s", resp.StatusCode, readBody(resp))
	}
	body := decode[struct {
		App     int `json:"app"`
		Sidecar int `json:"sidecar"`
		Init    int `json:"init"`
		Details []struct {
			Name string `json:"name"`
			Role string `json:"role"`
		} `json:"details"`
	}](t, resp)

	if len(body.Details) == 0 {
		t.Fatal("no per-container detail returned")
	}
	for _, detail := range body.Details {
		if detail.Role == "" {
			t.Errorf("container %q has no role, so its restarts cannot be attributed", detail.Name)
		}
	}
}

// kubectl's own describers, so the output is the one people already know how to read.
func TestDescribeMatchesKubectlShape(t *testing.T) {
	h := signedInAdmin(t)
	id := registerCluster(t, h, "describe")
	pod := firstPod(t, h, id)

	resp := h.do(http.MethodGet,
		"/api/v1/clusters/"+id+"/describe/pods?namespace=payments&name="+pod, nil)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("describe returned %d %s", resp.StatusCode, readBody(resp))
	}
	text := decode[struct {
		Text string `json:"text"`
	}](t, resp).Text

	for _, want := range []string{"Name:", "Namespace:", "Status:", "Containers:"} {
		if !strings.Contains(text, want) {
			t.Errorf("describe output is missing %q", want)
		}
	}
	// ShowEvents is on: describe without its events is half the diagnosis.
	if !strings.Contains(text, "Events:") {
		t.Error("describe output carries no Events section")
	}
}

// The log endpoint is the one route that upgrades rather than replying, so nothing else
// in the suite proves the middleware chain leaves the connection hijackable. It broke in
// development for a different reason — the dev proxy answered the upgrade itself — and a
// server-side regression would have looked identical from the browser.
func TestPodLogsUpgradeToAWebSocket(t *testing.T) {
	h := signedInAdmin(t)
	id := registerCluster(t, h, "logs-stream")
	pod := firstPod(t, h, id)

	endpoint := "ws" + strings.TrimPrefix(h.server.URL, "http") +
		"/api/v1/clusters/" + id + "/pod/payments/" + pod + "/logs?tail=5"

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, endpoint, &websocket.DialOptions{
		HTTPClient: h.client,
		HTTPHeader: http.Header{"Origin": {h.server.URL}},
	})
	if err != nil {
		t.Fatalf("the log endpoint did not upgrade: %v", err)
	}
	defer func() { _ = conn.CloseNow() }()

	// The first frame names the container the server chose, which is how the picker
	// learns what an empty choice resolved to (ADR-030).
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read first frame: %v", err)
	}

	var opened struct {
		Type      string `json:"type"`
		Container string `json:"container"`
		Message   string `json:"message"`
	}
	if err := json.Unmarshal(data, &opened); err != nil {
		t.Fatalf("first frame is not JSON: %s", data)
	}
	if opened.Type != "open" {
		t.Fatalf("first frame = %+v, want an open frame", opened)
	}
	if opened.Container == "" {
		t.Error("the open frame does not say which container was opened")
	}
}

// A cross-origin upgrade would hand any page a reader's session, because the browser
// attaches the session cookie to it.
func TestPodLogsRejectAForeignOrigin(t *testing.T) {
	h := signedInAdmin(t)
	id := registerCluster(t, h, "logs-origin")
	pod := firstPod(t, h, id)

	endpoint := "ws" + strings.TrimPrefix(h.server.URL, "http") +
		"/api/v1/clusters/" + id + "/pod/payments/" + pod + "/logs"

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, endpoint, &websocket.DialOptions{
		HTTPClient: h.client,
		HTTPHeader: http.Header{"Origin": {"https://evil.example.com"}},
	})
	if err == nil {
		_ = conn.CloseNow()
		t.Fatal("an upgrade from a foreign origin was accepted")
	}
}
