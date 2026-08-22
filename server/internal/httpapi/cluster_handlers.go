package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/erolbeyaz/kubby/internal/audit"
	"github.com/erolbeyaz/kubby/internal/cluster"
	"github.com/erolbeyaz/kubby/internal/rbac"
	"github.com/erolbeyaz/kubby/internal/store"
)

// maxKubeconfigBody is larger than the general body cap: a kubeconfig with embedded
// certificates is bigger than any other payload Kubby accepts.
const maxKubeconfigBody = 512 * 1024

type clusterHandlers struct {
	svc      *cluster.Service
	clusters *store.ClusterRepo
	users    *store.UserRepo
	audit    *audit.Emitter
}

// ---------------------------------------------------------------- reading

type clusterResponse struct {
	ID                    string `json:"id"`
	Name                  string `json:"name"`
	Environment           string `json:"environment"`
	EnvironmentLabel      string `json:"environmentLabel"`
	DisplayEnvironment    string `json:"displayEnvironment"`
	Color                 string `json:"color"`
	AuthSource            string `json:"authSource"`
	APIServerURL          string `json:"apiServerUrl"`
	InsecureSkipTLSVerify bool   `json:"insecureSkipTlsVerify"`
	CredentialStatus      string `json:"credentialStatus"`
	StatusDetail          string `json:"statusDetail,omitempty"`
	K8sVersion            string `json:"k8sVersion,omitempty"`
	NodeCount             *int   `json:"nodeCount,omitempty"`
	MetricsAvailable      bool   `json:"metricsAvailable"`
	ReadOnly              bool   `json:"readOnly"`
	ImpersonationEnabled  bool   `json:"impersonationEnabled"`
	QPSLimit              int    `json:"qpsLimit"`
	LastValidatedAt       string `json:"lastValidatedAt,omitempty"`
	AccessLevel           string `json:"accessLevel,omitempty"`
}

func clusterResponseFrom(c *store.Cluster, accessLevel string) clusterResponse {
	out := clusterResponse{
		ID:                    c.ID.String(),
		Name:                  c.Name,
		Environment:           c.Environment,
		EnvironmentLabel:      c.EnvironmentLabel,
		DisplayEnvironment:    c.DisplayEnvironment(),
		Color:                 c.Color,
		AuthSource:            c.AuthSource,
		APIServerURL:          c.APIServerURL,
		InsecureSkipTLSVerify: c.InsecureSkipTLSVerify,
		CredentialStatus:      c.CredentialStatus,
		StatusDetail:          c.StatusDetail,
		K8sVersion:            c.K8sVersion,
		NodeCount:             c.NodeCount,
		MetricsAvailable:      c.MetricsAvailable,
		ReadOnly:              c.ReadOnly,
		ImpersonationEnabled:  c.ImpersonationEnabled,
		QPSLimit:              c.QPSLimit,
		AccessLevel:           accessLevel,
	}
	if c.LastValidatedAt != nil {
		out.LastValidatedAt = c.LastValidatedAt.UTC().Format(time.RFC3339)
	}
	return out
}

// list returns only what the caller may see: an administrator sees everything, everyone
// else sees exactly the clusters they were granted.
func (h *clusterHandlers) list(w http.ResponseWriter, r *http.Request) {
	_, user := principal(r)
	isAdmin := rbac.Role(user.Role).Can(rbac.PermClusterManage)

	var (
		clusters []*store.Cluster
		err      error
	)
	if isAdmin {
		clusters, err = h.clusters.List(r.Context())
	} else {
		clusters, err = h.clusters.ListForUser(r.Context(), user.ID)
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "could not list clusters")
		return
	}

	out := make([]clusterResponse, 0, len(clusters))
	for _, c := range clusters {
		level := store.AccessWrite
		if !isAdmin {
			level, _ = h.clusters.AccessLevel(r.Context(), user.ID, c.ID)
		}
		out = append(out, clusterResponseFrom(c, level))
	}
	writeJSON(w, http.StatusOK, map[string]any{"clusters": out})
}

func (h *clusterHandlers) get(w http.ResponseWriter, r *http.Request) {
	c, level, ok := h.resolve(w, r, store.AccessRead)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, clusterResponseFrom(c, level))
}

// ---------------------------------------------------------------- validation

type validateRequest struct {
	Kubeconfig  string `json:"kubeconfig"`
	ContextName string `json:"contextName"`
}

type contextResponse struct {
	Name                    string `json:"name"`
	ClusterName             string `json:"clusterName"`
	UserName                string `json:"userName"`
	Server                  string `json:"server"`
	Namespace               string `json:"namespace,omitempty"`
	AuthMethod              string `json:"authMethod"`
	InsecureSkipTLSVerify   bool   `json:"insecureSkipTlsVerify"`
	HasCertificateAuthority bool   `json:"hasCertificateAuthority"`
	Blocked                 bool   `json:"blocked"`
	Problem                 string `json:"problem,omitempty"`
}

