// Package config loads and validates runtime configuration from the environment.
package config

import (
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	encryptionKeyBytes = 32

	// Placeholders shipped in .env.example. Booting with one of these would silently
	// encrypt every kubeconfig under a key that is public in the repository.
	placeholderEncryptionKey = "CHANGE_ME_BASE64_32_BYTES"
)

// Config is the fully validated application configuration. Every field is safe to use
// without further checks once Load returns without error.
type Config struct {
	HTTP    HTTPConfig
	DB      DBConfig
	Crypto  CryptoConfig
	Auth    AuthConfig
	Logging LoggingConfig
	K8s     K8sConfig
}

type HTTPConfig struct {
	Addr           string
	PublicURL      *url.URL
	AllowedOrigins []*url.URL
	TrustedProxies []string
	TLSCertFile    string
	TLSKeyFile     string
	ExtraCABundle  string
	ReadOnly       bool
}

// TLSEnabled reports whether Kubby terminates TLS itself rather than sitting behind
// an ingress or load balancer.
func (h HTTPConfig) TLSEnabled() bool {
	return h.TLSCertFile != "" && h.TLSKeyFile != ""
}

// SecureCookies follows the public URL scheme, not the listener: terminating TLS at an
// ingress must not downgrade cookie flags.
func (h HTTPConfig) SecureCookies() bool {
	return h.PublicURL.Scheme == "https"
}

type DBConfig struct {
	Host     string
	Port     int
	Name     string
	User     string
	Password string
	SSLMode  string
	MaxConns int32
}

// DSN renders a libpq connection string. The password is included, so the result must
// never be logged.
func (d DBConfig) DSN() string {
	return fmt.Sprintf("host=%s port=%d dbname=%s user=%s password=%s sslmode=%s",
		d.Host, d.Port, d.Name, d.User, d.Password, d.SSLMode)
}

// Redacted renders the same DSN with the password masked, for diagnostics.
func (d DBConfig) Redacted() string {
	return fmt.Sprintf("host=%s port=%d dbname=%s user=%s password=*** sslmode=%s",
		d.Host, d.Port, d.Name, d.User, d.SSLMode)
}

type CryptoConfig struct {
	EncryptionKey        []byte
	EncryptionKeyVersion int
	KMSProvider          string
}

type AuthConfig struct {
	SessionTTL         time.Duration
	RefreshTTL         time.Duration
	Argon2MemoryMiB    int
	LoginMaxAttempts   int
	LockoutDurations   []time.Duration
	MaxLockouts        int
	RequireMFAForAdmin bool
	LoginRatePerMinute float64
	LoginRateBurst     int
	APIRatePerMinute   float64
	APIRateBurst       int
}

type LoggingConfig struct {
	Level string
	Sinks []string
}

type K8sConfig struct {
	QPS               float32
	Burst             int
	Timeout           time.Duration
	InformerIdleTTL   time.Duration
	InformerMaxMemMB  int
	SidecarContainers []string
	AllowInCluster    bool
}

