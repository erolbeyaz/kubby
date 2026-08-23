package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/erolbeyaz/kubby/internal/audit"
	"github.com/erolbeyaz/kubby/internal/auth"
	"github.com/erolbeyaz/kubby/internal/rbac"
	"github.com/erolbeyaz/kubby/internal/store"
)

type userHandlers struct {
	users     *store.UserRepo
	sessions  *store.SessionRepo
	auditLog  *audit.Emitter
	auditRepo *store.AuditRepo
	argon2    auth.Argon2Params
}

func (h *userHandlers) list(w http.ResponseWriter, r *http.Request) {
	users, err := h.users.List(r.Context())
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "could not list users")
		return
	}

	out := make([]*userResponse, 0, len(users))
	for _, u := range users {
		out = append(out, userResponseFrom(u))
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": out})
}

type createUserRequest struct {
	Email       string `json:"email"`
	DisplayName string `json:"displayName"`
	Password    string `json:"password"`
	Role        string `json:"role"`
}

func (h *userHandlers) create(w http.ResponseWriter, r *http.Request) {
	_, actor := principal(r)

	var req createUserRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	role, err := rbac.ParseRole(req.Role)
	if err != nil {
		writeError(w, r, http.StatusUnprocessableEntity, "role must be admin, user or readonly")
		return
	}
	if err := auth.ValidatePassword(req.Password, req.Email, req.DisplayName); err != nil {
		writeError(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}

	hash, err := auth.HashPassword(req.Password, h.argon2)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "could not create the user")
		return
	}

	created, err := h.users.Create(r.Context(), req.Email, req.DisplayName, hash, role)
	if errors.Is(err, store.ErrEmailInUse) {
		writeError(w, r, http.StatusConflict, "that email address is already registered")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "could not create the user")
		return
	}

	h.auditLog.Record(r.Context(), audit.Event{
		Action: audit.ActionUserCreated, Result: audit.ResultSuccess,
		ActorID: &actor.ID, ActorEmail: actor.Email, IPAddress: clientAddr(r),
		ResourceKind: "User", ResourceName: created.Email,
		Details: map[string]any{"role": string(role)},
	})
	writeJSON(w, http.StatusCreated, userResponseFrom(created))
}

type updateUserRequest struct {
	Role     *string `json:"role,omitempty"`
	IsActive *bool   `json:"isActive,omitempty"`
	// Unblock lifts a block caused by repeated failed sign-ins.
	Unblock *bool `json:"unblock,omitempty"`
}

// update changes a role or activation state.
//
// Two guards protect the installation from locking itself out: an administrator cannot
// demote or deactivate themselves, and the last active administrator cannot be removed.
func (h *userHandlers) update(w http.ResponseWriter, r *http.Request) {
	_, actor := principal(r)

	targetID, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}

	var req updateUserRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	target, err := h.users.ByID(r.Context(), targetID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, r, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "could not load the user")
		return
	}

	if target.ID == actor.ID && (req.Role != nil || req.IsActive != nil) {
		writeError(w, r, http.StatusConflict, "you cannot change your own role or activation state")
		return
	}

	if req.Role != nil {
		role, parseErr := rbac.ParseRole(*req.Role)
		if parseErr != nil {
			writeError(w, r, http.StatusUnprocessableEntity, "role must be admin, user or readonly")
			return
		}
		if demotesLastAdmin(r, h, target, role) {
			writeError(w, r, http.StatusConflict, "the last active administrator cannot be demoted")
			return
		}
		if err := h.users.UpdateRole(r.Context(), target.ID, role); err != nil {
			writeError(w, r, http.StatusInternalServerError, "could not change the role")
			return
		}
		h.auditLog.Record(r.Context(), audit.Event{
			Action: audit.ActionUserRoleChanged, Result: audit.ResultSuccess,
			ActorID: &actor.ID, ActorEmail: actor.Email, IPAddress: clientAddr(r),
			ResourceKind: "User", ResourceName: target.Email,
			Details: map[string]any{"from": string(target.Role), "to": string(role)},
		})
	}

	if req.IsActive != nil {
		if !*req.IsActive && isLastAdmin(r, h, target) {
			writeError(w, r, http.StatusConflict, "the last active administrator cannot be deactivated")
			return
		}
		if err := h.users.SetActive(r.Context(), target.ID, *req.IsActive); err != nil {
			writeError(w, r, http.StatusInternalServerError, "could not change the activation state")
			return
		}

		// A deactivated user must lose access immediately, not at token expiry.
		if !*req.IsActive {
			if _, err := h.sessions.RevokeAllForUser(r.Context(), target.ID, nil); err != nil {
				writeError(w, r, http.StatusInternalServerError, "user deactivated but sessions could not be revoked")
				return
			}
		}

		action := audit.ActionUserReactivated
		if !*req.IsActive {
			action = audit.ActionUserDeactivated
		}
		h.auditLog.Record(r.Context(), audit.Event{
			Action: action, Result: audit.ResultSuccess,
			ActorID: &actor.ID, ActorEmail: actor.Email, IPAddress: clientAddr(r),
			ResourceKind: "User", ResourceName: target.Email,
		})
	}

	if req.Unblock != nil && *req.Unblock {
		if err := h.users.Unblock(r.Context(), target.ID); err != nil {
			writeError(w, r, http.StatusInternalServerError, "could not unblock the user")
			return
		}
		h.auditLog.Record(r.Context(), audit.Event{
			Action: audit.ActionUserUnblocked, Result: audit.ResultSuccess,
			ActorID: &actor.ID, ActorEmail: actor.Email, IPAddress: clientAddr(r),
			ResourceKind: "User", ResourceName: target.Email,
		})
	}

	updated, err := h.users.ByID(r.Context(), target.ID)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "could not reload the user")
		return
	}
	writeJSON(w, http.StatusOK, userResponseFrom(updated))
}

