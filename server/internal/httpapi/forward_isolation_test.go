package httpapi

import (
	"net/http"
	"strings"
	"testing"
)

// A forwarded page is served from Kubby's own origin. Without an opaque-origin sandbox
// the workload behind it could read the CSRF cookie, call Kubby's API with the reader's
// session, and reach every cluster they can — forwarding a port would be handing that
// workload the reader's account.
//
// Tested here rather than only through a live tunnel: this is the guarantee, and it
// should not depend on a pod in a test cluster answering.
func TestIsolateCutsTheProxiedPageOffFromKubby(t *testing.T) {
	resp := &http.Response{Header: http.Header{}}
	resp.Header.Set("Set-Cookie", "session=abc; Path=/")
	resp.Header.Add("Set-Cookie", "other=def")
	resp.Header.Set("X-Frame-Options", "DENY")
	resp.Header.Set("Content-Security-Policy", "default-src 'self'")
	resp.Header.Set("Content-Security-Policy-Report-Only", "default-src 'none'")
	resp.Header.Set("Content-Type", "text/html")

	isolate(resp)

	policy := resp.Header.Get("Content-Security-Policy")
	if !strings.HasPrefix(policy, "sandbox") {
		t.Fatalf("the page must be sandboxed, policy was %q", policy)
	}
	// With it the page is back on Kubby's origin and the whole exercise is pointless.
	if strings.Contains(policy, "allow-same-origin") {
		t.Fatalf("allow-same-origin defeats the sandbox: %q", policy)
	}
	if len(resp.Header.Values("Content-Security-Policy")) != 1 {
		t.Fatalf("the pod's own policy must be replaced, not added to: %v",
			resp.Header.Values("Content-Security-Policy"))
	}

	// Stored against Kubby's origin, the pod's cookies could overwrite Kubby's own.
	if cookies := resp.Header.Values("Set-Cookie"); len(cookies) != 0 {
		t.Fatalf("proxied cookies must not reach the browser, got %v", cookies)
	}
	if framing := resp.Header.Get("X-Frame-Options"); framing != "" {
		t.Fatalf("the pod's framing header should be gone, got %q", framing)
	}
	if reportOnly := resp.Header.Get("Content-Security-Policy-Report-Only"); reportOnly != "" {
		t.Fatalf("the pod's report-only policy should be gone, got %q", reportOnly)
	}

	// Everything else is the pod's page and is left alone.
	if resp.Header.Get("Content-Type") != "text/html" {
		t.Fatal("the page's own content type was altered")
	}
}

// A relocation must stay inside the tunnel: an app answering "/" with a redirect to
// "/login" would otherwise send the browser to Kubby's own login screen.
func TestRedirectsStayInsideTheTunnel(t *testing.T) {
	const prefix = "/api/v1/forward/abc"

	cases := map[string]string{
		"/login":              prefix + "/login",
		"/a/b?c=d":            prefix + "/a/b?c=d",
		"https://elsewhere/x": "https://elsewhere/x",
		"relative/path":       "relative/path",
		"":                    "",
	}

	for location, want := range cases {
		resp := &http.Response{Header: http.Header{}}
		if location != "" {
			resp.Header.Set("Location", location)
		}

		rewriteRedirect(resp, prefix)

		if got := resp.Header.Get("Location"); got != want {
			t.Errorf("Location %q became %q, want %q", location, got, want)
		}
	}
}
