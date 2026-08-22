package cluster

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixture loads a kubeconfig from the shared test data directory. The files are
// generated against a throwaway local cluster, so they never carry a real credential.
func fixture(t *testing.T, name string) []byte {
	t.Helper()

	path := filepath.Join("testdata", name)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("fixture %s is unavailable: %v", name, err)
	}
	return raw
}

var devPolicy = AddressPolicy{AllowLoopback: true}

func TestParseAcceptsATokenKubeconfig(t *testing.T) {
	parsed, err := ParseKubeconfig(fixture(t, "rancher-style.yaml"), devPolicy)
	if err != nil {
		t.Fatalf("ParseKubeconfig: %v", err)
	}

	if len(parsed.Contexts) != 1 {
		t.Fatalf("got %d contexts, want 1", len(parsed.Contexts))
	}
	ctx := parsed.Contexts[0]

	if ctx.AuthMethod != "token" {
		t.Errorf("AuthMethod = %q, want token", ctx.AuthMethod)
	}
	if !ctx.HasCertificateAuthority {
		t.Error("certificate authority was not detected")
	}
	if ctx.InsecureSkipTLSVerify {
		t.Error("InsecureSkipTLSVerify is true for a CA-pinned config")
	}
	if !strings.HasPrefix(ctx.Server, "https://") {
		t.Errorf("Server = %q", ctx.Server)
	}
}

// The single most dangerous input: client-go would run this command on the server.
func TestParseRejectsExecPlugins(t *testing.T) {
	_, err := ParseKubeconfig(fixture(t, "with-exec-plugin.yaml"), devPolicy)

	if !errors.Is(err, ErrExecPluginUsed) {
		t.Fatalf("ParseKubeconfig = %v, want ErrExecPluginUsed", err)
	}
	// The message must name the offending user and command so the operator can fix it.
	if !strings.Contains(err.Error(), "eks-user") || !strings.Contains(err.Error(), "aws") {
		t.Errorf("error does not identify the exec entry: %v", err)
	}
}

func TestParseListsEveryContextForSelection(t *testing.T) {
	parsed, err := ParseKubeconfig(fixture(t, "multi-context.yaml"), devPolicy)
	if err != nil {
		t.Fatalf("ParseKubeconfig: %v", err)
	}

	if len(parsed.Contexts) != 2 {
		t.Fatalf("got %d contexts, want 2", len(parsed.Contexts))
	}
	if parsed.CurrentContext != "production" {
		t.Errorf("CurrentContext = %q, want production", parsed.CurrentContext)
	}

	byName := map[string]ContextInfo{}
	for _, c := range parsed.Contexts {
		byName[c.Name] = c
	}

	if !byName["staging"].InsecureSkipTLSVerify {
		t.Error("staging context does not report insecure-skip-tls-verify; the warning would be missing")
	}
	if byName["production"].InsecureSkipTLSVerify {
		t.Error("production context wrongly reports insecure-skip-tls-verify")
	}

	if _, err := parsed.Context("staging"); err != nil {
		t.Errorf("Context(staging): %v", err)
	}
	if _, err := parsed.Context("nope"); !errors.Is(err, ErrUnknownContext) {
		t.Errorf("Context(nope) = %v, want ErrUnknownContext", err)
	}
}

func TestParseRejectsMalformedInput(t *testing.T) {
	cases := map[string][]byte{
		"empty":            {},
		"broken yaml":      fixture(t, "broken.yaml"),
		"not a kubeconfig": []byte("apiVersion: v1\nkind: Pod\nmetadata:\n  name: x\n"),
		"too large":        make([]byte, maxKubeconfigBytes+1),
	}

	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseKubeconfig(raw, devPolicy); err == nil {
				t.Error("ParseKubeconfig accepted invalid input")
			}
		})
	}
}

// A kubeconfig pointing at the cloud metadata endpoint must never be stored.
func TestParseRejectsMetadataEndpoints(t *testing.T) {
	_, err := ParseKubeconfig(fixture(t, "ssrf-metadata.yaml"), devPolicy)

	// Its only context is blocked, so the whole paste is refused.
	if !errors.Is(err, ErrNoContexts) {
		t.Fatalf("ParseKubeconfig = %v, want ErrNoContexts", err)
	}
	if !strings.Contains(err.Error(), "169.254") {
		t.Errorf("error does not mention the blocked address: %v", err)
	}
}

// An unreachable context is still listed: it may just be unreachable from where Kubby
// runs, and silently dropping it would leave the user wondering where it went.
func TestUnresolvableContextIsListedButFlagged(t *testing.T) {
	parsed, err := ParseKubeconfig(fixture(t, "multi-context.yaml"), devPolicy)
	if err != nil {
		t.Fatalf("ParseKubeconfig: %v", err)
	}

	byName := map[string]ContextInfo{}
	for _, c := range parsed.Contexts {
		byName[c.Name] = c
	}

	staging, ok := byName["staging"]
	if !ok {
		t.Fatal("the unreachable context was dropped instead of being reported")
	}
	if staging.Problem == "" {
		t.Error("staging carries no explanation of why it may not connect")
	}
	if staging.Blocked {
		t.Error("an unresolvable host must not be treated as a policy block")
	}
	if _, err := parsed.Context("staging"); err != nil {
		t.Errorf("selecting an unreachable context should be allowed: %v", err)
	}
}

// Selecting a blocked context must fail even if the caller asks for it by name.
func TestBlockedContextCannotBeSelected(t *testing.T) {
	raw := []byte(`
apiVersion: v1
kind: Config
clusters:
- {name: ok, cluster: {server: "https://127.0.0.1:6550", insecure-skip-tls-verify: true}}
- {name: meta, cluster: {server: "https://169.254.169.254", insecure-skip-tls-verify: true}}
users:
- {name: u, user: {token: t}}
contexts:
- {name: good, context: {cluster: ok, user: u}}
- {name: evil, context: {cluster: meta, user: u}}
current-context: good
`)

	parsed, err := ParseKubeconfig(raw, devPolicy)
	if err != nil {
		t.Fatalf("ParseKubeconfig: %v", err)
	}

	if _, err := parsed.Context("good"); err != nil {
		t.Errorf("the usable context was refused: %v", err)
	}
	if _, err := parsed.Context("evil"); !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("Context(evil) = %v, want ErrBlockedAddress", err)
	}
}
