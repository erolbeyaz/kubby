package store_test

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/erolbeyaz/kubby/internal/auth"
	"github.com/erolbeyaz/kubby/internal/config"
	"github.com/erolbeyaz/kubby/internal/rbac"
	"github.com/erolbeyaz/kubby/internal/store"
)

// Integration tests need a real PostgreSQL: the repositories are mostly SQL, and an
// in-memory fake would verify nothing. Set KUBBY_TEST_DB_DSN to enable them.
//
//	KUBBY_TEST_DB_DSN="host=localhost port=5432 dbname=kubby user=kubby password=... sslmode=disable"
func testDB(t *testing.T) *store.DB {
	t.Helper()

	dsn := os.Getenv("KUBBY_TEST_DB_DSN")
	if dsn == "" {
		t.Skip("KUBBY_TEST_DB_DSN is not set; skipping database integration tests")
	}

	db, err := store.OpenDSN(context.Background(), dsn, 5)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	t.Cleanup(db.Close)
	return db
}

// uniqueEmail keeps parallel runs and repeated runs from colliding.
func uniqueEmail(prefix string) string {
	return fmt.Sprintf("%s-%s@example.com", prefix, uuid.NewString()[:8])
}

func createUser(t *testing.T, db *store.DB, role rbac.Role) *store.User {
	t.Helper()
	ctx := context.Background()

	hash, err := auth.HashPassword("Tr0ubador&Horse!", auth.Argon2Params{
		MemoryKiB: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32,
	})
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	user, err := db.Users().Create(ctx, uniqueEmail("user"), "Test User", hash, role)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Pool().Exec(context.Background(), `DELETE FROM users WHERE id = $1`, user.ID)
	})
	return user
}

func TestConfigDSNMatchesRepository(t *testing.T) {
	cfg := config.DBConfig{Host: "h", Port: 5432, Name: "n", User: "u", Password: "p", SSLMode: "disable"}
	if cfg.DSN() == "" {
		t.Fatal("DSN is empty")
	}
}

func TestUserLifecycle(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	user := createUser(t, db, rbac.RoleUser)

	t.Run("lookup is case-insensitive", func(t *testing.T) {
		found, err := db.Users().ByEmail(ctx, user.Email)
		if err != nil {
			t.Fatalf("ByEmail: %v", err)
		}
		if found.ID != user.ID {
			t.Errorf("ByEmail returned %s, want %s", found.ID, user.ID)
		}

		upper, err := db.Users().ByEmail(ctx, upperCase(user.Email))
		if err != nil {
			t.Fatalf("ByEmail with different case: %v", err)
		}
		if upper.ID != user.ID {
			t.Error("email lookup is case-sensitive")
		}
	})

	t.Run("duplicate email is rejected", func(t *testing.T) {
		_, err := db.Users().Create(ctx, user.Email, "Duplicate", user.PasswordHash, rbac.RoleUser)
		if !errors.Is(err, store.ErrEmailInUse) {
			t.Errorf("Create with a duplicate email = %v, want ErrEmailInUse", err)
		}
	})

	t.Run("unknown id reports not found", func(t *testing.T) {
		if _, err := db.Users().ByID(ctx, uuid.New()); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("ByID for an unknown id = %v, want ErrNotFound", err)
		}
	})

	t.Run("role changes persist", func(t *testing.T) {
		if err := db.Users().UpdateRole(ctx, user.ID, rbac.RoleReadOnly); err != nil {
			t.Fatalf("UpdateRole: %v", err)
		}
		reloaded, err := db.Users().ByID(ctx, user.ID)
		if err != nil {
			t.Fatalf("ByID: %v", err)
		}
		if reloaded.Role != rbac.RoleReadOnly {
			t.Errorf("role = %s, want readonly", reloaded.Role)
		}
	})
}

// Lockout must be driven by the database so concurrent attempts cannot race past the
// threshold.
func TestFailedLoginsLockTheAccount(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	user := createUser(t, db, rbac.RoleUser)

	const maxAttempts = 3
	var latest *store.User

	for i := 1; i <= maxAttempts; i++ {
		var err error
		latest, err = db.Users().RecordFailedLogin(ctx, user.ID, maxAttempts, 5*time.Minute, 3)
		if err != nil {
			t.Fatalf("RecordFailedLogin attempt %d: %v", i, err)
		}
		// The counter resets when a lockout starts, so it only tracks attempts inside
		// the current window.
		wantCount := i % maxAttempts
		if latest.FailedLoginCount != wantCount {
			t.Errorf("after attempt %d, count = %d, want %d", i, latest.FailedLoginCount, wantCount)
		}

		locked := latest.IsLocked(time.Now())
		if i < maxAttempts && locked {
			t.Errorf("account locked after %d of %d attempts", i, maxAttempts)
		}
		if i == maxAttempts && !locked {
			t.Errorf("account not locked after %d attempts", maxAttempts)
		}
	}

	if err := db.Users().RecordSuccessfulLogin(ctx, user.ID); err != nil {
		t.Fatalf("RecordSuccessfulLogin: %v", err)
	}
	reloaded, err := db.Users().ByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if reloaded.IsLocked(time.Now()) || reloaded.FailedLoginCount != 0 || reloaded.LockoutCount != 0 {
		t.Error("a successful login did not clear the lockout state")
	}
}

