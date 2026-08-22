package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/erolbeyaz/kubby/internal/audit"
	"github.com/erolbeyaz/kubby/internal/auth"
	"github.com/erolbeyaz/kubby/internal/logging"
	"github.com/erolbeyaz/kubby/internal/rbac"
	"github.com/erolbeyaz/kubby/internal/store"
)

// maxBodyBytes caps request bodies. Auth payloads are tiny; anything larger is abuse.
const maxBodyBytes = 64 * 1024

type authHandlers struct {
	svc        *auth.Service
	audit      *audit.Emitter
	users      *store.UserRepo
	sessions   *store.SessionRepo
	recovery   *store.RecoveryCodeRepo
	secure     bool
	refreshTTL time.Duration
	loginLimit *rateLimiter
}

// ---------------------------------------------------------------- setup wizard

type setupStatusResponse struct {
	SetupRequired bool `json:"setupRequired"`
}

func (h *authHandlers) setupStatus(w http.ResponseWriter, r *http.Request) {
	required, err := h.svc.SetupRequired(r.Context())
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "could not determine setup state")
		return
	}
	writeJSON(w, http.StatusOK, setupStatusResponse{SetupRequired: required})
}

type createAdminRequest struct {
	Email       string `json:"email"`
	DisplayName string `json:"displayName"`
	Password    string `json:"password"`
}

// createFirstAdmin is the only path that creates an account without an existing
// administrator. It closes permanently once any user exists.
func (h *authHandlers) createFirstAdmin(w http.ResponseWriter, r *http.Request) {
	var req createAdminRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	user, err := h.svc.CreateFirstAdmin(r.Context(), req.Email, req.DisplayName, req.Password)
	switch {
	case errors.Is(err, auth.ErrSetupComplete):
		writeError(w, r, http.StatusGone, "setup has already been completed")
		return
	case errors.Is(err, auth.ErrWeakPassword):
		writeError(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	case err != nil:
		writeError(w, r, http.StatusInternalServerError, "could not create the administrator account")
		return
	}

	h.audit.Record(r.Context(), audit.Event{
		Action: audit.ActionSetupCompleted, Result: audit.ResultSuccess,
		ActorID: &user.ID, ActorEmail: user.Email, IPAddress: clientAddr(r),
	})
	writeJSON(w, http.StatusCreated, userResponseFrom(user))
}

// ---------------------------------------------------------------- login

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginResponse struct {
	MFARequired bool `json:"mfaRequired"`
	MFAEnrolled bool `json:"mfaEnrolled"`
	// MFAEnrolmentRequired tells the client to run enrolment rather than ask for a
	// code: policy demands a second factor but the account has none yet.
	MFAEnrolmentRequired bool          `json:"mfaEnrolmentRequired,omitempty"`
	User                 *userResponse `json:"user,omitempty"`
}

func (h *authHandlers) login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	ip := clientAddr(r)
	result, err := h.svc.Authenticate(r.Context(), req.Email, req.Password, ip, r.UserAgent())
	if err != nil {
		h.recordFailedLogin(r, req.Email, err)
		h.writeLoginFailure(w, r, err)
		return
	}

	// A legitimate user should not stay throttled by their own earlier typos.
	h.loginLimit.reset(clientKey(r))

	h.issueSession(w, result.Token)

	if result.MFARequired {
		h.audit.Record(r.Context(), audit.Event{
			Action: audit.ActionLoginSucceeded, Result: audit.ResultSuccess,
			ActorID: &result.User.ID, ActorEmail: result.User.Email, IPAddress: ip,
			Details: map[string]any{"stage": "password", "mfa_pending": true},
		})
		writeJSON(w, http.StatusOK, loginResponse{
			MFARequired:          true,
			MFAEnrolled:          result.User.HasMFA(),
			MFAEnrolmentRequired: !result.User.HasMFA(),
		})
		return
	}

	h.audit.Record(r.Context(), audit.Event{
		Action: audit.ActionLoginSucceeded, Result: audit.ResultSuccess,
		ActorID: &result.User.ID, ActorEmail: result.User.Email, IPAddress: ip,
	})
	writeJSON(w, http.StatusOK, loginResponse{User: userResponseFrom(result.User)})
}

