package store

import (
	"net/netip"
	"time"

	"github.com/google/uuid"

	"github.com/erolbeyaz/kubby/internal/rbac"
)

// User is a Kubby account. TOTPSecret stays encrypted in this struct; only the auth
// layer unwraps it, and it is never serialised to the API.
type User struct {
	ID               uuid.UUID
	Email            string
	DisplayName      string
	Provider         string
	ExternalID       *string
	PasswordHash     string
	Role             rbac.Role
	TOTPSecretEnc    []byte
	TOTPConfirmedAt  *time.Time
	IsActive         bool
	FailedLoginCount int
	LockedUntil      *time.Time
	LockoutCount     int
	BlockedAt        *time.Time
	LastLoginAt      *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// HasMFA reports whether the user completed TOTP enrolment.
func (u *User) HasMFA() bool {
	return len(u.TOTPSecretEnc) > 0 && u.TOTPConfirmedAt != nil
}

// IsLocked reports whether the account is currently serving a temporary lockout.
func (u *User) IsLocked(now time.Time) bool {
	return u.LockedUntil != nil && u.LockedUntil.After(now)
}

// IsBlocked reports whether the account was blocked after repeated lockouts. Unlike a
// lockout this does not expire; an administrator must lift it.
func (u *User) IsBlocked() bool {
	return u.BlockedAt != nil
}

// LockoutRemaining reports how long a temporary lockout still has to run.
func (u *User) LockoutRemaining(now time.Time) time.Duration {
	if !u.IsLocked(now) {
		return 0
	}
	return u.LockedUntil.Sub(now)
}

// Session is a server-side, revocable session. The token itself is never stored.
type Session struct {
	ID           uuid.UUID
	UserID       uuid.UUID
	IPAddress    *netip.Addr
	UserAgent    string
	MFASatisfied bool
	CreatedAt    time.Time
	LastSeenAt   time.Time
	ExpiresAt    time.Time
	RevokedAt    *time.Time
}

// IsValid reports whether the session may still be used.
func (s *Session) IsValid(now time.Time) bool {
	return s.RevokedAt == nil && s.ExpiresAt.After(now)
}

// AuditEvent is one entry in the append-only audit stream.
type AuditEvent struct {
	ID           int64
	OccurredAt   time.Time
	ActorID      *uuid.UUID
	ActorEmail   string
	Action       string
	Result       string
	ClusterID    *uuid.UUID
	Namespace    string
	ResourceKind string
	ResourceName string
	IPAddress    *netip.Addr
	RequestID    string
	Details      map[string]any
}
