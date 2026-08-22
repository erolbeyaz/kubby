// Package audit records security-relevant actions.
//
// The audit stream cannot be disabled and is always written locally (ADR-010).
// Emitting must never block or fail a request: a failed audit write is logged loudly,
// but the user's action still completes or fails on its own merits.
package audit

import (
	"context"
	"log/slog"
	"net/netip"
	"time"

	"github.com/google/uuid"

	"github.com/erolbeyaz/kubby/internal/logging"
	"github.com/erolbeyaz/kubby/internal/store"
)

// Action names are hierarchical and dot-separated so they can be filtered by prefix.
const (
	ActionLoginSucceeded   = "auth.login.succeeded"
	ActionLoginFailed      = "auth.login.failed"
	ActionLoginLocked      = "auth.login.locked"
	ActionLoginBlocked     = "auth.login.blocked"
	ActionUserUnblocked    = "user.unblocked"
	ActionLogout           = "auth.logout"
	ActionTokenRefreshed   = "auth.token.refreshed"
	ActionTokenReuse       = "auth.token.reuse_detected"
	ActionMFAEnrolled      = "auth.mfa.enrolled"
	ActionMFAVerified      = "auth.mfa.verified"
	ActionMFAFailed        = "auth.mfa.failed"
	ActionRecoveryCodeUsed = "auth.recovery_code.used"
	ActionPasswordChanged  = "auth.password.changed"
	ActionSetupCompleted   = "setup.completed"
	ActionUserCreated      = "user.created"
	ActionUserRoleChanged  = "user.role.changed"
	ActionUserDeactivated  = "user.deactivated"
	ActionUserReactivated  = "user.reactivated"
	ActionSessionRevoked   = "session.revoked"
	ActionSessionsRevoked  = "session.revoked_all"
)

const (
	ResultSuccess = "success"
	ResultDenied  = "denied"
	ResultError   = "error"
)

// Event describes one auditable action.
type Event struct {
	Action       string
	Result       string
	ActorID      *uuid.UUID
	ActorEmail   string
	ClusterID    *uuid.UUID
	Namespace    string
	ResourceKind string
	ResourceName string
	IPAddress    *netip.Addr
	Details      map[string]any
}

// Recorder persists audit events.
type Recorder interface {
	Append(ctx context.Context, e store.AuditEvent) error
}

// Logger writes audit events to the structured log stream as well, so they reach the
// configured sinks (Faz 9) even before a SIEM shipper is wired up.
type Emitter struct {
	recorder Recorder
	logger   *slog.Logger
}

func New(recorder Recorder, logger *slog.Logger) *Emitter {
	return &Emitter{
		recorder: recorder,
		logger:   logger.With(slog.String("stream", string(logging.StreamAudit))),
	}
}

// Record writes the event to the database and the audit log stream.
//
// The database write uses a detached context with its own timeout: an audit entry for a
// cancelled request is exactly the entry most worth keeping.
func (e *Emitter) Record(ctx context.Context, ev Event) {
	requestID := logging.RequestIDFrom(ctx)

	attrs := []slog.Attr{
		slog.String("action", ev.Action),
		slog.String("result", ev.Result),
		slog.String("user", ev.ActorEmail),
		slog.String("request_id", requestID),
	}
	if ev.ClusterID != nil {
		attrs = append(attrs, slog.String("cluster", ev.ClusterID.String()))
	}
	if ev.Namespace != "" {
		attrs = append(attrs, slog.String("namespace", ev.Namespace))
	}
	if ev.ResourceKind != "" {
		attrs = append(attrs, slog.String("resource_kind", ev.ResourceKind))
	}
	if ev.ResourceName != "" {
		attrs = append(attrs, slog.String("resource_name", ev.ResourceName))
	}
	if ev.IPAddress != nil {
		attrs = append(attrs, slog.String("client_ip", ev.IPAddress.String()))
	}
	e.logger.LogAttrs(ctx, slog.LevelInfo, "audit", attrs...)

	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	err := e.recorder.Append(writeCtx, store.AuditEvent{
		ActorID:      ev.ActorID,
		ActorEmail:   ev.ActorEmail,
		Action:       ev.Action,
		Result:       ev.Result,
		ClusterID:    ev.ClusterID,
		Namespace:    ev.Namespace,
		ResourceKind: ev.ResourceKind,
		ResourceName: ev.ResourceName,
		IPAddress:    ev.IPAddress,
		RequestID:    requestID,
		Details:      ev.Details,
	})
	if err != nil {
		// Losing an audit record is a security event in itself, so it is logged at
		// error level with everything needed to reconstruct it by hand.
		e.logger.LogAttrs(ctx, slog.LevelError, "audit write failed",
			append(attrs, slog.String("error", err.Error()))...)
	}
}
