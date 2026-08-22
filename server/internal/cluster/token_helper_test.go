package cluster_test

import (
	"strings"
	"testing"
)

// replaceToken corrupts the bearer token so the API server rejects it.
func replaceToken(t *testing.T, raw []byte, _ string) []byte {
	t.Helper()

	lines := strings.Split(string(raw), "\n")
	for i, line := range lines {
		if trimmed := strings.TrimSpace(line); strings.HasPrefix(trimmed, "token:") {
			indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
			lines[i] = indent + "token: definitely-not-a-valid-token"
		}
	}
	return []byte(strings.Join(lines, "\n"))
}
