package httpapi

import (
	"context"
	"net/http"

	"github.com/erolbeyaz/kubby/internal/audit"
	"github.com/erolbeyaz/kubby/internal/settings"
)

// settingsHandlers serve the deployment-wide options. Every route here sits behind
// PermSettingsWrite, which only an admin holds.
type settingsHandlers struct {
	svc   *settings.Service
	audit *audit.Emitter
	// shipper is reconfigured the moment the setting is saved, so an admin does not have
	// to restart the process to change where the audit trail is copied.
	shipper *audit.ShipperManager
}

func (h *settingsHandlers) read(w http.ResponseWriter, r *http.Request) {
	all, err := h.svc.All(r.Context())
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "could not read the settings")
		return
	}
	writeJSON(w, http.StatusOK, all)
}

func (h *settingsHandlers) saveNodeShell(w http.ResponseWriter, r *http.Request) {
	var body settings.NodeShell
	if !decodeJSON(w, r, &body) {
		return
	}

	_, user := principal(r)
	if err := h.svc.SaveNodeShell(r.Context(), body, user.ID); err != nil {
		writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	h.record(r, audit.ActionSettingsChanged, "node_shell")
	h.read(w, r)
}

// applyShipping installs the stored configuration in the running shipper.
func (h *settingsHandlers) applyShipping(ctx context.Context) error {
	if h.shipper == nil {
		return nil
	}
	enabled, cfg, err := h.svc.AuditSinkConfig(ctx)
	if err != nil {
		return err
	}
	return h.shipper.Apply(ctx, enabled, cfg)
}

func (h *settingsHandlers) savePodDebug(w http.ResponseWriter, r *http.Request) {
	var body settings.PodDebug
	if !decodeJSON(w, r, &body) {
		return
	}

	_, user := principal(r)
	if err := h.svc.SavePodDebug(r.Context(), body, user.ID); err != nil {
		writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	h.record(r, audit.ActionSettingsChanged, "pod_debug")
	h.read(w, r)
}

type metricsBody struct {
	settings.Metrics
	Password string `json:"password,omitempty"`
	// ClearPassword distinguishes "I did not retype it" from "remove it". Without the
	// difference, a form that is saved without touching the field loses the credential.
	ClearPassword bool `json:"clearPassword,omitempty"`
}

func (h *settingsHandlers) saveMetrics(w http.ResponseWriter, r *http.Request) {
	var body metricsBody
	if !decodeJSON(w, r, &body) {
		return
	}

	_, user := principal(r)
	if err := h.svc.SaveMetrics(r.Context(), body.Metrics, body.Password, body.ClearPassword, user.ID); err != nil {
		writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	h.record(r, audit.ActionSettingsChanged, "metrics")
	h.read(w, r)
}

type auditSinkBody struct {
	settings.AuditSink
	Token      string `json:"token,omitempty"`
	ClearToken bool   `json:"clearToken,omitempty"`
}

func (h *settingsHandlers) saveAuditSink(w http.ResponseWriter, r *http.Request) {
	var body auditSinkBody
	if !decodeJSON(w, r, &body) {
		return
	}

	_, user := principal(r)
	if err := h.svc.SaveAuditSink(r.Context(), body.AuditSink, body.Token, body.ClearToken, user.ID); err != nil {
		writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	// Changing where the audit trail is copied to is itself an auditable act, and one of
	// the more interesting ones.
	h.record(r, audit.ActionSettingsChanged, "audit_sink")

	if err := h.applyShipping(r.Context()); err != nil {
		// Saved but not running: said plainly rather than left for someone to discover
		// when they go looking for events that never arrived.
		writeError(w, r, http.StatusBadRequest,
			"the setting was saved but shipping could not start: "+err.Error())
		return
	}
	h.read(w, r)
}

func (h *settingsHandlers) record(r *http.Request, action, group string) {
	if h.audit == nil {
		return
	}
	_, user := principal(r)

	h.audit.Record(r.Context(), audit.Event{
		Action:     action,
		Result:     audit.ResultSuccess,
		ActorID:    &user.ID,
		ActorEmail: user.Email,
		// The group, never the values: a settings change is worth recording, and the
		// credential inside it is exactly what must not be.
		Details: map[string]any{"group": group},
	})
}
