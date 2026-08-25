package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/erolbeyaz/kubby/internal/audit"
)

type shellFrame struct {
	Type string `json:"type"`
	Data string `json:"data"`
}

// openShell dials the session socket and returns it.
func openShell(t *testing.T, h *harness, path string) (*websocket.Conn, context.Context, func()) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	endpoint := "ws" + strings.TrimPrefix(h.server.URL, "http") + path

	conn, _, err := websocket.Dial(ctx, endpoint, &websocket.DialOptions{
		HTTPClient: h.client,
		HTTPHeader: http.Header{"Origin": {h.server.URL}},
	})
	if err != nil {
		cancel()
		t.Fatalf("the shell endpoint did not upgrade: %v", err)
	}
	return conn, ctx, func() {
		_ = conn.CloseNow()
		cancel()
	}
}

func readFrame(t *testing.T, conn *websocket.Conn, ctx context.Context) shellFrame {
	t.Helper()

	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read frame: %v", err)
	}

	var frame shellFrame
	if err := json.Unmarshal(data, &frame); err != nil {
		t.Fatalf("frame is not JSON: %s", data)
	}
	return frame
}

// shellablePod creates a pod whose image actually has a shell.
//
// Not just any running pod: the seeded cluster deliberately runs images that have none —
// a StatefulSet on registry.k8s.io/pause, for one — and a shell test against those would
// be testing the error message.
func shellablePod(t *testing.T, h *harness, id, suffix string) string {
	t.Helper()

	// Its own name per test: two tests sharing one would race the first one's deletion.
	name := "kubby-shell-probe-" + suffix
	created := h.do(http.MethodPost, "/api/v1/clusters/"+id+"/apply", map[string]any{
		"dryRun": false,
		"manifest": `apiVersion: v1
kind: Pod
metadata:
  name: ` + name + `
  namespace: payments
spec:
  containers:
    - name: shell
      image: busybox:1.36
      command: ["sh", "-c", "sleep 600"]
`,
	})
	if created.StatusCode != http.StatusOK {
		t.Fatalf("create probe pod: %d %s", created.StatusCode, readBody(created))
	}
	_ = created.Body.Close()

	t.Cleanup(func() {
		resp := h.do(http.MethodDelete,
			"/api/v1/clusters/"+id+"/object/pods?namespace=payments&name="+name, nil)
		_ = resp.Body.Close()
	})

	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		resp := h.do(http.MethodGet, "/api/v1/clusters/"+id+"/resources/pods?namespace=payments", nil)
		rows := decode[listBody](t, resp).Rows
		_ = resp.Body.Close()

		for _, row := range rows {
			if row.Name == name && row.Fields["status"] == "Running" {
				return name
			}
		}
		time.Sleep(time.Second)
	}
	t.Skip("the probe pod did not start")
	return ""
}

// The session runs server-side: the browser talks to Kubby and Kubby talks to the API
// server. Nothing is needed on the reader's machine, which is the whole point (ADR-064).
func TestPodShellRunsCommandsInTheContainer(t *testing.T) {
	h := signedInAdmin(t)
	id := registerCluster(t, h, "shell")
	pod := shellablePod(t, h, id, "exec")

	conn, ctx, done := openShell(t, h,
		"/api/v1/clusters/"+id+"/pod/payments/"+pod+"/shell")
	defer done()

	if frame := readFrame(t, conn, ctx); frame.Type != "open" {
		t.Fatalf("first frame = %+v, want an open frame", frame)
	}

	marker := "kubby-shell-probe-7f3a"
	send := shellFrame{Type: "stdin", Data: "echo " + marker + "\n"}
	payload, _ := json.Marshal(send)
	if err := conn.Write(ctx, websocket.MessageText, payload); err != nil {
		t.Fatalf("write: %v", err)
	}

	// The shell echoes the command and then its output, so the marker arrives twice; one
	// is enough to prove the far end is a real shell.
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		frame := readFrame(t, conn, ctx)
		if frame.Type == "error" {
			t.Fatalf("session reported: %s", frame.Data)
		}
		if strings.Contains(frame.Data, marker) {
			return
		}
	}
	t.Fatal("the shell never echoed what was typed")
}

// A tool that hands out cluster-wide shells and keeps no record of what was typed cannot
// be audited (ADR-013 #5).
func TestShellSessionsAreRecorded(t *testing.T) {
	h := signedInAdmin(t)
	id := registerCluster(t, h, "shell-audit")
	pod := shellablePod(t, h, id, "audit")

	conn, ctx, done := openShell(t, h, "/api/v1/clusters/"+id+"/pod/payments/"+pod+"/shell")
	readFrame(t, conn, ctx)

	typed := "whoami\n"
	payload, _ := json.Marshal(shellFrame{Type: "stdin", Data: typed})
	_ = conn.Write(ctx, websocket.MessageText, payload)
	time.Sleep(500 * time.Millisecond)

	_ = conn.Close(websocket.StatusNormalClosure, "")
	done()
	time.Sleep(1500 * time.Millisecond)

	events := h.do(http.MethodGet, "/api/v1/audit", nil)
	defer func() { _ = events.Body.Close() }()
	trail := decode[struct {
		Events []struct {
			Action  string         `json:"action"`
			Details map[string]any `json:"details"`
		} `json:"events"`
	}](t, events)

	var opened, transcript bool
	for _, event := range trail.Events {
		switch event.Action {
		case audit.ActionPodShellOpened:
			opened = true
		case audit.ActionShellTranscript:
			transcript = true
			if text, _ := event.Details["transcript"].(string); !strings.Contains(text, "whoami") {
				t.Errorf("the transcript does not hold what was typed: %q", text)
			}
		}
	}
	if !opened {
		t.Error("opening a shell was not recorded")
	}
	if !transcript {
		t.Error("no transcript was recorded when the session ended")
	}
}

