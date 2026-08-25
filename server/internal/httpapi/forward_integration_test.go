package httpapi_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

type portsBody struct {
	Ports []struct {
		Name      string `json:"name"`
		Port      int    `json:"port"`
		Protocol  string `json:"protocol"`
		Container string `json:"container"`
	} `json:"ports"`
}

type forwardBody struct {
	ID        string `json:"id"`
	Namespace string `json:"namespace"`
	Pod       string `json:"pod"`
	Port      int    `json:"port"`
	URL       string `json:"url"`
}

// servingPod creates a pod that actually answers HTTP, so the tunnel is tested against
// something that talks back rather than against a port that merely exists.
func servingPod(t *testing.T, h *harness, id, suffix string) string {
	t.Helper()

	name := "kubby-forward-probe-" + suffix
	created := h.do(http.MethodPost, "/api/v1/clusters/"+id+"/apply", map[string]any{
		"dryRun": false,
		"manifest": `apiVersion: v1
kind: Pod
metadata:
  name: ` + name + `
  namespace: payments
spec:
  containers:
    - name: web
      image: busybox:1.36
      command:
        - sh
        - -c
        # A real server rather than a one-shot nc: nc writes its reply the instant a
        # connection opens, which lands as an unsolicited response whenever the proxy
        # dials ahead of sending, and the test failed at random because of it.
        - mkdir -p /www && printf 'hello kubby!' > /www/index.html && httpd -f -p 8080 -h /www
      ports:
        - name: http
          containerPort: 8080
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

// The ports offered are the object's own, so nobody has to remember a number.
func TestPortsComeFromTheObject(t *testing.T) {
	h := signedInAdmin(t)
	id := registerCluster(t, h, "ports")
	pod := servingPod(t, h, id, "ports")

	resp := h.do(http.MethodGet, "/api/v1/clusters/"+id+"/ports/payments/"+pod+"?type=pods", nil)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ports: %d %s", resp.StatusCode, readBody(resp))
	}

	ports := decode[portsBody](t, resp).Ports
	if len(ports) != 1 || ports[0].Port != 8080 || ports[0].Name != "http" {
		t.Fatalf("expected the pod's declared 8080/http, got %+v", ports)
	}
	if ports[0].Container != "web" {
		t.Fatalf("the port should name the container it belongs to, got %q", ports[0].Container)
	}
}

// The point of the whole feature: a page inside the cluster, in the browser, with nothing
// installed on the reader's machine.
func TestForwardServesThePodsOwnPages(t *testing.T) {
	h := signedInAdmin(t)
	id := registerCluster(t, h, "forward")
	pod := servingPod(t, h, id, "serve")

	started := h.do(http.MethodPost, "/api/v1/clusters/"+id+"/forwards", map[string]any{
		"type": "pods", "namespace": "payments", "name": pod, "port": 8080,
	})
	if started.StatusCode != http.StatusOK {
		t.Fatalf("start forward: %d %s", started.StatusCode, readBody(started))
	}
	forward := decode[forwardBody](t, started)
	_ = started.Body.Close()

	resp := h.do(http.MethodGet, forward.URL, nil)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("through the tunnel: %d %s", resp.StatusCode, readBody(resp))
	}
	if body := readBody(resp); body != "hello kubby!" {
		t.Fatalf("the pod's own body should come through, got %q", body)
	}
}

// A forwarded page is served from Kubby's own origin, so without an opaque-origin sandbox
// the workload behind it could read the CSRF cookie and act as the reader. This is the
// test that stops that regression.
func TestAForwardedPageCannotReachKubby(t *testing.T) {
	h := signedInAdmin(t)
	id := registerCluster(t, h, "forward-isolation")
	pod := servingPod(t, h, id, "isolate")

	started := h.do(http.MethodPost, "/api/v1/clusters/"+id+"/forwards", map[string]any{
		"type": "pods", "namespace": "payments", "name": pod, "port": 8080,
	})
	if started.StatusCode != http.StatusOK {
		t.Fatalf("start forward: %d %s", started.StatusCode, readBody(started))
	}
	forward := decode[forwardBody](t, started)
	_ = started.Body.Close()

	resp := h.do(http.MethodGet, forward.URL, nil)
	defer func() { _ = resp.Body.Close() }()

	// A vacuous pass would be worse than a failure here: assert the tunnel actually
	// carried the page before asserting anything about how it is contained.
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("through the tunnel: %d %s", resp.StatusCode, readBody(resp))
	}

	policy := resp.Header.Get("Content-Security-Policy")
	if !strings.Contains(policy, "sandbox") {
		t.Fatalf("the proxied page must be sandboxed, policy was %q", policy)
	}
	if strings.Contains(policy, "allow-same-origin") {
		t.Fatalf("allow-same-origin defeats the sandbox: %q", policy)
	}
	if framing := resp.Header.Get("X-Frame-Options"); framing != "" {
		t.Fatalf("the pod's framing header should be replaced by ours, got %q", framing)
	}
}

// A tunnel is opened under one person's permissions and is not lent to another's session.
func TestAForwardBelongsToTheReaderWhoOpenedIt(t *testing.T) {
	h := signedInAdmin(t)
	id := registerCluster(t, h, "forward-owner")
	pod := servingPod(t, h, id, "owner")

	started := h.do(http.MethodPost, "/api/v1/clusters/"+id+"/forwards", map[string]any{
		"type": "pods", "namespace": "payments", "name": pod, "port": 8080,
	})
	if started.StatusCode != http.StatusOK {
		t.Fatalf("start forward: %d %s", started.StatusCode, readBody(started))
	}
	forward := decode[forwardBody](t, started)
	_ = started.Body.Close()

	member := h.do(http.MethodPost, "/api/v1/users", map[string]string{
		"email": "second@example.com", "displayName": "Second",
		"password": testPassword, "role": "admin",
	})
	memberBody := decode[map[string]any](t, member)
	_ = member.Body.Close()
	if memberBody["id"] == nil {
		t.Fatalf("could not create the second reader")
	}

	logout := h.do(http.MethodPost, "/api/v1/auth/logout", nil)
	_ = logout.Body.Close()
	login := h.login("second@example.com", testPassword)
	_ = login.Body.Close()

	resp := h.do(http.MethodGet, forward.URL, nil)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("another reader's forward should not be reachable, got %d: %s",
			resp.StatusCode, fmt.Sprint(readBody(resp)))
	}
}

// Closing it means closed: the path stops answering rather than quietly staying open.
func TestClosingAForwardEndsIt(t *testing.T) {
	h := signedInAdmin(t)
	id := registerCluster(t, h, "forward-close")
	pod := servingPod(t, h, id, "close")

	started := h.do(http.MethodPost, "/api/v1/clusters/"+id+"/forwards", map[string]any{
		"type": "pods", "namespace": "payments", "name": pod, "port": 8080,
	})
	forward := decode[forwardBody](t, started)
	_ = started.Body.Close()

	stopped := h.do(http.MethodDelete, "/api/v1/forwards/"+forward.ID, nil)
	_ = stopped.Body.Close()
	if stopped.StatusCode != http.StatusNoContent {
		t.Fatalf("stop forward: %d", stopped.StatusCode)
	}

	resp := h.do(http.MethodGet, forward.URL, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("a closed forward should not answer, got %d", resp.StatusCode)
	}
}
