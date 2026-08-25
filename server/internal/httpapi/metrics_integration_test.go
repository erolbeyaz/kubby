package httpapi_test

import (
	"net/http"
	"strings"
	"testing"
)

// Kubby's own metrics name every cluster it talks to and how much of everything it holds.
// That is a map of the estate, and an unauthenticated /metrics hands it to anything that
// can reach the port.
func TestMetricsAreNotPublic(t *testing.T) {
	h := newHarness(t)

	resp := h.do(http.MethodGet, "/metrics", nil)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusOK {
		t.Fatalf("/metrics answered %d without a session", resp.StatusCode)
	}
}

func TestAnAdminCanReadMetrics(t *testing.T) {
	h := signedInAdmin(t)

	resp := h.do(http.MethodGet, "/metrics", nil)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/metrics answered %d for an admin", resp.StatusCode)
	}

	body := readBody(resp)
	for _, want := range []string{
		"kubby_http_requests_total",
		"kubby_http_request_duration_seconds",
		"kubby_audit_shipping_up",
		"kubby_login_attempts_total",
		// The runtime collectors: a tool holding informer caches for a fleet is one
		// whose memory is worth watching.
		"go_goroutines",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("%s is not exported", want)
		}
	}
}

// A plain user has no business reading the estate's shape.
func TestAPlainUserCannotReadMetrics(t *testing.T) {
	h := signedInAdmin(t)

	member := h.do(http.MethodPost, "/api/v1/users", map[string]string{
		"email": "reader@example.com", "displayName": "Reader",
		"password": testPassword, "role": "user",
	})
	_ = member.Body.Close()

	logout := h.do(http.MethodPost, "/api/v1/auth/logout", nil)
	_ = logout.Body.Close()
	login := h.login("reader@example.com", testPassword)
	_ = login.Body.Close()

	resp := h.do(http.MethodGet, "/metrics", nil)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("a plain user read /metrics: %d", resp.StatusCode)
	}
}

// Labelled by chi's route pattern, never by the path. One series per cluster per kind per
// object name is how a metrics endpoint takes down the Prometheus scraping it.
func TestRequestMetricsAreLabelledByRouteNotPath(t *testing.T) {
	h := signedInAdmin(t)
	id := registerCluster(t, h, "metrics-cardinality")

	// Several distinct paths that share one route pattern.
	for _, path := range []string{
		"/api/v1/clusters/" + id + "/resources/pods?namespace=payments",
		"/api/v1/clusters/" + id + "/resources/services?namespace=payments",
		"/api/v1/clusters/" + id + "/resources/nodes",
	} {
		resp := h.do(http.MethodGet, path, nil)
		_ = resp.Body.Close()
	}

	resp := h.do(http.MethodGet, "/metrics", nil)
	body := readBody(resp)
	_ = resp.Body.Close()

	if strings.Contains(body, id) {
		t.Fatal("a cluster id reached a metric label; the series count would grow with the fleet")
	}
	if strings.Contains(body, "payments") {
		t.Fatal("a namespace reached a metric label")
	}
	if !strings.Contains(body, `route="/api/v1/clusters/{id}/resources/*"`) {
		t.Errorf("requests are not counted under their route pattern:\n%s", grepLines(body, "resources"))
	}
}

// An unmatched path must not mint a series, or anything probing random URLs can grow the
// endpoint without limit.
func TestUnmatchedPathsShareOneSeries(t *testing.T) {
	h := signedInAdmin(t)

	for _, path := range []string{"/nope/one", "/nope/two", "/nope/three"} {
		resp := h.do(http.MethodGet, path, nil)
		_ = resp.Body.Close()
	}

	resp := h.do(http.MethodGet, "/metrics", nil)
	body := readBody(resp)
	_ = resp.Body.Close()

	for _, path := range []string{"/nope/one", "/nope/two", "/nope/three"} {
		if strings.Contains(body, path) {
			t.Fatalf("%s became its own series", path)
		}
	}
	if !strings.Contains(body, `route="unmatched"`) {
		t.Error("unmatched requests are not counted at all")
	}
}

func TestLoginOutcomesAreCounted(t *testing.T) {
	h := signedInAdmin(t)

	logout := h.do(http.MethodPost, "/api/v1/auth/logout", nil)
	_ = logout.Body.Close()

	wrong := h.login("admin@example.com", "not-the-password")
	_ = wrong.Body.Close()

	right := h.login("admin@example.com", testPassword)
	_ = right.Body.Close()

	resp := h.do(http.MethodGet, "/metrics", nil)
	body := readBody(resp)
	_ = resp.Body.Close()

	if !strings.Contains(body, `kubby_login_attempts_total{result="failure"}`) {
		t.Error("a failed sign-in was not counted; a password spray would be invisible here")
	}
	if !strings.Contains(body, `kubby_login_attempts_total{result="success"}`) {
		t.Error("a successful sign-in was not counted")
	}
}

func grepLines(body, needle string) string {
	var kept []string
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(line, needle) {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}
