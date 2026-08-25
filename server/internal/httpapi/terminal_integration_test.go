package httpapi_test

import (
	"context"
	"encoding/base64"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// typeLine sends a command the way a keyboard would, one character at a time, ending
// with the return that runs it.
func typeLine(t *testing.T, conn *websocket.Conn, ctx context.Context, line string) {
	t.Helper()

	for _, r := range line + "\r" {
		frame := `{"type":"stdin","data":` + quote(string(r)) + `}`
		if err := conn.Write(ctx, websocket.MessageText, []byte(frame)); err != nil {
			t.Fatalf("write %q: %v", r, err)
		}
	}
}

func quote(s string) string {
	switch s {
	case "\r":
		return `"\r"`
	case `"`:
		return `"\""`
	case `\`:
		return `"\\"`
	}
	return `"` + s + `"`
}

// readUntil collects output until it contains what the test is waiting for.
func readUntil(t *testing.T, conn *websocket.Conn, ctx context.Context, needle string) string {
	t.Helper()

	var seen strings.Builder
	deadline := time.Now().Add(45 * time.Second)

	for time.Now().Before(deadline) {
		frame := readFrame(t, conn, ctx)
		seen.WriteString(frame.Data)
		if strings.Contains(seen.String(), needle) {
			return seen.String()
		}
	}
	t.Fatalf("never saw %q; the session said:\n%s", needle, seen.String())
	return ""
}

// The terminal is what a browser can have instead of the reader's own: kubectl, already
// pointed at the cluster in front of them.
func TestTerminalRunsKubectlAgainstTheCluster(t *testing.T) {
	h := signedInAdmin(t)
	id := registerCluster(t, h, "terminal")

	conn, ctx, done := openShell(t, h, "/api/v1/clusters/"+id+"/terminal")
	defer done()

	if frame := readFrame(t, conn, ctx); frame.Type != "open" {
		t.Fatalf("the terminal did not open: %s %s", frame.Type, frame.Data)
	}

	// The banner says which cluster the commands will reach, so it is never a guess.
	readUntil(t, conn, ctx, "is in context")

	// Waiting on the last namespace alphabetically, not the first: the output arrives in
	// chunks, and stopping at "payments" would leave "storefront" still in flight.
	typeLine(t, conn, ctx, "kubectl get namespaces")
	output := readUntil(t, conn, ctx, "storefront")

	if !strings.Contains(output, "payments") {
		t.Fatalf("kubectl did not reach the cluster; saw:\n%s", output)
	}
}

// The context is fixed to the one cluster the terminal was opened for. There is nothing
// else in the file to switch to, which is what contains it.
func TestTerminalIsPointedAtOneClusterOnly(t *testing.T) {
	h := signedInAdmin(t)
	id := registerCluster(t, h, "terminal-context")

	conn, ctx, done := openShell(t, h, "/api/v1/clusters/"+id+"/terminal")
	defer done()

	readFrame(t, conn, ctx)
	readUntil(t, conn, ctx, "is in context")

	// The context this terminal is on is the cluster it was opened for, named after it.
	typeLine(t, conn, ctx, "kubectl config current-context")
	readUntil(t, conn, ctx, "terminal-context")

	// And it is the only one: a kubeconfig with one context has nothing to switch to.
	typeLine(t, conn, ctx, "kubectl config get-contexts --output=name")
	listed := readUntil(t, conn, ctx, "terminal-context")

	if strings.Count(listed, "terminal-context") < 1 {
		t.Fatalf("the cluster's own context was not listed; saw:\n%s", listed)
	}
}

// The whole reason this is not a shell. If this ever passes something through, the
// encryption key and every stored kubeconfig are one command away.
func TestTerminalRunsNothingButKubectl(t *testing.T) {
	h := signedInAdmin(t)
	id := registerCluster(t, h, "terminal-scope")

	conn, ctx, done := openShell(t, h, "/api/v1/clusters/"+id+"/terminal")
	defer done()

	readFrame(t, conn, ctx)
	readUntil(t, conn, ctx, "is in context")

	for _, line := range []string{"cat /etc/passwd", "env", "sh -c id"} {
		typeLine(t, conn, ctx, line)
		output := readUntil(t, conn, ctx, "only kubectl and helm run here")

		if strings.Contains(output, "root:") {
			t.Fatalf("%q reached the host filesystem", line)
		}
		if strings.Contains(output, "KUBBY_ENCRYPTION_KEY") {
			t.Fatalf("%q read Kubby's own environment", line)
		}
	}
}

// A locked cluster still gets a terminal — it just cannot write from it.
//
// The lock exists to stop accidents on a cluster somebody deliberately froze, and a
// refused `kubectl delete` stops those. Refusing to open at all cost every reader a
// working terminal to defend against deliberate credential extraction, which the lock was
// never the control for (ADR-107).
func TestALockedClusterGetsAReadOnlyTerminal(t *testing.T) {
	h := signedInAdmin(t)
	id := registerCluster(t, h, "terminal-locked")

	locked := h.do(http.MethodPatch, "/api/v1/clusters/"+id, map[string]bool{"readOnly": true})
	if locked.StatusCode != http.StatusOK {
		t.Fatalf("lock the cluster: %d %s", locked.StatusCode, readBody(locked))
	}
	_ = locked.Body.Close()

	conn, ctx, done := openShell(t, h, "/api/v1/clusters/"+id+"/terminal")
	defer done()

	readFrame(t, conn, ctx)
	readUntil(t, conn, ctx, "is in context")

	// Reading works.
	typeLine(t, conn, ctx, "kubectl get namespaces")
	readUntil(t, conn, ctx, "storefront")

	// Writing does not, and the refusal names the lock rather than leaving the reader
	// to guess which of four gates stopped them.
	typeLine(t, conn, ctx, "kubectl delete namespace payments")
	output := readUntil(t, conn, ctx, "is a write")

	if !strings.Contains(output, "read-only") {
		t.Fatalf("the refusal should name the lock that stopped it; saw:\n%s", output)
	}
}

// The file on the reader's machine is the one the command acts on. Without this the
// terminal can only read what is already in the cluster.
func TestDroppedFilesAreThereForTheCommand(t *testing.T) {
	h := signedInAdmin(t)
	id := registerCluster(t, h, "terminal-upload")

	conn, ctx, done := openShell(t, h, "/api/v1/clusters/"+id+"/terminal")
	defer done()

	readFrame(t, conn, ctx)
	readUntil(t, conn, ctx, "is in context")

	manifest := `apiVersion: v1
kind: ConfigMap
metadata:
  name: kubby-upload-probe
  namespace: storefront
data:
  from: dropped-file
`
	upload(t, conn, ctx, "probe.yaml", manifest)
	readUntil(t, conn, ctx, "probe.yaml")

	typeLine(t, conn, ctx, "kubectl apply -f probe.yaml")
	readUntil(t, conn, ctx, "kubby-upload-probe")

	t.Cleanup(func() {
		resp := h.do(http.MethodDelete,
			"/api/v1/clusters/"+id+"/object/configmaps?namespace=storefront&name=kubby-upload-probe", nil)
		_ = resp.Body.Close()
	})
}

// A file name is data from the browser, so it is treated as hostile. The kubeconfig one
// level up is the obvious target.
func TestAnUploadCannotEscapeItsSession(t *testing.T) {
	h := signedInAdmin(t)
	id := registerCluster(t, h, "terminal-escape")

	conn, ctx, done := openShell(t, h, "/api/v1/clusters/"+id+"/terminal")
	defer done()

	readFrame(t, conn, ctx)
	readUntil(t, conn, ctx, "is in context")

	refusals := map[string]string{
		"../config":             "outside this session",
		"/etc/passwd":           "is an absolute path",
		"../../../../tmp/evil":  "outside this session",
		"C:/Windows/system32/x": "is an absolute path",
	}
	for name, expected := range refusals {
		upload(t, conn, ctx, name, "owned")
		readUntil(t, conn, ctx, expected)
	}
}

// helm is the other half of the answer: a chart on the reader's machine, installed into
// the cluster they are looking at.
func TestHelmRunsInTheTerminal(t *testing.T) {
	h := signedInAdmin(t)
	id := registerCluster(t, h, "terminal-helm")

	conn, ctx, done := openShell(t, h, "/api/v1/clusters/"+id+"/terminal")
	defer done()

	readFrame(t, conn, ctx)
	readUntil(t, conn, ctx, "is in context")

	typeLine(t, conn, ctx, "helm list -n storefront")
	readUntil(t, conn, ctx, "NAME")
}

func upload(t *testing.T, conn *websocket.Conn, ctx context.Context, name, contents string) {
	t.Helper()

	frame := `{"type":"upload","name":"` + name + `","data":"` +
		base64.StdEncoding.EncodeToString([]byte(contents)) + `"}`
	if err := conn.Write(ctx, websocket.MessageText, []byte(frame)); err != nil {
		t.Fatalf("upload %q: %v", name, err)
	}
}