// Load reads configuration from the environment and validates it. All problems are
// reported together so a misconfigured deployment can be fixed in one pass.
func Load() (*Config, error) {
	var errs []error
	fail := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }

	cfg := &Config{}

	cfg.HTTP.Addr = envOr("KUBBY_HTTP_ADDR", ":8080")
	cfg.HTTP.ReadOnly = envBool("KUBBY_READ_ONLY", false)
	cfg.HTTP.TLSCertFile = os.Getenv("KUBBY_TLS_CERT_FILE")
	cfg.HTTP.TLSKeyFile = os.Getenv("KUBBY_TLS_KEY_FILE")
	cfg.HTTP.ExtraCABundle = os.Getenv("KUBBY_EXTRA_CA_BUNDLE")
	cfg.HTTP.TrustedProxies = envList("KUBBY_TRUSTED_PROXIES")

	// Extra origins for installations reachable under more than one hostname, such as
	// an internal load-balancer alias. The public URL is always trusted implicitly.
	for _, raw := range envList("KUBBY_ALLOWED_ORIGINS") {
		parsed, parseErr := url.Parse(raw)
		if parseErr != nil || parsed.Scheme == "" || parsed.Host == "" {
			fail("KUBBY_ALLOWED_ORIGINS entry %q must be an absolute origin such as https://kubby.example.com", raw)
			continue
		}
		cfg.HTTP.AllowedOrigins = append(cfg.HTTP.AllowedOrigins, parsed)
	}

	rawPublicURL := envOr("KUBBY_PUBLIC_URL", "http://localhost:8080")
	publicURL, err := url.Parse(rawPublicURL)
	switch {
	case err != nil:
		fail("KUBBY_PUBLIC_URL is not a valid URL: %w", err)
	case publicURL.Scheme != "http" && publicURL.Scheme != "https":
		fail("KUBBY_PUBLIC_URL must use http or https, got %q", publicURL.Scheme)
	case publicURL.Host == "":
		fail("KUBBY_PUBLIC_URL must include a host, got %q", rawPublicURL)
	default:
		cfg.HTTP.PublicURL = publicURL
	}

	if (cfg.HTTP.TLSCertFile == "") != (cfg.HTTP.TLSKeyFile == "") {
		fail("KUBBY_TLS_CERT_FILE and KUBBY_TLS_KEY_FILE must be set together")
	}
	for name, path := range map[string]string{
		"KUBBY_TLS_CERT_FILE":   cfg.HTTP.TLSCertFile,
		"KUBBY_TLS_KEY_FILE":    cfg.HTTP.TLSKeyFile,
		"KUBBY_EXTRA_CA_BUNDLE": cfg.HTTP.ExtraCABundle,
	} {
		if path == "" {
			continue
		}
		if _, statErr := os.Stat(path); statErr != nil {
			fail("%s points to an unreadable file %q: %w", name, path, statErr)
		}
	}

	cfg.DB.Host = envOr("KUBBY_DB_HOST", "localhost")
	cfg.DB.Name = envOr("KUBBY_DB_NAME", "kubby")
	cfg.DB.User = envOr("KUBBY_DB_USER", "kubby")
	cfg.DB.Password = os.Getenv("KUBBY_DB_PASSWORD")
	cfg.DB.SSLMode = envOr("KUBBY_DB_SSLMODE", "prefer")
	cfg.DB.Port = envInt("KUBBY_DB_PORT", 5432, &errs)
	cfg.DB.MaxConns = int32(envInt("KUBBY_DB_MAX_CONNS", 20, &errs))

	if cfg.DB.Password == "" {
		fail("KUBBY_DB_PASSWORD is required")
	}
	if cfg.DB.Password == "CHANGE_ME" {
		fail("KUBBY_DB_PASSWORD is still the .env.example placeholder")
	}
	switch cfg.DB.SSLMode {
	case "disable", "allow", "prefer", "require", "verify-ca", "verify-full":
	default:
		fail("KUBBY_DB_SSLMODE %q is not a valid libpq sslmode", cfg.DB.SSLMode)
	}

	cfg.Crypto.KMSProvider = envOr("KUBBY_KMS_PROVIDER", "none")
	cfg.Crypto.EncryptionKeyVersion = envInt("KUBBY_ENCRYPTION_KEY_VERSION", 1, &errs)
	if cfg.Crypto.KMSProvider == "none" {
		key, keyErr := parseEncryptionKey(os.Getenv("KUBBY_ENCRYPTION_KEY"))
		if keyErr != nil {
			errs = append(errs, keyErr)
		}
		cfg.Crypto.EncryptionKey = key
	}

	cfg.Auth.SessionTTL = envDuration("KUBBY_SESSION_TTL", 15*time.Minute, &errs)
	cfg.Auth.RefreshTTL = envDuration("KUBBY_REFRESH_TTL", 12*time.Hour, &errs)
	// Lockouts escalate. The list is comma separated; the last entry repeats if an
	// account is locked more times than there are entries.
	for _, raw := range envList("KUBBY_LOCKOUT_DURATIONS") {
		parsed, parseErr := time.ParseDuration(raw)
		if parseErr != nil || parsed <= 0 {
			fail("KUBBY_LOCKOUT_DURATIONS entry %q must be a positive duration such as 5m", raw)
			continue
		}
		cfg.Auth.LockoutDurations = append(cfg.Auth.LockoutDurations, parsed)
	}
	if len(cfg.Auth.LockoutDurations) == 0 {
		cfg.Auth.LockoutDurations = []time.Duration{5 * time.Minute, 10 * time.Minute}
	}
	cfg.Auth.MaxLockouts = envInt("KUBBY_MAX_LOCKOUTS", 3, &errs)
	if cfg.Auth.MaxLockouts < 1 {
		fail("KUBBY_MAX_LOCKOUTS must be at least 1, got %d", cfg.Auth.MaxLockouts)
	}
	cfg.Auth.Argon2MemoryMiB = envInt("KUBBY_ARGON2_MEMORY_MB", 64, &errs)
	cfg.Auth.LoginMaxAttempts = envInt("KUBBY_LOGIN_MAX_ATTEMPTS", 5, &errs)
	cfg.Auth.RequireMFAForAdmin = envBool("KUBBY_REQUIRE_2FA_FOR_ADMIN", true)
	cfg.Auth.LoginRatePerMinute = float64(envInt("KUBBY_LOGIN_RATE_PER_MINUTE", 10, &errs))
	cfg.Auth.LoginRateBurst = envInt("KUBBY_LOGIN_RATE_BURST", 5, &errs)
	cfg.Auth.APIRatePerMinute = float64(envInt("KUBBY_API_RATE_PER_MINUTE", 600, &errs))
	cfg.Auth.APIRateBurst = envInt("KUBBY_API_RATE_BURST", 100, &errs)

	if cfg.Auth.Argon2MemoryMiB < 16 {
		fail("KUBBY_ARGON2_MEMORY_MB must be at least 16, got %d", cfg.Auth.Argon2MemoryMiB)
	}
	if cfg.Auth.LoginMaxAttempts < 1 {
		fail("KUBBY_LOGIN_MAX_ATTEMPTS must be at least 1, got %d", cfg.Auth.LoginMaxAttempts)
	}
	if cfg.Auth.RefreshTTL <= cfg.Auth.SessionTTL {
		fail("KUBBY_REFRESH_TTL (%s) must be longer than KUBBY_SESSION_TTL (%s)",
			cfg.Auth.RefreshTTL, cfg.Auth.SessionTTL)
	}

	cfg.Logging.Level = envOr("KUBBY_LOG_LEVEL", "info")
	switch cfg.Logging.Level {
	case "debug", "info", "warn", "error":
	default:
		fail("KUBBY_LOG_LEVEL %q must be one of debug, info, warn, error", cfg.Logging.Level)
	}

	cfg.Logging.Sinks = envList("KUBBY_LOG_SINKS")
	if len(cfg.Logging.Sinks) == 0 {
		cfg.Logging.Sinks = []string{"stdout"}
	}
	for _, sink := range cfg.Logging.Sinks {
		switch sink {
		case "stdout", "elasticsearch", "loki", "syslog":
		default:
			fail("KUBBY_LOG_SINKS contains unknown sink %q", sink)
		}
	}

	cfg.K8s.QPS = float32(envInt("KUBBY_K8S_QPS", 50, &errs))
	cfg.K8s.Burst = envInt("KUBBY_K8S_BURST", 100, &errs)
	cfg.K8s.InformerMaxMemMB = envInt("KUBBY_INFORMER_MAX_MEMORY_MB", 128, &errs)
	cfg.K8s.AllowInCluster = envBool("KUBBY_ALLOW_IN_CLUSTER", false)
	cfg.K8s.Timeout = envDuration("KUBBY_K8S_TIMEOUT", 30*time.Second, &errs)
	cfg.K8s.InformerIdleTTL = envDuration("KUBBY_INFORMER_IDLE_TTL", 30*time.Minute, &errs)

	cfg.K8s.SidecarContainers = envList("KUBBY_SIDECAR_CONTAINERS")
	if len(cfg.K8s.SidecarContainers) == 0 {
		cfg.K8s.SidecarContainers = []string{"istio-proxy", "istio-init", "vault-agent"}
	}

	if len(errs) > 0 {
		return nil, fmt.Errorf("invalid configuration:\n%w", errors.Join(errs...))
	}
	return cfg, nil
}

