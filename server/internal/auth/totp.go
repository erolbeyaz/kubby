package auth

import (
	"bytes"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"errors"
	"fmt"
	"image/png"
	"strings"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

const (
	// A one-step window each side tolerates clock skew without meaningfully widening
	// the guessing window.
	totpSkewSteps = 1
	totpDigits    = otp.DigitsSix
	totpPeriod    = 30

	qrCodeSize = 256

	RecoveryCodeCount  = 10
	recoveryCodeBytes  = 10
	recoveryCodeGroups = 4
)

var (
	ErrInvalidTOTP         = errors.New("invalid verification code")
	ErrInvalidRecoveryCode = errors.New("invalid or already used recovery code")
)

// TOTPEnrollment is handed to the user once during setup.
type TOTPEnrollment struct {
	Secret string
	URI    string
	// QRCodePNG is a base64 data URI. Typing a 32-character secret by hand is
	// error-prone, so the QR code is the primary path and the secret the fallback.
	QRCodePNG string
}

// NewTOTPEnrollment generates a secret and the otpauth:// URI for an authenticator app.
func NewTOTPEnrollment(issuer, accountName string) (TOTPEnrollment, error) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      issuer,
		AccountName: accountName,
		Period:      totpPeriod,
		Digits:      totpDigits,
		Algorithm:   otp.AlgorithmSHA1, // required for authenticator app compatibility
	})
	if err != nil {
		return TOTPEnrollment{}, fmt.Errorf("generate TOTP secret: %w", err)
	}
	qr, err := qrCodeDataURI(key)
	if err != nil {
		return TOTPEnrollment{}, err
	}
	return TOTPEnrollment{Secret: key.Secret(), URI: key.URL(), QRCodePNG: qr}, nil
}

// qrCodeDataURI renders the otpauth URI as a PNG the browser can display inline. The
// image is produced here rather than in the browser so no QR library ships to the
// client and the strict CSP needs no extra allowance beyond data: images.
func qrCodeDataURI(key *otp.Key) (string, error) {
	image, err := key.Image(qrCodeSize, qrCodeSize)
	if err != nil {
		return "", fmt.Errorf("render TOTP QR code: %w", err)
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, image); err != nil {
		return "", fmt.Errorf("encode TOTP QR code: %w", err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

// VerifyTOTP checks a code against the secret at the given time.
func VerifyTOTP(code, secret string, now time.Time) error {
	code = strings.TrimSpace(strings.ReplaceAll(code, " ", ""))
	if code == "" {
		return ErrInvalidTOTP
	}

	ok, err := totp.ValidateCustom(code, secret, now, totp.ValidateOpts{
		Period:    totpPeriod,
		Skew:      totpSkewSteps,
		Digits:    totpDigits,
		Algorithm: otp.AlgorithmSHA1,
	})
	if err != nil || !ok {
		return ErrInvalidTOTP
	}
	return nil
}

// NewRecoveryCodes returns the plaintext codes to show once, plus the hashes to store.
// They are the fallback when the authenticator device is lost, so they are treated
// exactly like passwords: shown once, stored hashed, single use.
func NewRecoveryCodes(p Argon2Params) (codes []string, hashes []string, err error) {
	codes = make([]string, 0, RecoveryCodeCount)
	hashes = make([]string, 0, RecoveryCodeCount)

	for range RecoveryCodeCount {
		raw := make([]byte, recoveryCodeBytes)
		if _, err := rand.Read(raw); err != nil {
			return nil, nil, fmt.Errorf("generate recovery code: %w", err)
		}

		// Crockford-friendly base32 without padding, grouped for legibility.
		encoded := strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw))
		code := groupCode(encoded)

		hash, err := HashPassword(NormalizeRecoveryCode(code), p)
		if err != nil {
			return nil, nil, fmt.Errorf("hash recovery code: %w", err)
		}
		codes = append(codes, code)
		hashes = append(hashes, hash)
	}
	return codes, hashes, nil
}

// NormalizeRecoveryCode makes comparison insensitive to the formatting a user retypes.
func NormalizeRecoveryCode(code string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(code) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// MatchRecoveryCode returns the index of the stored hash the candidate matches, or -1.
// Every hash is checked so the running time does not reveal which code matched.
func MatchRecoveryCode(candidate string, hashes []string) int {
	normalized := NormalizeRecoveryCode(candidate)
	match := -1

	for i, hash := range hashes {
		if err := VerifyPassword(normalized, hash); err == nil {
			if subtle.ConstantTimeEq(int32(match), -1) == 1 {
				match = i
			}
		}
	}
	return match
}

func groupCode(s string) string {
	var b strings.Builder
	for i, r := range s {
		if i > 0 && i%recoveryCodeGroups == 0 {
			b.WriteByte('-')
		}
		b.WriteRune(r)
	}
	return b.String()
}
