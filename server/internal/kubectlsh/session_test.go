package kubectlsh

import (
	"strings"
	"testing"
)

func TestSplitGroupsQuotedArguments(t *testing.T) {
	args, err := split(`kubectl get pods -l "app in (a, b)" -o json`)
	if err != nil {
		t.Fatalf("split: %v", err)
	}

	want := []string{"kubectl", "get", "pods", "-l", "app in (a, b)", "-o", "json"}
	if len(args) != len(want) {
		t.Fatalf("got %#v, want %#v", args, want)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("arg %d: got %q, want %q", i, args[i], want[i])
		}
	}
}

// Nothing here is ever handed to a shell, so an operator is text. This is the test that
// says so: if the splitter ever grew a notion of `;` it would have grown a shell.
func TestSplitTreatsShellOperatorsAsText(t *testing.T) {
	args, err := split(`kubectl get pods; rm -rf /`)
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	if args[2] != "pods;" {
		t.Fatalf("a semicolon must stay part of the argument, got %#v", args)
	}

	substitution, err := split("kubectl get $(whoami)")
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	if substitution[2] != "$(whoami)" {
		t.Fatalf("a substitution must stay literal, got %#v", substitution)
	}
}

func TestSplitRejectsAnUnclosedQuote(t *testing.T) {
	if _, err := split(`kubectl get pods -l "app=x`); err == nil {
		t.Fatal("an unclosed quote should be refused rather than guessed at")
	}
}

// The whole point of this terminal: it reaches the cluster, not the machine Kubby runs on.
func TestOnlyKubectlIsAllowed(t *testing.T) {
	s := &Session{opts: Options{Permission: Permission{MayWrite: true}}}

	for _, line := range []string{"bash", "sh -c 'id'", "cat /etc/passwd", "env", "psql"} {
		args, err := split(line)
		if err != nil {
			t.Fatalf("split %q: %v", line, err)
		}
		err = s.allow(args)
		if err == nil {
			t.Fatalf("%q was allowed; only kubectl may run here", line)
		}
		if !strings.Contains(err.Error(), "only kubectl") {
			t.Fatalf("%q was refused for the wrong reason: %v", line, err)
		}
	}
}

// A mutating command is a write and goes through the same gate as every other write,
// rather than a second one that could drift away from it.
func TestMutatingCommandsNeedTheWritePermission(t *testing.T) {
	readOnly := &Session{opts: Options{Permission: Permission{
		MayWrite:     false,
		DeniedReason: "this cluster is locked read-only",
	}}}

	for _, line := range []string{"kubectl delete pod x", "kubectl apply -f y.yaml", "kubectl scale deploy/x --replicas=3"} {
		args, _ := split(line)
		err := readOnly.allow(args)
		if err == nil {
			t.Fatalf("%q was allowed on a read-only cluster", line)
		}
		if !strings.Contains(err.Error(), "locked read-only") {
			t.Fatalf("the refusal should name the gate that stopped it: %v", err)
		}
	}

	// Reading is never gated by the write permission.
	for _, line := range []string{"kubectl get pods", "kubectl describe node n", "kubectl logs pod/x"} {
		args, _ := split(line)
		if err := readOnly.allow(args); err != nil {
			t.Fatalf("%q should be readable on a read-only cluster: %v", line, err)
		}
	}
}

func TestWritableClusterAllowsMutatingCommands(t *testing.T) {
	writable := &Session{opts: Options{Permission: Permission{MayWrite: true}}}

	args, _ := split("kubectl delete pod x")
	if err := writable.allow(args); err != nil {
		t.Fatalf("delete should be allowed when the reader may write: %v", err)
	}
}

// Each of these reaches past the cluster to the machine Kubby runs on, or needs a
// terminal this session does not have. Every refusal names the way that does work.
func TestCommandsThatReachPastTheClusterAreRefused(t *testing.T) {
	s := &Session{opts: Options{Permission: Permission{MayWrite: true}}}

	cases := map[string]string{
		"kubectl proxy":               "port on the machine",
		"kubectl cp a b":              "files on the machine",
		"kubectl exec -it pod -- sh":  "Shell action",
		"kubectl port-forward pod 80": "Port forward action",
		"kubectl plugin list":         "arbitrary binary",
		"kubectl debug pod --image=x": "Shell action",
	}

	for line, expected := range cases {
		args, _ := split(line)
		err := s.allow(args)
		if err == nil {
			t.Fatalf("%q was allowed", line)
		}
		if !strings.Contains(err.Error(), expected) {
			t.Fatalf("%q: refusal should mention %q, got %v", line, expected, err)
		}
	}
}

// A flag before the verb must not hide it: `kubectl -n x delete pod y` is still a delete.
func TestTheVerbIsFoundPastLeadingFlags(t *testing.T) {
	readOnly := &Session{opts: Options{Permission: Permission{MayWrite: false}}}

	args, _ := split("kubectl -n payments --context c delete pod x")
	if err := readOnly.allow(args); err == nil {
		t.Fatal("a delete behind flags was allowed on a read-only cluster")
	}
}
