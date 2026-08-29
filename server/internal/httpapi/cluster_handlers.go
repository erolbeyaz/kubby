package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/erolbeyaz/kubby/internal/audit"
	"github.com/erolbeyaz/kubby/internal/cluster"
	"github.com/erolbeyaz/kubby/internal/logsearch"
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
	logs     *cluster.LogSweeper
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
	// The metrics endpoint, so the reader can see and change where the history comes
	// from. Whether a password is stored is configuration; the password is not.
	MetricsURL                string `json:"metricsUrl,omitempty"`
	MetricsUsername           string `json:"metricsUsername,omitempty"`
	MetricsInsecureSkipVerify bool   `json:"metricsInsecureSkipVerify"`
	// The log source, on the same terms: the address and who it connects as are
	// configuration and are shown; the secret is not, only whether one is held.
	LogsURL                string `json:"logsUrl,omitempty"`
	LogsIndex              string `json:"logsIndex,omitempty"`
	LogsAuthScheme         string `json:"logsAuthScheme,omitempty"`
	LogsUsername           string `json:"logsUsername,omitempty"`
	LogsInsecureSkipVerify bool   `json:"logsInsecureSkipVerify"`
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

		MetricsURL:                c.MetricsURL,
		MetricsUsername:           c.MetricsUsername,
		MetricsInsecureSkipVerify: c.MetricsInsecureSkipVerify,

		LogsURL:                c.LogsURL,
		LogsIndex:              c.LogsIndex,
		LogsAuthScheme:         c.LogsAuthScheme,
		LogsUsername:           c.LogsUsername,
		LogsInsecureSkipVerify: c.LogsInsecureSkipVerify,
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

	// The cache was built with the credential that just went away; keeping it would
	// mean watching with something the cluster no longer accepts.
	h.svc.ReleaseCache(c.ID)

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
	// Where this cluster's history is read from. Per cluster because Prometheus is
	// normally deployed into the cluster it observes.
	MetricsURL                *string `json:"metricsUrl,omitempty"`
	MetricsUsername           *string `json:"metricsUsername,omitempty"`
	MetricsInsecureSkipVerify *bool   `json:"metricsInsecureSkipVerify,omitempty"`
	// MetricsPassword is write-only. Empty means "leave what is stored", not "remove it":
	// a form saved without retyping a credential must not lose it.
	MetricsPassword      string `json:"metricsPassword,omitempty"`
	ClearMetricsPassword bool   `json:"clearMetricsPassword,omitempty"`
	// Where this cluster's applications already ship their logs. Kubby reads them from
	// there and never asks the cluster for them.
	LogsURL                *string `json:"logsUrl,omitempty"`
	LogsIndex              *string `json:"logsIndex,omitempty"`
	LogsAuthScheme         *string `json:"logsAuthScheme,omitempty"`
	LogsUsername           *string `json:"logsUsername,omitempty"`
	LogsInsecureSkipVerify *bool   `json:"logsInsecureSkipVerify,omitempty"`
	// LogsSecret is write-only, on the same terms as the metrics password: empty means
	// "leave what is stored", not "remove it".
	LogsSecret      string `json:"logsSecret,omitempty"`
	ClearLogsSecret bool   `json:"clearLogsSecret,omitempty"`
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
		MetricsURL: req.MetricsURL, MetricsUsername: req.MetricsUsername,
		MetricsInsecureSkipVerify: req.MetricsInsecureSkipVerify,
		LogsURL:                   req.LogsURL,
		LogsIndex:                 req.LogsIndex,
		LogsAuthScheme:            req.LogsAuthScheme,
		LogsUsername:              req.LogsUsername,
		LogsInsecureSkipVerify:    req.LogsInsecureSkipVerify,
	})
	if errors.Is(err, store.ErrClusterNameInUse) {
		writeError(w, r, http.StatusConflict, "a cluster with that name already exists")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "could not update the cluster")
		return
	}

	// An operator who has just typed an address, or cleared one to go back to whatever
	// Kubby can find, is asking for it to take effect now. Half an hour of a remembered
	// search would read as the change not having worked.
	if req.MetricsURL != nil {
		h.svc.ForgetDiscoveredMetrics(c.ID.String())
	}

	if req.MetricsPassword != "" || req.ClearMetricsPassword {
		var sealed []byte
		if !req.ClearMetricsPassword {
			sealed, err = h.svc.SealMetricsPassword(c.ID.String(), req.MetricsPassword)
			if err != nil {
				writeError(w, r, http.StatusInternalServerError, "could not store the metrics password")
				return
			}
		}
		if err := h.clusters.SetMetricsPassword(r.Context(), c.ID, sealed); err != nil {
			writeError(w, r, http.StatusInternalServerError, "could not store the metrics password")
			return
		}
	}

	// An operator who has just pointed a cluster at a different store is asking for it
	// to take effect now; a minute of answers from the old one would read as the change
	// not having worked.
	if h.logs != nil && (req.LogsURL != nil || req.LogsIndex != nil || req.LogsAuthScheme != nil ||
		req.LogsUsername != nil || req.LogsSecret != "" || req.ClearLogsSecret) {
		h.logs.Forget(c.ID.String())
	}

	if req.LogsSecret != "" || req.ClearLogsSecret {
		var sealed []byte
		if !req.ClearLogsSecret {
			sealed, err = h.svc.SealLogsSecret(c.ID.String(), req.LogsSecret)
			if err != nil {
				writeError(w, r, http.StatusInternalServerError, "could not store the log source secret")
				return
			}
		}
		if err := h.clusters.SetLogsSecret(r.Context(), c.ID, sealed); err != nil {
			writeError(w, r, http.StatusInternalServerError, "could not store the log source secret")
			return
		}
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
	h.svc.ReleaseCache(c.ID)

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

type probeLogsRequest struct {
	// The values as typed, which may not be saved yet: a test that only ever checked
	// what is stored would mean saving a wrong address to find out it is wrong.
	URL                string `json:"logsUrl,omitempty"`
	Index              string `json:"logsIndex,omitempty"`
	AuthScheme         string `json:"logsAuthScheme,omitempty"`
	Username           string `json:"logsUsername,omitempty"`
	Secret             string `json:"logsSecret,omitempty"`
	InsecureSkipVerify bool   `json:"logsInsecureSkipVerify,omitempty"`
}

type probeLogsResponse struct {
	Reachable bool `json:"reachable"`
	// Detail says what went wrong in the operator's terms — a refused credential and a
	// pattern matching nothing are different problems with the same red tick.
	Detail string           `json:"detail,omitempty"`
	Probe  *logsearch.Probe `json:"probe,omitempty"`
}

// probeLogs tests a cluster's log source.
//
// A failure here is an answer, not a server error: "the credential was refused" is the
// result the operator asked for. Only the request being unusable is a 4xx.
func (h *clusterHandlers) probeLogs(w http.ResponseWriter, r *http.Request) {
	c, _, ok := h.resolve(w, r, store.AccessWrite)
	if !ok {
		return
	}

	var req probeLogsRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	probe, err := h.svc.ProbeLogs(ctx, c, logsearch.Config{
		URL: req.URL, Index: req.Index, Username: req.Username,
		Secret: req.Secret, Scheme: req.AuthScheme,
		InsecureSkipVerify: req.InsecureSkipVerify,
	}, 15*time.Minute)
	if err != nil {
		writeJSON(w, http.StatusOK, probeLogsResponse{Reachable: false, Detail: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, probeLogsResponse{Reachable: true, Probe: probe})
}

type logFindingsResponse struct {
	State  string `json:"state"`
	Detail string `json:"detail,omitempty"`
	// SweptAt is when the answer was taken, so a reader can tell a quiet cluster from a
	// sweeper that stopped running.
	SweptAt  string              `json:"sweptAt,omitempty"`
	Findings []logsearch.Finding `json:"findings"`
}

// logFindings reports what this cluster's own logs are saying.
//
// Scoped by the cluster grant and nothing finer, which is correct rather than a
// shortcut: Kubby's grants are (user, cluster), so a reader who reaches this endpoint
// can already list every pod in the cluster and open any of their logs. A namespace
// filter here would narrow the summary of what it already lets them read in full.
func (h *clusterHandlers) logFindings(w http.ResponseWriter, r *http.Request) {
	c, _, ok := h.resolve(w, r, store.AccessRead)
	if !ok {
		return
	}
	if h.logs == nil {
		writeJSON(w, http.StatusOK, logFindingsResponse{
			State:    cluster.LogsStateUnknown,
			Detail:   "log sweeping is not running",
			Findings: []logsearch.Finding{},
		})
		return
	}

	found := h.logs.Findings(c.ID.String())
	findings := found.Findings
	if findings == nil {
		// An empty array rather than null: the client tells "nothing is wrong" from
		// "nothing was looked at" by the state, not by the shape of this field.
		findings = []logsearch.Finding{}
	}

	writeJSON(w, http.StatusOK, logFindingsResponse{
		State:    found.State,
		Detail:   found.Detail,
		SweptAt:  rfc3339(found.SweptAt),
		Findings: findings,
	})
}

func rfc3339(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