func demotesLastAdmin(r *http.Request, h *userHandlers, target *store.User, newRole rbac.Role) bool {
	if target.Role != rbac.RoleAdmin || newRole == rbac.RoleAdmin {
		return false
	}
	return isLastAdmin(r, h, target)
}

func isLastAdmin(r *http.Request, h *userHandlers, target *store.User) bool {
	if target.Role != rbac.RoleAdmin {
		return false
	}
	others, err := h.users.CountActiveAdmins(r.Context(), target.ID)
	if err != nil {
		return true // fail closed
	}
	return others == 0
}

// ---------------------------------------------------------------- audit

func (h *userHandlers) listAudit(w http.ResponseWriter, r *http.Request) {
	filter := store.AuditFilter{
		Action: r.URL.Query().Get("action"),
		Result: r.URL.Query().Get("result"),
		Limit:  atoiOr(r.URL.Query().Get("limit"), 100),
		Offset: atoiOr(r.URL.Query().Get("offset"), 0),
	}
	if raw := r.URL.Query().Get("actorId"); raw != "" {
		if id, err := uuid.Parse(raw); err == nil {
			filter.ActorID = &id
		}
	}

	events, err := h.auditRepo.List(r.Context(), filter)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "could not list audit events")
		return
	}

	type auditItem struct {
		ID         int64  `json:"id"`
		OccurredAt string `json:"occurredAt"`
		ActorEmail string `json:"actorEmail"`
		Action     string `json:"action"`
		Result     string `json:"result"`
		// A resource name alone is ambiguous: the same secret name exists in many
		// namespaces and many clusters, so the record has to say which.
		ClusterID    string         `json:"clusterId,omitempty"`
		Namespace    string         `json:"namespace,omitempty"`
		ResourceKind string         `json:"resourceKind,omitempty"`
		ResourceName string         `json:"resourceName,omitempty"`
		IPAddress    string         `json:"ipAddress,omitempty"`
		RequestID    string         `json:"requestId,omitempty"`
		Details      map[string]any `json:"details,omitempty"`
	}

	out := make([]auditItem, 0, len(events))
	for _, e := range events {
		item := auditItem{
			ID:           e.ID,
			OccurredAt:   e.OccurredAt.UTC().Format(time.RFC3339),
			ActorEmail:   e.ActorEmail,
			Action:       e.Action,
			Result:       e.Result,
			Namespace:    e.Namespace,
			ResourceKind: e.ResourceKind,
			ResourceName: e.ResourceName,
			RequestID:    e.RequestID,
			Details:      e.Details,
		}
		if e.ClusterID != nil {
			item.ClusterID = e.ClusterID.String()
		}
		if e.IPAddress != nil {
			item.IPAddress = e.IPAddress.String()
		}
		out = append(out, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": out})
}

func parseUUIDParam(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid identifier")
		return uuid.Nil, false
	}
	return id, true
}

func atoiOr(s string, fallback int) int {
	if s == "" {
		return fallback
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return fallback
	}
	return n
}