type probeResponse struct {
	Status           string   `json:"status"`
	Detail           string   `json:"detail,omitempty"`
	K8sVersion       string   `json:"k8sVersion,omitempty"`
	NodeCount        *int     `json:"nodeCount,omitempty"`
	MetricsAvailable bool     `json:"metricsAvailable"`
	Permissions      []string `json:"permissions,omitempty"`
}

type validateResponse struct {
	Contexts       []contextResponse `json:"contexts"`
	CurrentContext string            `json:"currentContext"`
	Probe          *probeResponse    `json:"probe,omitempty"`
}

// validate previews a pasted kubeconfig without storing anything, so the user can see
// which contexts exist, whether the credential connects, and what it may do (ADR-018).
func (h *clusterHandlers) validate(w http.ResponseWriter, r *http.Request) {
	var req validateRequest
	if !decodeJSONLimit(w, r, &req, maxKubeconfigBody) {
		return
	}

	result, err := h.svc.Validate(r.Context(), []byte(req.Kubeconfig), req.ContextName)
	if err != nil {
		writeKubeconfigError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, validateResponseFrom(result))
}

func validateResponseFrom(result *cluster.ValidationResult) validateResponse {
	out := validateResponse{
		Contexts:       make([]contextResponse, 0, len(result.Contexts)),
		CurrentContext: result.CurrentContext,
	}
	for _, c := range result.Contexts {
		out.Contexts = append(out.Contexts, contextResponse{
			Name: c.Name, ClusterName: c.ClusterName, UserName: c.UserName,
			Server: c.Server, Namespace: c.Namespace, AuthMethod: c.AuthMethod,
			InsecureSkipTLSVerify:   c.InsecureSkipTLSVerify,
			HasCertificateAuthority: c.HasCertificateAuthority,
			Blocked:                 c.Blocked, Problem: c.Problem,
		})
	}
	if result.Probe != nil {
		out.Probe = &probeResponse{
			Status: result.Probe.Status, Detail: result.Probe.Detail,
			K8sVersion: result.Probe.K8sVersion, NodeCount: result.Probe.NodeCount,
			MetricsAvailable: result.Probe.MetricsAvailable, Permissions: result.Probe.Permissions,
		}
	}
	return out
}

// ---------------------------------------------------------------- writing

type createClusterRequest struct {
	Name             string  `json:"name"`
	Environment      string  `json:"environment"`
	EnvironmentLabel string  `json:"environmentLabel"`
	Color            string  `json:"color"`
	Kubeconfig       string  `json:"kubeconfig"`
	ContextName      string  `json:"contextName"`
	ProxyURL         *string `json:"proxyUrl"`
}

func (h *clusterHandlers) create(w http.ResponseWriter, r *http.Request) {
	_, actor := principal(r)

	var req createClusterRequest
	if !decodeJSONLimit(w, r, &req, maxKubeconfigBody) {
		return
	}
	if !validEnvironment(req.Environment) {
		writeError(w, r, http.StatusUnprocessableEntity, "environment must be prod, preprod, test or dr")
		return
	}

	created, err := h.svc.Create(r.Context(), cluster.CreateInput{
		Name:             req.Name,
		Environment:      req.Environment,
		EnvironmentLabel: req.EnvironmentLabel,
		Color:            req.Color,
		Kubeconfig:       []byte(req.Kubeconfig),
		ContextName:      req.ContextName,
		ProxyURL:         req.ProxyURL,
		CreatedBy:        actor.ID,
	})
	if errors.Is(err, store.ErrClusterNameInUse) {
		writeError(w, r, http.StatusConflict, "a cluster with that name already exists")
		return
	}
	if err != nil {
		writeKubeconfigError(w, r, err)
		return
	}

	h.audit.Record(r.Context(), audit.Event{
		Action: audit.ActionClusterCreated, Result: audit.ResultSuccess,
		ActorID: &actor.ID, ActorEmail: actor.Email, IPAddress: clientAddr(r),
		ClusterID: &created.ID, ResourceKind: "Cluster", ResourceName: created.Name,
		Details: map[string]any{"environment": created.Environment, "server": created.APIServerURL},
	})
	writeJSON(w, http.StatusCreated, clusterResponseFrom(created, store.AccessWrite))
}

type replaceCredentialRequest struct {
	Kubeconfig  string `json:"kubeconfig"`
	ContextName string `json:"contextName"`
}