// Root on the machine is not something a tool turns on for you.
func TestNodeShellIsRefusedUntilItIsEnabled(t *testing.T) {
	h := signedInAdmin(t)
	id := registerCluster(t, h, "node-shell-off")

	nodes := h.do(http.MethodGet, "/api/v1/clusters/"+id+"/resources/nodes", nil)
	rows := decode[listBody](t, nodes).Rows
	_ = nodes.Body.Close()
	if len(rows) == 0 {
		t.Skip("no nodes")
	}

	resp := h.do(http.MethodGet, "/api/v1/clusters/"+id+"/node/"+rows[0].Name+"/shell", nil)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("returned %d, want 403 while node shells are off", resp.StatusCode)
	}
	if !strings.Contains(readBody(resp), "turned off") {
		t.Errorf("the refusal does not say why: %s", readBody(resp))
	}
}

// A shell is a write in every sense that matters, so a locked cluster refuses it.
func TestALockedClusterRefusesAShell(t *testing.T) {
	h := signedInAdmin(t)
	id := registerCluster(t, h, "shell-locked")
	pod := firstPod(t, h, id)

	lock := h.do(http.MethodPatch, "/api/v1/clusters/"+id, map[string]any{"readOnly": true})
	_ = lock.Body.Close()

	resp := h.do(http.MethodGet, "/api/v1/clusters/"+id+"/pod/payments/"+pod+"/shell", nil)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("returned %d, want 403 on a locked cluster", resp.StatusCode)
	}
}

// A distroless image has no shell on purpose. The answer is to bring one alongside it
// rather than to rebuild the image (ADR-013 #4).
func TestADebugContainerOpensAShellInAPodThatHasNone(t *testing.T) {
	h := signedInAdmin(t)
	id := registerCluster(t, h, "debug")

	// registry.k8s.io/pause is the seeded StatefulSet's image and has no shell at all,
	// which is exactly the case this exists for.
	pod := shellessPod(t, h, id)

	conn, ctx, done := openShell(t, h,
		"/api/v1/clusters/"+id+"/pod/payments/"+pod+"/debug")
	defer done()

	if frame := readFrame(t, conn, ctx); frame.Type != "open" {
		t.Fatalf("the debug session did not open: %s %s", frame.Type, frame.Data)
	}

	marker := "kubby-debug-worked"
	if err := conn.Write(ctx, websocket.MessageText,
		[]byte(`{"type":"stdin","data":"echo `+marker+"\\n\"}")); err != nil {
		t.Fatalf("write stdin: %v", err)
	}

	deadline := time.Now().Add(20 * time.Second)
	var seen strings.Builder
	for time.Now().Before(deadline) {
		frame := readFrame(t, conn, ctx)
		if frame.Type == "error" {
			t.Fatalf("the debug session reported: %s", frame.Data)
		}
		seen.WriteString(frame.Data)
		if strings.Contains(seen.String(), marker) {
			return
		}
	}
	t.Fatalf("the debug container never echoed back; saw %q", seen.String())
}

// shellessPod creates a pod whose image has no shell, so the debug path is exercised
// against the case it was built for rather than against a busybox that never needed it.
func shellessPod(t *testing.T, h *harness, id string) string {
	t.Helper()

	const name = "kubby-distroless-probe"
	created := h.do(http.MethodPost, "/api/v1/clusters/"+id+"/apply", map[string]any{
		"dryRun": false,
		"manifest": `apiVersion: v1
kind: Pod
metadata:
  name: ` + name + `
  namespace: payments
spec:
  containers:
    - name: app
      image: registry.k8s.io/pause:3.9
`,
	})
	if created.StatusCode != http.StatusOK {
		t.Fatalf("create probe pod: %d %s", created.StatusCode, readBody(created))
	}
	_ = created.Body.Close()

	t.Cleanup(func() {
		resp := h.do(http.MethodDelete,
			"/api/v1/clusters/"+id+"/object/pods?namespace=payments&name="+name, nil)
		_ = resp.Body.Close()
	})

	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		resp := h.do(http.MethodGet, "/api/v1/clusters/"+id+"/resources/pods?namespace=payments", nil)
		rows := decode[listBody](t, resp).Rows
		_ = resp.Body.Close()

		for _, row := range rows {
			if row.Name == name && row.Fields["status"] == "Running" {
				return name
			}
		}
		time.Sleep(time.Second)
	}
	t.Skip("the probe pod did not start")
	return ""
}
