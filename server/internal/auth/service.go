package auth

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/erolbeyaz/kubby/internal/crypto"
	"github.com/erolbeyaz/kubby/internal/rbac"
	"github.com/erolbeyaz/kubby/internal/store"
)

var (
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrAccountLocked      = errors.New("account is temporarily locked")
	ErrAccountBlocked     = errors.New("account is blocked after repeated failed sign-ins")
	ErrAccountInactive    = errors.New("account is deactivated")
	ErrMFARequired        = errors.New("multi-factor authentication is required")
	ErrSessionInvalid     = errors.New("session is invalid or expired")
	ErrSetupComplete      = errors.New("setup has already been completed")
	ErrLastAdmin          = errors.New("the last active administrator cannot be removed")
)

// Settings are the tunable parts of the auth flow.
type Settings struct {
	SessionTTL       time.Duration
	RefreshTTL       time.Duration
	LoginMaxAttempts int
	// LockoutDurations escalate: the first lockout uses the first entry, the second the
	// second, and so on. The last entry repeats if there are more lockouts than entries.
	LockoutDurations   []time.Duration
	MaxLockouts        int
	RequireMFAForAdmin bool
	Argon2             Argon2Params
	Issuer             string
}

// lockoutDuration returns how long the next lockout should last.
func (s Settings) lockoutDuration(previousLockouts int) time.Duration {
	if len(s.LockoutDurations) == 0 {
		return 15 * time.Minute
	}
	if previousLockouts >= len(s.LockoutDurations) {
		return s.LockoutDurations[len(s.LockoutDurations)-1]
	}
	return s.LockoutDurations[previousLockouts]
}

// Service owns the authentication and account lifecycle.
type Service struct {
	users    *store.UserRepo
	sessions *store.SessionRepo
	recovery *store.RecoveryCodeRepo
	keyring  *crypto.Keyring
	settings Settings
	nowFunc  func() time.Time
}

func NewService(db *store.DB, keyring *crypto.Keyring, settings Settings) *Service {
	return &Service{
		users:    db.Users(),
		sessions: db.Sessions(),
		recovery: db.RecoveryCodes(),
		keyring:  keyring,
		settings: settings,
		nowFunc:  func() time.Time { return time.Now().UTC() },
	}
}

func (s *Service) now() time.Time { return s.nowFunc() }

// SetupRequired reports whether the first-run wizard should be reachable.
func (s *Service) SetupRequired(ctx context.Context) (bool, error) {
	count, err := s.users.Count(ctx)
	if err != nil {
		return false, err
	}
	return count == 0, nil
}

// CreateFirstAdmin completes the setup wizard. It is the only way to create an account
// without an existing administrator, and the repository guarantees it can run once.
func (s *Service) CreateFirstAdmin(ctx context.Context, email, displayName, password string) (*store.User, error) {
	email = strings.TrimSpace(email)
	if err := ValidatePassword(password, email, displayName); err != nil {
		return nil, err
	}

	hash, err := HashPassword(password, s.settings.Argon2)
	if err != nil {
		return nil, err
	}

	user, err := s.users.CreateFirstAdmin(ctx, email, displayName, hash)
	if errors.Is(err, store.ErrSetupComplete) {
		return nil, ErrSetupComplete
	}
	return user, err
}

// LoginResult reports the outcome of a password check.
type LoginResult struct {
	User        *store.User
	Session     *store.Session
	Token       string
	MFARequired bool
}

// FailedLogin carries the detail the sign-in screen needs to explain what happened:
// how many attempts remain before a lockout, and how long a lockout still has to run.
type FailedLogin struct {
	Err               error
	AttemptsRemaining int
	LockedFor         time.Duration
	Blocked           bool
}

func (f *FailedLogin) Error() string { return f.Err.Error() }
func (f *FailedLogin) Unwrap() error { return f.Err }