// replaceCredential is the only way to change a stored kubeconfig. It is never sent
// back to the client for editing, so "update" means "paste a new one" (ADR-018).
func (h *clusterHandlers) replaceCredential(w http.ResponseWriter, r *http.Request) {
	_, actor := principal(r)

	c, _, ok := h.resolve(w, r, store.AccessWrite)
	if !ok {
		return
	}

	var req replaceCredentialRequest
	if !decodeJSONLimit(w, r, &req, maxKubeconfigBody) {
		return
	}

	if err := h.svc.ReplaceCredential(r.Context(), c.ID, []byte(req.Kubeconfig), req.ContextName); err != nil {
		writeKubeconfigError(w, r, err)
		return
	}

	h.audit.Record(r.Context(), audit.Event{
		Action: audit.ActionClusterCredentialUpdated, Result: audit.ResultSuccess,
		ActorID: &actor.ID, ActorEmail: actor.Email, IPAddress: clientAddr(r),
		ClusterID: &c.ID, ResourceKind: "Cluster", ResourceName: c.Name,
	})

	updated, err := h.clusters.ByID(r.Context(), c.ID)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "credential replaced but the cluster could not be reloaded")
		return
	}
	writeJSON(w, http.StatusOK, clusterResponseFrom(updated, store.AccessWrite))
}

type updateClusterRequest struct {
	Name                 *string `json:"name,omitempty"`
	Environment          *string `json:"environment,omitempty"`
	EnvironmentLabel     *string `json:"environmentLabel,omitempty"`
	Color                *string `json:"color,omitempty"`
	ReadOnly             *bool   `json:"readOnly,omitempty"`
	ImpersonationEnabled *bool   `json:"impersonationEnabled,omitempty"`
	QPSLimit             *int    `json:"qpsLimit,omitempty"`
}

func (h *clusterHandlers) update(w http.ResponseWriter, r *http.Request) {
	_, actor := principal(r)

	c, _, ok := h.resolve(w, r, store.AccessWrite)
	if !ok {
		return
	}

	var req updateClusterRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Environment != nil && !validEnvironment(*req.Environment) {
		writeError(w, r, http.StatusUnprocessableEntity, "environment must be prod, preprod, test or dr")
		return
	}

	err := h.clusters.UpdateSettings(r.Context(), c.ID, store.ClusterSettings{
		Name: req.Name, Environment: req.Environment, EnvironmentLabel: req.EnvironmentLabel,
		Color: req.Color, ReadOnly: req.ReadOnly,
		ImpersonationEnabled: req.ImpersonationEnabled, QPSLimit: req.QPSLimit,
	})
	if errors.Is(err, store.ErrClusterNameInUse) {
		writeError(w, r, http.StatusConflict, "a cluster with that name already exists")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "could not update the cluster")
		return
	}

	if req.ReadOnly != nil {
		action := audit.ActionClusterUnlocked
		if *req.ReadOnly {
			action = audit.ActionClusterLocked
		}
		h.audit.Record(r.Context(), audit.Event{
			Action: action, Result: audit.ResultSuccess,
			ActorID: &actor.ID, ActorEmail: actor.Email, IPAddress: clientAddr(r),
			ClusterID: &c.ID, ResourceKind: "Cluster", ResourceName: c.Name,
		})
	}

	updated, err := h.clusters.ByID(r.Context(), c.ID)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "could not reload the cluster")
		return
	}
	writeJSON(w, http.StatusOK, clusterResponseFrom(updated, store.AccessWrite))
}

func (h *clusterHandlers) remove(w http.ResponseWriter, r *http.Request) {
	_, actor := principal(r)

	c, _, ok := h.resolve(w, r, store.AccessWrite)
	if !ok {
		return
	}

	if err := h.clusters.Delete(r.Context(), c.ID); err != nil {
		writeError(w, r, http.StatusInternalServerError, "could not delete the cluster")
		return
	}

	h.audit.Record(r.Context(), audit.Event{
		Action: audit.ActionClusterDeleted, Result: audit.ResultSuccess,
		ActorID: &actor.ID, ActorEmail: actor.Email, IPAddress: clientAddr(r),
		ResourceKind: "Cluster", ResourceName: c.Name,
	})
	w.WriteHeader(http.StatusNoContent)
}

// test re-probes a cluster on demand, which is what the "check connection" action and
// the recovery flow after a credential update both use.
func (h *clusterHandlers) test(w http.ResponseWriter, r *http.Request) {
	c, level, ok := h.resolve(w, r, store.AccessRead)
	if !ok {
		return
	}

	refreshed, err := h.svc.Refresh(r.Context(), c)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "could not test the connection")
		return
	}
	writeJSON(w, http.StatusOK, clusterResponseFrom(refreshed, level))
}

// ---------------------------------------------------------------- grants

