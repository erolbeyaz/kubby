package config

import (
	"encoding/base64"
	"strings"
	"testing"
)

func validEnv(t *testing.T) {
	t.Helper()
	t.Setenv("KUBBY_DB_PASSWORD", "local-dev-password")
	t.Setenv("KUBBY_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef")))
	t.Setenv("KUBBY_PUBLIC_URL", "https://kubby.example.com")
}

func TestLoadSucceedsWithValidEnvironment(t *testing.T) {
	validEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if got := cfg.HTTP.Addr; got != ":8080" {
		t.Errorf("default addr = %q, want :8080", got)
	}
	if !cfg.HTTP.SecureCookies() {
		t.Error("SecureCookies() = false for an https public URL, want true")
	}
	if len(cfg.Crypto.EncryptionKey) != encryptionKeyBytes {
		t.Errorf("encryption key length = %d, want %d", len(cfg.Crypto.EncryptionKey), encryptionKeyBytes)
	}
	if len(cfg.K8s.SidecarContainers) == 0 {
		t.Error("sidecar container defaults are empty; ADR-030 requires istio-proxy to be known")
	}
}

func TestLoadRejectsBadEncryptionKeys(t *testing.T) {
	cases := map[string]string{
		"missing":     "",
		"placeholder": placeholderEncryptionKey,
		"not base64":  "not-base64!!",
		"too short":   base64.StdEncoding.EncodeToString([]byte("short")),
		"all zero":    base64.StdEncoding.EncodeToString(make([]byte, encryptionKeyBytes)),
	}

	for name, key := range cases {
		t.Run(name, func(t *testing.T) {
			validEnv(t)
			t.Setenv("KUBBY_ENCRYPTION_KEY", key)

			if _, err := Load(); err == nil {
				t.Fatal("Load() succeeded, want failure: a weak key must stop startup (ADR-009)")
			}
		})
	}
}

func TestLoadAcceptsMissingKeyWhenKMSConfigured(t *testing.T) {
	validEnv(t)
	t.Setenv("KUBBY_ENCRYPTION_KEY", "")
	t.Setenv("KUBBY_KMS_PROVIDER", "vault")

	if _, err := Load(); err != nil {
		t.Fatalf("Load() with KMS provider returned error: %v", err)
	}
}

func TestLoadRejectsPlaceholderDatabasePassword(t *testing.T) {
	validEnv(t)
	t.Setenv("KUBBY_DB_PASSWORD", "CHANGE_ME")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() succeeded with the example password, want failure")
	}
	if !strings.Contains(err.Error(), "KUBBY_DB_PASSWORD") {
		t.Errorf("error %q does not name the offending variable", err)
	}
}

func TestLoadReportsEveryProblemAtOnce(t *testing.T) {
	validEnv(t)
	t.Setenv("KUBBY_LOG_LEVEL", "verbose")
	t.Setenv("KUBBY_DB_SSLMODE", "maybe")
	t.Setenv("KUBBY_K8S_TIMEOUT", "soon")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() succeeded with three invalid values, want failure")
	}
	for _, want := range []string{"KUBBY_LOG_LEVEL", "KUBBY_DB_SSLMODE", "KUBBY_K8S_TIMEOUT"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error is missing %s; an operator should see every problem in one pass:\n%v", want, err)
		}
	}
}

func TestLoadRejectsHalfConfiguredTLS(t *testing.T) {
	validEnv(t)
	t.Setenv("KUBBY_TLS_CERT_FILE", "/etc/kubby/tls/tls.crt")

	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted a certificate without a key, want failure")
	}
}

func TestRedactedDSNHidesPassword(t *testing.T) {
	db := DBConfig{Host: "pg", Port: 5432, Name: "kubby", User: "kubby", Password: "s3cr3t", SSLMode: "require"}

	if strings.Contains(db.Redacted(), "s3cr3t") {
		t.Error("Redacted() leaked the password")
	}
	if !strings.Contains(db.DSN(), "s3cr3t") {
		t.Error("DSN() must carry the real password for the driver")
	}
}