// Authenticate verifies a password and issues a session.
//
// When the account requires a second factor the session is created but marked
// unsatisfied: it can only be used to complete MFA, nothing else.
func (s *Service) Authenticate(ctx context.Context, email, password string, ip *netip.Addr, userAgent string) (*LoginResult, error) {
	user, err := s.users.ByEmail(ctx, email)
	if errors.Is(err, store.ErrNotFound) {
		// Spend comparable time on an unknown account so response timing does not
		// reveal whether the address is registered.
		_ = VerifyPassword(password, dummyHash)
		return nil, &FailedLogin{Err: ErrInvalidCredentials}
	}
	if err != nil {
		return nil, err
	}

	if !user.IsActive {
		return nil, ErrAccountInactive
	}
	if user.IsBlocked() {
		return nil, &FailedLogin{Err: ErrAccountBlocked, Blocked: true}
	}
	if user.IsLocked(s.now()) {
		return nil, &FailedLogin{Err: ErrAccountLocked, LockedFor: user.LockoutRemaining(s.now())}
	}

	if err := VerifyPassword(password, user.PasswordHash); err != nil {
		lockFor := s.settings.lockoutDuration(user.LockoutCount)

		updated, recErr := s.users.RecordFailedLogin(
			ctx, user.ID, s.settings.LoginMaxAttempts, lockFor, s.settings.MaxLockouts)
		if recErr != nil {
			return nil, recErr
		}

		switch {
		case updated.IsBlocked():
			return nil, &FailedLogin{Err: ErrAccountBlocked, Blocked: true}
		case updated.IsLocked(s.now()):
			return nil, &FailedLogin{Err: ErrAccountLocked, LockedFor: updated.LockoutRemaining(s.now())}
		default:
			return nil, &FailedLogin{
				Err:               ErrInvalidCredentials,
				AttemptsRemaining: max(0, s.settings.LoginMaxAttempts-updated.FailedLoginCount),
			}
		}
	}

	mfaRequired := user.HasMFA() || rbac.Role(user.Role).RequiresMFA(s.settings.RequireMFAForAdmin)

	token, hash, err := NewToken()
	if err != nil {
		return nil, err
	}
	session, err := s.sessions.Create(ctx, user.ID, hash, ip, userAgent, !mfaRequired, s.now().Add(s.settings.RefreshTTL))
	if err != nil {
		return nil, err
	}

	if !mfaRequired {
		if err := s.users.RecordSuccessfulLogin(ctx, user.ID); err != nil {
			return nil, err
		}
	}

	return &LoginResult{User: user, Session: session, Token: token, MFARequired: mfaRequired}, nil
}

// CompleteMFA verifies a TOTP code against a session that passed the password step.
func (s *Service) CompleteMFA(ctx context.Context, session *store.Session, user *store.User, code string) error {
	if !user.HasMFA() {
		return ErrMFARequired
	}

	secret, err := s.decryptTOTPSecret(user)
	if err != nil {
		return err
	}
	if err := VerifyTOTP(code, secret, s.now()); err != nil {
		return err
	}

	if err := s.sessions.MarkMFASatisfied(ctx, session.ID); err != nil {
		return err
	}
	return s.users.RecordSuccessfulLogin(ctx, user.ID)
}

// CompleteMFAWithRecoveryCode consumes a single-use recovery code instead of a TOTP.
func (s *Service) CompleteMFAWithRecoveryCode(ctx context.Context, session *store.Session, user *store.User, code string) error {
	ids, hashes, err := s.recovery.UnusedHashes(ctx, user.ID)
	if err != nil {
		return err
	}

	index := MatchRecoveryCode(code, hashes)
	if index < 0 {
		return ErrInvalidRecoveryCode
	}
	if err := s.recovery.Consume(ctx, ids[index]); err != nil {
		return ErrInvalidRecoveryCode
	}
	if err := s.sessions.MarkMFASatisfied(ctx, session.ID); err != nil {
		return err
	}
	return s.users.RecordSuccessfulLogin(ctx, user.ID)
}

// Resolve loads the session and user behind a token, rejecting anything expired,
// revoked, deactivated or still awaiting MFA.
func (s *Service) Resolve(ctx context.Context, token string) (*store.Session, *store.User, error) {
	session, err := s.sessions.ByTokenHash(ctx, HashToken(token))
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil, ErrSessionInvalid
	}
	if err != nil {
		return nil, nil, err
	}
	if !session.IsValid(s.now()) {
		return nil, nil, ErrSessionInvalid
	}

	user, err := s.users.ByID(ctx, session.UserID)
	if err != nil {
		return nil, nil, ErrSessionInvalid
	}
	if !user.IsActive {
		return nil, nil, ErrAccountInactive
	}
	if !session.MFASatisfied {
		return session, user, ErrMFARequired
	}
	return session, user, nil
}

// Refresh rotates a session token. Rotation is atomic in the repository, so presenting
// an already-rotated token fails instead of minting a parallel session.
func (s *Service) Refresh(ctx context.Context, token string) (*store.Session, string, error) {
	newToken, newHash, err := NewToken()
	if err != nil {
		return nil, "", err
	}

	session, err := s.sessions.Rotate(ctx, HashToken(token), newHash, s.now().Add(s.settings.RefreshTTL))
	if errors.Is(err, store.ErrNotFound) {
		return nil, "", ErrSessionInvalid
	}
	if err != nil {
		return nil, "", err
	}
	return session, newToken, nil
}

