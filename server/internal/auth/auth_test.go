package auth

import (
	"bytes"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"
)

// Cheap parameters keep the suite fast; production cost comes from config.
var testParams = Argon2Params{MemoryKiB: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}

func TestPasswordHashRoundTrip(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple", testParams)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if !strings.HasPrefix(hash, "$argon2id$") {
		t.Errorf("hash is not in argon2id PHC format: %q", hash)
	}
	if strings.Contains(hash, "correct horse") {
		t.Error("hash contains the plaintext password")
	}
	if err := VerifyPassword("correct horse battery staple", hash); err != nil {
		t.Errorf("VerifyPassword on the correct password: %v", err)
	}
	if err := VerifyPassword("wrong password entirely", hash); !errors.Is(err, ErrHashMismatch) {
		t.Errorf("VerifyPassword on a wrong password = %v, want ErrHashMismatch", err)
	}
}

func TestPasswordHashIsSaltedPerCall(t *testing.T) {
	first, _ := HashPassword("same password", testParams)
	second, _ := HashPassword("same password", testParams)

	if first == second {
		t.Error("identical passwords produced identical hashes; salt is not applied")
	}
}

func TestVerifyPasswordRejectsMalformedHashes(t *testing.T) {
	for name, hash := range map[string]string{
		"empty":          "",
		"not phc":        "plaintext",
		"wrong alg":      "$bcrypt$v=19$m=65536,t=3,p=4$c2FsdA$aGFzaA",
		"missing fields": "$argon2id$v=19$m=65536",
	} {
		t.Run(name, func(t *testing.T) {
			if err := VerifyPassword("anything", hash); err == nil {
				t.Error("VerifyPassword accepted a malformed hash")
			}
		})
	}
}