// parseEncryptionKey enforces the key discipline from ADR-009: a missing, short, or
// placeholder key must stop the process rather than silently weaken encryption.
func parseEncryptionKey(raw string) ([]byte, error) {
	if raw == "" {
		return nil, errors.New("KUBBY_ENCRYPTION_KEY is required (generate one with `make gen-key`)")
	}
	if raw == placeholderEncryptionKey {
		return nil, errors.New("KUBBY_ENCRYPTION_KEY is still the .env.example placeholder; generate a real key with `make gen-key`")
	}

	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("KUBBY_ENCRYPTION_KEY must be standard base64: %w", err)
	}
	if len(key) != encryptionKeyBytes {
		return nil, fmt.Errorf("KUBBY_ENCRYPTION_KEY must decode to exactly %d bytes, got %d", encryptionKeyBytes, len(key))
	}
	if subtle.ConstantTimeCompare(key, make([]byte, encryptionKeyBytes)) == 1 {
		return nil, errors.New("KUBBY_ENCRYPTION_KEY must not be all zero bytes")
	}
	return key, nil
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return parsed
}

func envInt(key string, fallback int, errs *[]error) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(v)
	if err != nil {
		*errs = append(*errs, fmt.Errorf("%s must be an integer, got %q", key, v))
		return fallback
	}
	return parsed
}

func envDuration(key string, fallback time.Duration, errs *[]error) time.Duration {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(v)
	if err != nil {
		*errs = append(*errs, fmt.Errorf("%s must be a duration such as 30s or 5m, got %q", key, v))
		return fallback
	}
	return parsed
}

func envList(key string) []string {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