// Lockouts escalate and finally block a non-admin account, while an administrator is
// never blocked (an installation must stay administrable).
func TestLockoutEscalationAndAdminExemption(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	lockOut := func(u *store.User, durations []time.Duration, maxLockouts int) *store.User {
		t.Helper()
		for round := range len(durations) + 1 {
			for range 3 {
				if _, err := db.Users().RecordFailedLogin(
					ctx, u.ID, 3, durations[min(round, len(durations)-1)], maxLockouts); err != nil {
					t.Fatalf("RecordFailedLogin: %v", err)
				}
			}
			if _, err := db.Pool().Exec(ctx, `UPDATE users SET locked_until = NULL WHERE id = $1`, u.ID); err != nil {
				t.Fatalf("clear lock: %v", err)
			}
		}
		reloaded, err := db.Users().ByID(ctx, u.ID)
		if err != nil {
			t.Fatalf("ByID: %v", err)
		}
		return reloaded
	}

	durations := []time.Duration{5 * time.Minute, 10 * time.Minute}

	t.Run("a member is blocked after the third lockout", func(t *testing.T) {
		member := createUser(t, db, rbac.RoleUser)
		after := lockOut(member, durations, 3)

		if !after.IsBlocked() {
			t.Fatalf("member was not blocked after %d lockouts", after.LockoutCount)
		}
	})

	t.Run("an administrator is never blocked", func(t *testing.T) {
		admin := createUser(t, db, rbac.RoleAdmin)
		after := lockOut(admin, durations, 3)

		if after.IsBlocked() {
			t.Fatal("an administrator was blocked; the installation would be unadministrable")
		}
		if after.LockoutCount == 0 {
			t.Error("administrator lockouts were not counted at all")
		}
	})

	t.Run("unblocking clears the escalation state", func(t *testing.T) {
		member := createUser(t, db, rbac.RoleUser)
		lockOut(member, durations, 3)

		if err := db.Users().Unblock(ctx, member.ID); err != nil {
			t.Fatalf("Unblock: %v", err)
		}
		reloaded, err := db.Users().ByID(ctx, member.ID)
		if err != nil {
			t.Fatalf("ByID: %v", err)
		}
		if reloaded.IsBlocked() || reloaded.LockoutCount != 0 || reloaded.IsLocked(time.Now()) {
			t.Error("unblocking left escalation state behind")
		}
	})
}

func TestSessionLifecycle(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	user := createUser(t, db, rbac.RoleUser)

	addr := netip.MustParseAddr("198.51.100.7")
	token, hash, err := auth.NewToken()
	if err != nil {
		t.Fatalf("NewToken: %v", err)
	}

	session, err := db.Sessions().Create(ctx, user.ID, hash, &addr, "Mozilla/5.0", true, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}
	if !session.IsValid(time.Now()) {
		t.Fatal("a freshly created session is not valid")
	}
	if session.IPAddress == nil || session.IPAddress.String() != "198.51.100.7" {
		t.Errorf("IP address round trip failed: %v", session.IPAddress)
	}

	t.Run("found by token hash", func(t *testing.T) {
		found, err := db.Sessions().ByTokenHash(ctx, auth.HashToken(token))
		if err != nil {
			t.Fatalf("ByTokenHash: %v", err)
		}
		if found.ID != session.ID {
			t.Error("ByTokenHash returned a different session")
		}
	})

	t.Run("revocation takes effect", func(t *testing.T) {
		if err := db.Sessions().Revoke(ctx, session.ID); err != nil {
			t.Fatalf("Revoke: %v", err)
		}
		found, err := db.Sessions().ByTokenHash(ctx, auth.HashToken(token))
		if err != nil {
			t.Fatalf("ByTokenHash: %v", err)
		}
		if found.IsValid(time.Now()) {
			t.Error("session is still valid after revocation")
		}
	})
}

// A stolen refresh token must be usable at most once.
func TestSessionRotationInvalidatesTheOldToken(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	user := createUser(t, db, rbac.RoleUser)

	firstToken, firstHash, _ := auth.NewToken()
	if _, err := db.Sessions().Create(ctx, user.ID, firstHash, nil, "agent", true, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Create session: %v", err)
	}

	secondToken, secondHash, _ := auth.NewToken()
	rotated, err := db.Sessions().Rotate(ctx, auth.HashToken(firstToken), secondHash, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	if !rotated.MFASatisfied {
		t.Error("rotation lost the MFA flag; the user would be challenged again on every refresh")
	}

	t.Run("replaying the old token fails", func(t *testing.T) {
		thirdToken, thirdHash, _ := auth.NewToken()
		_ = thirdToken
		if _, err := db.Sessions().Rotate(ctx, auth.HashToken(firstToken), thirdHash, time.Now().Add(time.Hour)); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("replaying a rotated token = %v, want ErrNotFound", err)
		}
	})

	t.Run("the new token works", func(t *testing.T) {
		found, err := db.Sessions().ByTokenHash(ctx, auth.HashToken(secondToken))
		if err != nil {
			t.Fatalf("ByTokenHash: %v", err)
		}
		if !found.IsValid(time.Now()) {
			t.Error("the rotated session is not valid")
		}
	})
}