func TestPasswordPolicyRejectsWeakPasswords(t *testing.T) {
	cases := map[string]struct {
		password string
		want     string
	}{
		"too short":        {"Ab1!xyz", "at least 12"},
		"one class":        {"aaaaaaaaaaaaaaaa", "three of"},
		"common":           {"password123!", "commonly used"},
		"sequential":       {"abcdefghijklm", "simple sequence"},
		"contains email":   {"erolbeyaz-Secret1!", "email address"},
		"contains appname": {"Kubby-Secret-123!", "application name"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := ValidatePassword(tc.password, "erolbeyaz@example.com", "Erol")
			if err == nil {
				t.Fatalf("ValidatePassword(%q) accepted a weak password", tc.password)
			}
			if !errors.Is(err, ErrWeakPassword) {
				t.Errorf("error is not ErrWeakPassword: %v", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

func TestPasswordPolicyAcceptsStrongPasswords(t *testing.T) {
	for _, password := range []string{
		"Tr0ubador&Horse!", "9xK-quiet-Meadow-vault", "Zg7#pluto-ORBIT-quay",
	} {
		if err := ValidatePassword(password, "erolbeyaz@example.com", "Erol"); err != nil {
			t.Errorf("ValidatePassword(%q) = %v, want nil", password, err)
		}
	}
}

// An operator should be able to fix every problem in one pass.
func TestPasswordPolicyReportsAllProblems(t *testing.T) {
	problems := PasswordProblems("short", "erolbeyaz@example.com", "Erol")
	if len(problems) < 2 {
		t.Errorf("got %d problems (%v), want at least length and character-class", len(problems), problems)
	}
}

func TestTokensAreUniqueAndStoredHashed(t *testing.T) {
	token, hash, err := NewToken()
	if err != nil {
		t.Fatalf("NewToken: %v", err)
	}
	if token == "" || hash == "" {
		t.Fatal("NewToken returned an empty value")
	}
	if strings.Contains(hash, token) {
		t.Error("stored hash contains the token itself")
	}
	if HashToken(token) != hash {
		t.Error("HashToken is not deterministic")
	}

	seen := map[string]bool{token: true}
	for range 100 {
		next, _, err := NewToken()
		if err != nil {
			t.Fatalf("NewToken: %v", err)
		}
		if seen[next] {
			t.Fatal("NewToken produced a duplicate")
		}
		seen[next] = true
	}
}

func TestTOTPEnrollmentAndVerification(t *testing.T) {
	enrollment, err := NewTOTPEnrollment("Kubby", "erolbeyaz@example.com")
	if err != nil {
		t.Fatalf("NewTOTPEnrollment: %v", err)
	}
	if !strings.HasPrefix(enrollment.URI, "otpauth://totp/") {
		t.Errorf("URI = %q, want an otpauth:// URI", enrollment.URI)
	}

	now := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	code, err := generateCode(t, enrollment.Secret, now)
	if err != nil {
		t.Fatalf("generate code: %v", err)
	}

	if err := VerifyTOTP(code, enrollment.Secret, now); err != nil {
		t.Errorf("VerifyTOTP on a valid code: %v", err)
	}
	if err := VerifyTOTP(code, enrollment.Secret, now.Add(5*time.Minute)); !errors.Is(err, ErrInvalidTOTP) {
		t.Error("VerifyTOTP accepted a code five minutes out of window")
	}
	if err := VerifyTOTP("000000", enrollment.Secret, now); !errors.Is(err, ErrInvalidTOTP) {
		t.Error("VerifyTOTP accepted an obviously wrong code")
	}
	if err := VerifyTOTP("", enrollment.Secret, now); !errors.Is(err, ErrInvalidTOTP) {
		t.Error("VerifyTOTP accepted an empty code")
	}
}

// Users retype recovery codes by hand, so formatting must not matter.
func TestRecoveryCodesAreSingleUseAndFormatInsensitive(t *testing.T) {
	codes, hashes, err := NewRecoveryCodes(testParams)
	if err != nil {
		t.Fatalf("NewRecoveryCodes: %v", err)
	}
	if len(codes) != RecoveryCodeCount || len(hashes) != RecoveryCodeCount {
		t.Fatalf("got %d codes and %d hashes, want %d of each", len(codes), len(hashes), RecoveryCodeCount)
	}

	for i, code := range codes {
		if strings.Contains(hashes[i], code) {
			t.Fatal("hash contains the recovery code")
		}
	}

	target := codes[3]
	for _, variant := range []string{target, strings.ToUpper(target), strings.ReplaceAll(target, "-", ""), " " + target + " "} {
		if got := MatchRecoveryCode(variant, hashes); got != 3 {
			t.Errorf("MatchRecoveryCode(%q) = %d, want 3", variant, got)
		}
	}

	if got := MatchRecoveryCode("not-a-real-code", hashes); got != -1 {
		t.Errorf("MatchRecoveryCode on an unknown code = %d, want -1", got)
	}
}

// Typing a 32-character secret by hand is error-prone, so enrolment must produce a
// scannable code, not just the secret.
func TestTOTPEnrollmentIncludesAScannableQRCode(t *testing.T) {
	enrollment, err := NewTOTPEnrollment("Kubby", "erolbeyaz@example.com")
	if err != nil {
		t.Fatalf("NewTOTPEnrollment: %v", err)
	}

	if !strings.HasPrefix(enrollment.QRCodePNG, "data:image/png;base64,") {
		t.Fatalf("QRCodePNG = %.40q, want a base64 PNG data URI", enrollment.QRCodePNG)
	}

	payload := strings.TrimPrefix(enrollment.QRCodePNG, "data:image/png;base64,")
	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		t.Fatalf("QR code payload is not valid base64: %v", err)
	}
	if !bytes.HasPrefix(decoded, []byte("\x89PNG\r\n\x1a\n")) {
		t.Error("QR code payload is not a PNG")
	}
	if len(decoded) < 200 {
		t.Errorf("QR code PNG is only %d bytes; it is unlikely to be a real image", len(decoded))
	}
}
