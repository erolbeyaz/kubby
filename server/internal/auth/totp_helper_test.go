package auth

import (
	"testing"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

// generateCode produces a valid code for a secret, mirroring what an authenticator app
// would show at that instant.
func generateCode(t *testing.T, secret string, now time.Time) (string, error) {
	t.Helper()
	return totp.GenerateCodeCustom(secret, now, totp.ValidateOpts{
		Period:    totpPeriod,
		Skew:      totpSkewSteps,
		Digits:    totpDigits,
		Algorithm: otp.AlgorithmSHA1,
	})
}
