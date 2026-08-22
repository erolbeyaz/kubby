package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestSensitiveFieldNamesAreRedacted(t *testing.T) {
	for _, key := range []string{"password", "Authorization", "api_key", "kubeconfig", "totp_secret", "session_token"} {
		if !IsSensitiveKey(key) {
			t.Errorf("IsSensitiveKey(%q) = false, want true", key)
		}
	}
	for _, key := range []string{"namespace", "cluster", "resource_kind", "duration_ms"} {
		if IsSensitiveKey(key) {
			t.Errorf("IsSensitiveKey(%q) = true, want false", key)
		}
	}
}

func TestSecretPatternsAreRedactedInFreeText(t *testing.T) {
	cases := map[string]string{
		"jwt":     "token is eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJhZG1pbiJ9.c2lnbmF0dXJlX2hlcmU",
		"bearer":  "Authorization: Bearer kubeconfig-user-abc123def456ghi789",
		"pem":     "-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEA\n-----END RSA PRIVATE KEY-----",
		"k8s key": "client-key-data: LS0tLS1CRUdJTiBSU0EgUFJJVkFURSBLRVk",
	}

	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			got := RedactString(input)
			if !strings.Contains(got, Redacted) {
				t.Fatalf("RedactString(%q) did not redact anything, got %q", input, got)
			}
		})
	}
}

// The logger must not leak secrets even when a caller passes them explicitly.
func TestLoggerRedactsAttributesAndMessages(t *testing.T) {
	var buf bytes.Buffer
	logger := New("info", &buf)

	logger.Info("login attempt",
		slog.String("user", "erol"),
		slog.String("password", "hunter2"),
		slog.String("cluster", "prod-app"),
	)

	out := buf.String()
	if strings.Contains(out, "hunter2") {
		t.Fatalf("password leaked into the log stream:\n%s", out)
	}
	if !strings.Contains(out, "erol") || !strings.Contains(out, "prod-app") {
		t.Fatalf("non-sensitive fields were dropped:\n%s", out)
	}

	var record map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &record); err != nil {
		t.Fatalf("log line is not valid JSON: %v\n%s", err, out)
	}
	for _, field := range []string{"timestamp", "level", "service", "message", "stream"} {
		if _, ok := record[field]; !ok {
			t.Errorf("log record is missing required field %q (ADR-010)", field)
		}
	}
	if record["password"] != Redacted {
		t.Errorf("password field = %v, want %s", record["password"], Redacted)
	}
}

func TestLoggerIncludesRequestID(t *testing.T) {
	var buf bytes.Buffer
	logger := New("info", &buf)

	ctx := WithRequestID(context.Background(), "req-abc123")
	logger.InfoContext(ctx, "handled")

	if !strings.Contains(buf.String(), "req-abc123") {
		t.Fatalf("request_id missing from log record:\n%s", buf.String())
	}
}

func TestNestedGroupsAreRedacted(t *testing.T) {
	var buf bytes.Buffer
	logger := New("info", &buf)

	logger.Info("cluster added", slog.Group("details",
		slog.String("name", "prod-app"),
		slog.String("kubeconfig", "apiVersion: v1\ntoken: abcdef123456"),
	))

	if strings.Contains(buf.String(), "abcdef123456") {
		t.Fatalf("secret inside a group leaked:\n%s", buf.String())
	}
}
