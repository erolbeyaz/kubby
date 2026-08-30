package cluster_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/erolbeyaz/kubby/internal/cluster"
)

// A real application through a real tunnel.
//
// Seq is the case that motivated this: an AngularJS single-page app that reads its own
// storage on start-up. Under the path-prefix proxy it rendered as raw template
// placeholders — the page was sandboxed into an opaque origin where storage and cookies
// throw, so the app never bootstrapped. A port of its own gives it an ordinary origin,
// and nothing about it needs special handling any more.
//
//	KUBBY_TEST_KUBECONFIG=… KUBBY_TEST_FORWARD_NS=nx-apps KUBBY_TEST_FORWARD_APP=nx-seq \
//	  go test ./internal/cluster/ -run ForwardsARealApplication
func TestForwardsARealApplication(t *testing.T) {
	namespace := os.Getenv("KUBBY_TEST_FORWARD_NS")
	name := os.Getenv("KUBBY_TEST_FORWARD_APP")
	if namespace == "" || name == "" {
		t.Skip("KUBBY_TEST_FORWARD_NS and KUBBY_TEST_FORWARD_APP are not set")
	}

	svc, c := seededCluster(t)

	target, err := svc.ResolveForward(context.Background(), c, "apps/deployments", namespace, name, 80, nil)
	if err != nil {
		t.Fatalf("ResolveForward: %v", err)
	}

	forward, err := svc.ListenForward(context.Background(), c, *target, nil,
		"127.0.0.1", 0, cluster.PortRange{}, nil, func(err error) { t.Errorf("dial: %v", err) })
	if err != nil {
		t.Fatalf("ListenForward: %v", err)
	}
	defer forward.Close()

	base := fmt.Sprintf("http://127.0.0.1:%d", forward.Port)
	client := &http.Client{Timeout: 20 * time.Second}

	response, err := client.Get(base + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	page, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	_ = response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET / -> %d", response.StatusCode)
	}
	if !strings.Contains(string(page), "<html") {
		t.Fatalf("the page is not HTML: %.200s", page)
	}

	// The assets the page names. These are what the reader was missing: the app arrived
	// and the files that render it did not.
	var assets []string
	for _, part := range strings.Split(string(page), `"`) {
		if strings.HasSuffix(part, ".js") || strings.HasSuffix(part, ".css") {
			assets = append(assets, strings.TrimPrefix(part, "/"))
		}
	}
	if len(assets) == 0 {
		t.Fatal("the page names no scripts or stylesheets; the wrong thing answered")
	}

	for _, asset := range assets[:min(4, len(assets))] {
		r, err := client.Get(base + "/" + asset)
		if err != nil {
			t.Errorf("GET %s: %v", asset, err)
			continue
		}
		_, _ = io.Copy(io.Discard, r.Body)
		_ = r.Body.Close()
		if r.StatusCode != http.StatusOK {
			t.Errorf("GET %s -> %d, want 200", asset, r.StatusCode)
		}
	}
}