// loginFailureResponse tells the sign-in screen exactly what to say.
type loginFailureResponse struct {
	Error             string `json:"error"`
	RequestID         string `json:"requestId,omitempty"`
	AttemptsRemaining int    `json:"attemptsRemaining,omitempty"`
	LockedForSeconds  int    `json:"lockedForSeconds,omitempty"`
	Blocked           bool   `json:"blocked,omitempty"`
}

// writeLoginFailure reports why a sign-in failed, including how many attempts remain.
//
// This does reveal whether an address is registered, which is a deliberate trade-off:
// on a small internal tool the usability gain outweighs account enumeration, and the
// lockout ladder limits what an attacker can do with the knowledge (ADR-035).
func (h *authHandlers) writeLoginFailure(w http.ResponseWriter, r *http.Request, err error) {
	body := loginFailureResponse{RequestID: logging.RequestIDFrom(r.Context())}
	status := http.StatusUnauthorized

	var failure *auth.FailedLogin
	if errors.As(err, &failure) {
		body.AttemptsRemaining = failure.AttemptsRemaining
		body.LockedForSeconds = int(failure.LockedFor.Round(time.Second).Seconds())
		body.Blocked = failure.Blocked
	}

	switch {
	case errors.Is(err, auth.ErrAccountBlocked):
		status = http.StatusForbidden
		body.Error = "account is blocked after repeated failed sign-ins; ask an administrator to unblock it"
	case errors.Is(err, auth.ErrAccountLocked):
		status = http.StatusTooManyRequests
		body.Error = "too many failed attempts; this account is temporarily locked"
	case errors.Is(err, auth.ErrAccountInactive):
		status = http.StatusForbidden
		body.Error = "account is deactivated"
	case errors.Is(err, auth.ErrInvalidCredentials):
		body.Error = "invalid email or password"
	default:
		status = http.StatusInternalServerError
		body.Error = "could not sign in"
	}

	writeJSON(w, status, body)
}

func (h *authHandlers) recordFailedLogin(r *http.Request, email string, cause error) {
	action := audit.ActionLoginFailed
	switch {
	case errors.Is(cause, auth.ErrAccountBlocked):
		action = audit.ActionLoginBlocked
	case errors.Is(cause, auth.ErrAccountLocked):
		action = audit.ActionLoginLocked
	}
	h.audit.Record(r.Context(), audit.Event{
		Action: action, Result: audit.ResultDenied,
		ActorEmail: email, IPAddress: clientAddr(r),
	})
}

// ---------------------------------------------------------------- MFA

type mfaRequest struct {
	Code         string `json:"code"`
	RecoveryCode string `json:"recoveryCode"`
}

func (h *authHandlers) verifyMFA(w http.ResponseWriter, r *http.Request) {
	session, user := principal(r)
	if session.MFASatisfied {
		writeJSON(w, http.StatusOK, loginResponse{User: userResponseFrom(user)})
		return
	}

	var req mfaRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	if !user.HasMFA() {
		writeError(w, r, http.StatusConflict, "no authenticator is enrolled; complete enrolment first")
		return
	}

	var err error
	action := audit.ActionMFAVerified
	if req.RecoveryCode != "" {
		action = audit.ActionRecoveryCodeUsed
		err = h.svc.CompleteMFAWithRecoveryCode(r.Context(), session, user, req.RecoveryCode)
	} else {
		err = h.svc.CompleteMFA(r.Context(), session, user, req.Code)
	}

	if err != nil {
		h.audit.Record(r.Context(), audit.Event{
			Action: audit.ActionMFAFailed, Result: audit.ResultDenied,
			ActorID: &user.ID, ActorEmail: user.Email, IPAddress: clientAddr(r),
		})
		writeError(w, r, http.StatusUnauthorized, "invalid verification code")
		return
	}

	h.audit.Record(r.Context(), audit.Event{
		Action: action, Result: audit.ResultSuccess,
		ActorID: &user.ID, ActorEmail: user.Email, IPAddress: clientAddr(r),
	})
	writeJSON(w, http.StatusOK, loginResponse{User: userResponseFrom(user)})
}

type enrollTOTPResponse struct {
	Secret    string `json:"secret"`
	URI       string `json:"uri"`
	QRCodePNG string `json:"qrCodePng"`
}