type grantResponse struct {
	UserID      string `json:"userId"`
	Email       string `json:"email"`
	DisplayName string `json:"displayName"`
	AccessLevel string `json:"accessLevel"`
}

func (h *clusterHandlers) listGrants(w http.ResponseWriter, r *http.Request) {
	c, _, ok := h.resolve(w, r, store.AccessWrite)
	if !ok {
		return
	}

	grants, err := h.clusters.ListGrants(r.Context(), c.ID)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "could not list cluster access")
		return
	}

	out := make([]grantResponse, 0, len(grants))
	for _, g := range grants {
		out = append(out, grantResponse{
			UserID: g.UserID.String(), Email: g.Email,
			DisplayName: g.DisplayName, AccessLevel: g.AccessLevel,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"grants": out})
}

type setGrantRequest struct {
	UserID      string `json:"userId"`
	AccessLevel string `json:"accessLevel"`
}

func (h *clusterHandlers) setGrant(w http.ResponseWriter, r *http.Request) {
	_, actor := principal(r)

	c, _, ok := h.resolve(w, r, store.AccessWrite)
	if !ok {
		return
	}

	var req setGrantRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	targetID, err := uuid.Parse(req.UserID)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid user identifier")
		return
	}

	if req.AccessLevel == "" {
		if err := h.clusters.RemoveGrant(r.Context(), targetID, c.ID); err != nil {
			writeError(w, r, http.StatusInternalServerError, "could not remove access")
			return
		}
	} else {
		if req.AccessLevel != store.AccessRead && req.AccessLevel != store.AccessWrite {
			writeError(w, r, http.StatusUnprocessableEntity, "access level must be read or write")
			return
		}
		if err := h.clusters.SetGrant(r.Context(), targetID, c.ID, actor.ID, req.AccessLevel); err != nil {
			writeError(w, r, http.StatusInternalServerError, "could not grant access")
			return
		}
	}

	h.audit.Record(r.Context(), audit.Event{
		Action: audit.ActionClusterAccessChanged, Result: audit.ResultSuccess,
		ActorID: &actor.ID, ActorEmail: actor.Email, IPAddress: clientAddr(r),
		ClusterID: &c.ID, ResourceKind: "Cluster", ResourceName: c.Name,
		Details: map[string]any{"user": req.UserID, "level": req.AccessLevel},
	})
	h.listGrants(w, r)
}

// ---------------------------------------------------------------- helpers

// resolve loads the cluster in the URL and enforces access in one place, so no handler
// can forget the check. Administrators pass implicitly; everyone else needs a grant.
func (h *clusterHandlers) resolve(w http.ResponseWriter, r *http.Request, needed string) (*store.Cluster, string, bool) {
	_, user := principal(r)

	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return nil, "", false
	}

	c, err := h.clusters.ByID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, r, http.StatusNotFound, "cluster not found")
		return nil, "", false
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "could not load the cluster")
		return nil, "", false
	}

	level := store.AccessWrite
	if !rbac.Role(user.Role).Can(rbac.PermClusterManage) {
		level, err = h.clusters.AccessLevel(r.Context(), user.ID, c.ID)
		if err != nil {
			writeError(w, r, http.StatusInternalServerError, "could not check cluster access")
			return nil, "", false
		}
		// An ungranted cluster reports as missing rather than forbidden: whether a
		// cluster exists is itself information the user has no claim to.
		if level == "" {
			writeError(w, r, http.StatusNotFound, "cluster not found")
			return nil, "", false
		}
		if needed == store.AccessWrite && level != store.AccessWrite {
			writeError(w, r, http.StatusForbidden, "you have read-only access to this cluster")
			return nil, "", false
		}
	}

	// The read-only lock is deliberately NOT checked here. It guards mutations sent to
	// the Kubernetes API — scale, delete, apply, drain, exec — which arrive in a later
	// phase. Administration of Kubby's own record of the cluster (settings, credential,
	// access, removal) stays available, or a lock would make a broken registration
	// unrepairable and undeletable (ADR-039).
	return c, level, true
}

func validEnvironment(env string) bool {
	switch env {
	case "prod", "preprod", "test", "dr":
		return true
	default:
		return false
	}
}

// writeKubeconfigError turns a validation failure into a status the UI can act on.
func writeKubeconfigError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, cluster.ErrExecPluginUsed):
		writeError(w, r, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, cluster.ErrBlockedAddress):
		writeError(w, r, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, cluster.ErrNoContexts),
		errors.Is(err, cluster.ErrUnknownContext),
		errors.Is(err, cluster.ErrInvalidKubeconfig):
		writeError(w, r, http.StatusUnprocessableEntity, err.Error())
	default:
		writeError(w, r, http.StatusInternalServerError, "could not process the kubeconfig")
	}
}
