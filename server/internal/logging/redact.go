package logging

import (
	"regexp"
	"strings"
)

// Redacted replaces sensitive values in log output. Per ADR-010 this layer is not
// optional: the logger routes every field through it before any sink sees the value.
const Redacted = "***REDACTED***"

// sensitiveKeys are matched case-insensitively as substrings of a field name.
var sensitiveKeys = []string{
	"password", "passwd", "secret", "token", "authorization", "auth",
	"kubeconfig", "credential", "apikey", "api_key", "private_key",
	"client-key-data", "client_key_data", "cookie", "session",
	"encryption_key", "totp", "bearer",
}

// sensitivePatterns catch secrets that appear inside otherwise harmless strings.
var sensitivePatterns = []*regexp.Regexp{
	// PEM blocks (private keys, certificates carrying keys).
	regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`),
	// JWT / Rancher-style bearer tokens.
	regexp.MustCompile(`eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`),
	// Authorization header values.
	regexp.MustCompile(`(?i)(bearer|basic)\s+[A-Za-z0-9._~+/=-]{8,}`),
	// kubeconfig inline credentials.
	regexp.MustCompile(`(?i)(client-key-data|token)\s*:\s*\S+`),
}

// IsSensitiveKey reports whether a field name should have its value replaced entirely.
func IsSensitiveKey(key string) bool {
	lower := strings.ToLower(key)
	for _, s := range sensitiveKeys {
		if strings.Contains(lower, s) {
			return true
		}
	}
	return false
}

// RedactString removes secret-looking substrings from a free-form value.
func RedactString(s string) string {
	for _, p := range sensitivePatterns {
		s = p.ReplaceAllString(s, Redacted)
	}
	return s
}

// RedactValue applies key-based and content-based redaction to one field.
func RedactValue(key string, value any) any {
	if IsSensitiveKey(key) {
		return Redacted
	}
	if s, ok := value.(string); ok {
		return RedactString(s)
	}
	return value
}