func (h *authHandlers) enrollTOTP(w http.ResponseWriter, r *http.Request) {
	_, user := principal(r)

	enrollment, err := h.svc.EnrollTOTP(r.Context(), user)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "could not start MFA enrolment")
		return
	}
	writeJSON(w, http.StatusOK, enrollTOTPResponse{
		Secret:    enrollment.Secret,
		URI:       enrollment.URI,
		QRCodePNG: enrollment.QRCodePNG,
	})
}

type confirmTOTPResponse struct {
	RecoveryCodes []string `json:"recoveryCodes"`
}

// confirmTOTP activates MFA and returns the recovery codes. They are shown once and
// never retrievable again.
func (h *authHandlers) confirmTOTP(w http.ResponseWriter, r *http.Request) {
	session, user := principal(r)

	var req mfaRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	codes, err := h.svc.ConfirmTOTP(r.Context(), session, user, req.Code)
	if err != nil {
		writeError(w, r, http.StatusUnauthorized, "invalid verification code")
		return
	}

	h.audit.Record(r.Context(), audit.Event{
		Action: audit.ActionMFAEnrolled, Result: audit.ResultSuccess,
		ActorID: &user.ID, ActorEmail: user.Email, IPAddress: clientAddr(r),
	})
	writeJSON(w, http.StatusOK, confirmTOTPResponse{RecoveryCodes: codes})
}

// ---------------------------------------------------------------- session lifecycle

func (h *authHandlers) refresh(w http.ResponseWriter, r *http.Request) {
	token := cookieValue(r, sessionCookieName)
	if token == "" {
		writeError(w, r, http.StatusUnauthorized, "authentication required")
		return
	}

	session, newToken, err := h.svc.Refresh(r.Context(), token)
	if err != nil {
		// A token that no longer rotates is either expired or already used. The latter
		// may be replay, so it is worth an audit entry either way.
		h.audit.Record(r.Context(), audit.Event{
			Action: audit.ActionTokenReuse, Result: audit.ResultDenied, IPAddress: clientAddr(r),
		})
		clearSessionCookie(w, h.secure)
		h.rotateCSRFCookie(w)
		writeError(w, r, http.StatusUnauthorized, "session expired, please sign in again")
		return
	}

	h.issueSession(w, newToken)
	h.audit.Record(r.Context(), audit.Event{
		Action: audit.ActionTokenRefreshed, Result: audit.ResultSuccess,
		ActorID: &session.UserID, IPAddress: clientAddr(r),
	})
	w.WriteHeader(http.StatusNoContent)
}

func (h *authHandlers) logout(w http.ResponseWriter, r *http.Request) {
	session, user := principal(r)

	if err := h.svc.Logout(r.Context(), session.ID); err != nil {
		writeError(w, r, http.StatusInternalServerError, "could not sign out")
		return
	}

	clearSessionCookie(w, h.secure)
	// The CSRF token is an anti-forgery value, not a session secret. Clearing it would
	// leave the next sign-in with nothing to double-submit, so it is rotated instead.
	h.rotateCSRFCookie(w)
	h.audit.Record(r.Context(), audit.Event{
		Action: audit.ActionLogout, Result: audit.ResultSuccess,
		ActorID: &user.ID, ActorEmail: user.Email, IPAddress: clientAddr(r),
	})
	w.WriteHeader(http.StatusNoContent)
}

// rotateCSRFCookie replaces the anti-forgery token without touching the session.
func (h *authHandlers) rotateCSRFCookie(w http.ResponseWriter) {
	if token, _, err := auth.NewToken(); err == nil {
		setCSRFCookie(w, token, h.secure, h.refreshTTL)
	}
}

// issueSession sets both the session cookie and a fresh CSRF token.
func (h *authHandlers) issueSession(w http.ResponseWriter, token string) {
	setSessionCookie(w, token, h.secure, h.refreshTTL)

	csrfToken, _, err := auth.NewToken()
	if err == nil {
		setCSRFCookie(w, csrfToken, h.secure, h.refreshTTL)
	}
}

// ---------------------------------------------------------------- account

type meResponse struct {
	User        *userResponse     `json:"user"`
	Permissions []rbac.Permission `json:"permissions"`
	MFAEnrolled bool              `json:"mfaEnrolled"`
	ReadOnly    bool              `json:"readOnly"`
}

func (h *authHandlers) me(readOnly bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, user := principal(r)
		writeJSON(w, http.StatusOK, meResponse{
			User:        userResponseFrom(user),
			Permissions: rbac.Role(user.Role).Permissions(),
			MFAEnrolled: user.HasMFA(),
			ReadOnly:    readOnly,
		})
	}
}