func (s *Service) Logout(ctx context.Context, sessionID uuid.UUID) error {
	err := s.sessions.Revoke(ctx, sessionID)
	if errors.Is(err, store.ErrNotFound) {
		return nil // already gone; logout is idempotent
	}
	return err
}

// EnrollTOTP generates a secret and stores it encrypted, unconfirmed until the user
// proves they can produce a code from it.
func (s *Service) EnrollTOTP(ctx context.Context, user *store.User) (TOTPEnrollment, error) {
	enrollment, err := NewTOTPEnrollment(s.settings.Issuer, user.Email)
	if err != nil {
		return TOTPEnrollment{}, err
	}

	sealed, err := s.keyring.Seal([]byte(enrollment.Secret), []byte(user.ID.String()))
	if err != nil {
		return TOTPEnrollment{}, fmt.Errorf("encrypt TOTP secret: %w", err)
	}
	if err := s.users.SetTOTPSecret(ctx, user.ID, sealed); err != nil {
		return TOTPEnrollment{}, err
	}
	return enrollment, nil
}

// ConfirmTOTP activates MFA once the user submits a valid code, and returns the
// recovery codes, which are shown exactly once.
//
// The current session is marked satisfied here as well. Without it a user who is
// required to enrol would finish enrolment and still be locked out of every endpoint.
func (s *Service) ConfirmTOTP(ctx context.Context, session *store.Session, user *store.User, code string) ([]string, error) {
	secret, err := s.decryptTOTPSecret(user)
	if err != nil {
		return nil, err
	}
	if err := VerifyTOTP(code, secret, s.now()); err != nil {
		return nil, err
	}

	codes, hashes, err := NewRecoveryCodes(s.settings.Argon2)
	if err != nil {
		return nil, err
	}
	if err := s.recovery.Replace(ctx, user.ID, hashes); err != nil {
		return nil, err
	}
	if err := s.users.ConfirmTOTP(ctx, user.ID); err != nil {
		return nil, err
	}
	if session != nil && !session.MFASatisfied {
		if err := s.sessions.MarkMFASatisfied(ctx, session.ID); err != nil {
			return nil, err
		}
		if err := s.users.RecordSuccessfulLogin(ctx, user.ID); err != nil {
			return nil, err
		}
	}
	return codes, nil
}

// MFAEnrolmentPending reports that the user must set up a second factor before they can
// use the application: policy requires MFA, but nothing is enrolled yet.
func (s *Service) MFAEnrolmentPending(user *store.User) bool {
	return !user.HasMFA() && rbac.Role(user.Role).RequiresMFA(s.settings.RequireMFAForAdmin)
}

// ChangePassword verifies the current password before setting a new one and revokes
// every other session, so a stolen session cannot survive a password change.
func (s *Service) ChangePassword(ctx context.Context, user *store.User, currentPassword, newPassword string, keepSession uuid.UUID) error {
	if err := VerifyPassword(currentPassword, user.PasswordHash); err != nil {
		return ErrInvalidCredentials
	}
	if err := ValidatePassword(newPassword, user.Email, user.DisplayName); err != nil {
		return err
	}

	hash, err := HashPassword(newPassword, s.settings.Argon2)
	if err != nil {
		return err
	}
	if err := s.users.UpdatePassword(ctx, user.ID, hash); err != nil {
		return err
	}
	_, err = s.sessions.RevokeAllForUser(ctx, user.ID, &keepSession)
	return err
}

func (s *Service) decryptTOTPSecret(user *store.User) (string, error) {
	if len(user.TOTPSecretEnc) == 0 {
		return "", ErrMFARequired
	}
	plaintext, err := s.keyring.Open(user.TOTPSecretEnc, []byte(user.ID.String()))
	if err != nil {
		return "", fmt.Errorf("decrypt TOTP secret: %w", err)
	}
	return string(plaintext), nil
}

// dummyHash is a valid argon2id hash of an unguessable value. Verifying against it
// makes the unknown-account path cost roughly the same as the known-account path.
const dummyHash = "$argon2id$v=19$m=65536,t=3,p=4$c29tZXNhbHR2YWx1ZXg$C1EPYbRYgAG1TeXfHKmL5j0dLpBsBJI3T0KcMdcFH2M"