func TestRevokeAllSparesTheCurrentSession(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	user := createUser(t, db, rbac.RoleUser)

	var keep uuid.UUID
	for i := range 3 {
		_, hash, _ := auth.NewToken()
		s, err := db.Sessions().Create(ctx, user.ID, hash, nil, "agent", true, time.Now().Add(time.Hour))
		if err != nil {
			t.Fatalf("Create session %d: %v", i, err)
		}
		if i == 0 {
			keep = s.ID
		}
	}

	revoked, err := db.Sessions().RevokeAllForUser(ctx, user.ID, &keep)
	if err != nil {
		t.Fatalf("RevokeAllForUser: %v", err)
	}
	if revoked != 2 {
		t.Errorf("revoked %d sessions, want 2", revoked)
	}

	active, err := db.Sessions().ListActiveForUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("ListActiveForUser: %v", err)
	}
	if len(active) != 1 || active[0].ID != keep {
		t.Errorf("expected only the spared session to remain, got %d", len(active))
	}
}

func TestRecoveryCodesAreSingleUse(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	user := createUser(t, db, rbac.RoleAdmin)

	params := auth.Argon2Params{MemoryKiB: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}
	codes, hashes, err := auth.NewRecoveryCodes(params)
	if err != nil {
		t.Fatalf("NewRecoveryCodes: %v", err)
	}
	if err := db.RecoveryCodes().Replace(ctx, user.ID, hashes); err != nil {
		t.Fatalf("Replace: %v", err)
	}

	ids, stored, err := db.RecoveryCodes().UnusedHashes(ctx, user.ID)
	if err != nil {
		t.Fatalf("UnusedHashes: %v", err)
	}
	if len(ids) != auth.RecoveryCodeCount {
		t.Fatalf("got %d unused codes, want %d", len(ids), auth.RecoveryCodeCount)
	}

	index := auth.MatchRecoveryCode(codes[2], stored)
	if index < 0 {
		t.Fatal("a freshly generated code did not match its stored hash")
	}
	if err := db.RecoveryCodes().Consume(ctx, ids[index]); err != nil {
		t.Fatalf("Consume: %v", err)
	}
	if err := db.RecoveryCodes().Consume(ctx, ids[index]); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("consuming a used code = %v, want ErrNotFound", err)
	}

	remaining, err := db.RecoveryCodes().CountUnused(ctx, user.ID)
	if err != nil {
		t.Fatalf("CountUnused: %v", err)
	}
	if remaining != auth.RecoveryCodeCount-1 {
		t.Errorf("remaining = %d, want %d", remaining, auth.RecoveryCodeCount-1)
	}
}

func TestAuditAppendAndFilter(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	user := createUser(t, db, rbac.RoleAdmin)

	addr := netip.MustParseAddr("203.0.113.5")
	marker := uuid.NewString()

	for _, ev := range []store.AuditEvent{
		{ActorID: &user.ID, ActorEmail: user.Email, Action: "auth.login", Result: "success",
			IPAddress: &addr, RequestID: marker, Details: map[string]any{"method": "password"}},
		{ActorID: &user.ID, ActorEmail: user.Email, Action: "auth.login", Result: "denied",
			IPAddress: &addr, RequestID: marker},
		{ActorID: &user.ID, ActorEmail: user.Email, Action: "user.role.changed", Result: "success",
			RequestID: marker},
	} {
		if err := db.Audit().Append(ctx, ev); err != nil {
			t.Fatalf("Append %s: %v", ev.Action, err)
		}
	}

	all, err := db.Audit().List(ctx, store.AuditFilter{ActorID: &user.ID})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("got %d events, want 3", len(all))
	}
	if all[0].Action != "user.role.changed" {
		t.Errorf("events are not newest-first: %s", all[0].Action)
	}

	denied, err := db.Audit().List(ctx, store.AuditFilter{ActorID: &user.ID, Result: "denied"})
	if err != nil {
		t.Fatalf("List filtered: %v", err)
	}
	if len(denied) != 1 || denied[0].Action != "auth.login" {
		t.Errorf("result filter returned %d events", len(denied))
	}

	logins, err := db.Audit().List(ctx, store.AuditFilter{ActorID: &user.ID, Action: "auth.login"})
	if err != nil {
		t.Fatalf("List by action: %v", err)
	}
	if len(logins) != 2 {
		t.Errorf("action filter returned %d events, want 2", len(logins))
	}
	if logins[1].IPAddress == nil || logins[1].IPAddress.String() != "203.0.113.5" {
		t.Errorf("IP address round trip failed: %v", logins[1].IPAddress)
	}
}

func upperCase(s string) string {
	out := []rune(s)
	for i, r := range out {
		if r >= 'a' && r <= 'z' {
			out[i] = r - 32
		}
	}
	return string(out)
}