type changePasswordRequest struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

func (h *authHandlers) changePassword(w http.ResponseWriter, r *http.Request) {
	session, user := principal(r)

	var req changePasswordRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	err := h.svc.ChangePassword(r.Context(), user, req.CurrentPassword, req.NewPassword, session.ID)
	switch {
	case errors.Is(err, auth.ErrInvalidCredentials):
		writeError(w, r, http.StatusUnauthorized, "current password is incorrect")
		return
	case errors.Is(err, auth.ErrWeakPassword):
		writeError(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	case err != nil:
		writeError(w, r, http.StatusInternalServerError, "could not change the password")
		return
	}

	h.audit.Record(r.Context(), audit.Event{
		Action: audit.ActionPasswordChanged, Result: audit.ResultSuccess,
		ActorID: &user.ID, ActorEmail: user.Email, IPAddress: clientAddr(r),
	})
	w.WriteHeader(http.StatusNoContent)
}

type sessionResponse struct {
	ID        string `json:"id"`
	IPAddress string `json:"ipAddress,omitempty"`
	UserAgent string `json:"userAgent"`
	Current   bool   `json:"current"`
	CreatedAt string `json:"createdAt"`
	LastSeen  string `json:"lastSeenAt"`
	ExpiresAt string `json:"expiresAt"`
}

func (h *authHandlers) listSessions(w http.ResponseWriter, r *http.Request) {
	current, user := principal(r)

	sessions, err := h.sessions.ListActiveForUser(r.Context(), user.ID)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "could not list sessions")
		return
	}

	out := make([]sessionResponse, 0, len(sessions))
	for _, s := range sessions {
		item := sessionResponse{
			ID:        s.ID.String(),
			UserAgent: s.UserAgent,
			Current:   s.ID == current.ID,
			CreatedAt: s.CreatedAt.UTC().Format(time.RFC3339),
			LastSeen:  s.LastSeenAt.UTC().Format(time.RFC3339),
			ExpiresAt: s.ExpiresAt.UTC().Format(time.RFC3339),
		}
		if s.IPAddress != nil {
			item.IPAddress = s.IPAddress.String()
		}
		out = append(out, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": out})
}

// revokeOtherSessions is the "sign out everywhere else" action.
func (h *authHandlers) revokeOtherSessions(w http.ResponseWriter, r *http.Request) {
	current, user := principal(r)

	count, err := h.sessions.RevokeAllForUser(r.Context(), user.ID, &current.ID)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "could not revoke sessions")
		return
	}

	h.audit.Record(r.Context(), audit.Event{
		Action: audit.ActionSessionsRevoked, Result: audit.ResultSuccess,
		ActorID: &user.ID, ActorEmail: user.Email, IPAddress: clientAddr(r),
		Details: map[string]any{"revoked": count},
	})
	writeJSON(w, http.StatusOK, map[string]any{"revoked": count})
}

// ---------------------------------------------------------------- helpers

type userResponse struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	DisplayName string `json:"displayName"`
	Role        string `json:"role"`
	IsActive    bool   `json:"isActive"`
	IsBlocked   bool   `json:"isBlocked"`
	MFAEnrolled bool   `json:"mfaEnrolled"`
	CreatedAt   string `json:"createdAt"`
	LastLoginAt string `json:"lastLoginAt,omitempty"`
}

func userResponseFrom(u *store.User) *userResponse {
	out := &userResponse{
		ID:          u.ID.String(),
		Email:       u.Email,
		DisplayName: u.DisplayName,
		Role:        string(u.Role),
		IsActive:    u.IsActive,
		IsBlocked:   u.IsBlocked(),
		MFAEnrolled: u.HasMFA(),
		CreatedAt:   u.CreatedAt.UTC().Format(time.RFC3339),
	}
	if u.LastLoginAt != nil {
		out.LastLoginAt = u.LastLoginAt.UTC().Format(time.RFC3339)
	}
	return out
}

// decodeJSON reads a bounded, strict JSON body. Unknown fields are rejected so a typo
// in a client payload fails loudly instead of being silently ignored.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)

	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		writeError(w, r, http.StatusBadRequest, "request body is not valid JSON")
		return false
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		writeError(w, r, http.StatusBadRequest, "request body must contain a single JSON object")
		return false
	}
	return true
}
