package httpapi

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/erolbeyaz/kubby/internal/audit"
	"github.com/erolbeyaz/kubby/internal/cluster"
)

// The structured operations: each is a small, named change rather than a whole object
// sent back, so what happened is legible in the audit trail without diffing manifests.

type scaleBody struct {
	TypeKey   string `json:"typeKey"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Replicas  int32  `json:"replicas"`
}

func (h *resourceHandlers) scale(w http.ResponseWriter, r *http.Request) {
	var body scaleBody
	if !decodeJSON(w, r, &body) {
		return
	}

	resourceType, err := cluster.LookupType(body.TypeKey)
	if err != nil {
		writeError(w, r, http.StatusNotFound, "unknown resource type")
		return
	}
	ctx, ok := h.authoriseWrite(w, r, resourceType, body.Namespace, body.Name, cluster.VerbPatch)
	if !ok {
		return
	}

	req := cluster.WriteRequest{Type: resourceType, Namespace: body.Namespace, Name: body.Name, Verb: cluster.VerbPatch}
	if err := h.svc.Scale(r.Context(), ctx.cluster, req, body.Replicas, impersonationFor(r, ctx.cluster)); err != nil {
		h.recordWrite(r, ctx, audit.ActionObjectScaled, body.Namespace, body.Name, audit.ResultError, nil)
		writeResourceError(w, r, err)
		return
	}

	h.recordWrite(r, ctx, audit.ActionObjectScaled, body.Namespace, body.Name, audit.ResultSuccess, nil)
	writeJSON(w, http.StatusOK, map[string]any{"replicas": body.Replicas})
}

type targetBody struct {
	TypeKey   string `json:"typeKey"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

func (h *resourceHandlers) restart(w http.ResponseWriter, r *http.Request) {
	var body targetBody
	if !decodeJSON(w, r, &body) {
		return
	}

	resourceType, err := cluster.LookupType(body.TypeKey)
	if err != nil {
		writeError(w, r, http.StatusNotFound, "unknown resource type")
		return
	}
	ctx, ok := h.authoriseWrite(w, r, resourceType, body.Namespace, body.Name, cluster.VerbPatch)
	if !ok {
		return
	}

	req := cluster.WriteRequest{Type: resourceType, Namespace: body.Namespace, Name: body.Name, Verb: cluster.VerbPatch}
	if err := h.svc.Restart(r.Context(), ctx.cluster, req, time.Now(), impersonationFor(r, ctx.cluster)); err != nil {
		h.recordWrite(r, ctx, audit.ActionObjectRestarted, body.Namespace, body.Name, audit.ResultError, nil)
		writeResourceError(w, r, err)
		return
	}

	h.recordWrite(r, ctx, audit.ActionObjectRestarted, body.Namespace, body.Name, audit.ResultSuccess, nil)
	writeJSON(w, http.StatusOK, map[string]any{"restarted": true})
}

func (h *resourceHandlers) evict(w http.ResponseWriter, r *http.Request) {
	var body targetBody
	if !decodeJSON(w, r, &body) {
		return
	}

	podType, err := cluster.LookupType("pods")
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "pods are not registered")
		return
	}
	ctx, ok := h.authoriseWrite(w, r, podType, body.Namespace, body.Name, cluster.VerbDelete)
	if !ok {
		return
	}

	if err := h.svc.Evict(r.Context(), ctx.cluster, body.Namespace, body.Name, impersonationFor(r, ctx.cluster)); err != nil {
		h.recordWrite(r, ctx, audit.ActionPodEvicted, body.Namespace, body.Name, audit.ResultError, nil)
		// A budget that refuses is the system working, so it is a conflict rather than a
		// failure of the request.
		writeError(w, r, http.StatusConflict, err.Error())
		return
	}

	h.recordWrite(r, ctx, audit.ActionPodEvicted, body.Namespace, body.Name, audit.ResultSuccess, nil)
	writeJSON(w, http.StatusOK, map[string]any{"evicted": true})
}

type suspendBody struct {
	targetBody
	Suspended bool `json:"suspended"`
}

func (h *resourceHandlers) suspendCronJob(w http.ResponseWriter, r *http.Request) {
	var body suspendBody
	if !decodeJSON(w, r, &body) {
		return
	}

	cronType, err := cluster.LookupType("batch/cronjobs")
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "cronjobs are not registered")
		return
	}
	ctx, ok := h.authoriseWrite(w, r, cronType, body.Namespace, body.Name, cluster.VerbPatch)
	if !ok {
		return
	}

	req := cluster.WriteRequest{Type: cronType, Namespace: body.Namespace, Name: body.Name, Verb: cluster.VerbPatch}
	if err := h.svc.SetSuspended(r.Context(), ctx.cluster, req, body.Suspended, impersonationFor(r, ctx.cluster)); err != nil {
		h.recordWrite(r, ctx, audit.ActionCronJobSuspended, body.Namespace, body.Name, audit.ResultError, nil)
		writeResourceError(w, r, err)
		return
	}

	h.recordWrite(r, ctx, audit.ActionCronJobSuspended, body.Namespace, body.Name, audit.ResultSuccess, nil)
	writeJSON(w, http.StatusOK, map[string]any{"suspended": body.Suspended})
}

func (h *resourceHandlers) triggerCronJob(w http.ResponseWriter, r *http.Request) {
	var body targetBody
	if !decodeJSON(w, r, &body) {
		return
	}

	jobType, err := cluster.LookupType("batch/jobs")
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "jobs are not registered")
		return
	}
	// Triggering creates a Job, so that is the permission the cluster is asked for.
	ctx, ok := h.authoriseWrite(w, r, jobType, body.Namespace, body.Name, cluster.VerbCreate)
	if !ok {
		return
	}

	name, err := h.svc.TriggerCronJob(r.Context(), ctx.cluster, body.Namespace, body.Name, time.Now(), impersonationFor(r, ctx.cluster))
	if err != nil {
		h.recordWrite(r, ctx, audit.ActionCronJobTriggered, body.Namespace, body.Name, audit.ResultError, nil)
		writeResourceError(w, r, err)
		return
	}

	h.recordWrite(r, ctx, audit.ActionCronJobTriggered, body.Namespace, name, audit.ResultSuccess, nil)
	writeJSON(w, http.StatusOK, map[string]any{"job": name})
}

type cordonBody struct {
	Name          string `json:"name"`
	Unschedulable bool   `json:"unschedulable"`
}

func (h *resourceHandlers) cordonNode(w http.ResponseWriter, r *http.Request) {
	var body cordonBody
	if !decodeJSON(w, r, &body) {
		return
	}

	nodeType, err := cluster.LookupType("nodes")
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "nodes are not registered")
		return
	}
	ctx, ok := h.authoriseWrite(w, r, nodeType, "", body.Name, cluster.VerbPatch)
	if !ok {
		return
	}

	if err := h.svc.SetUnschedulable(r.Context(), ctx.cluster, body.Name, body.Unschedulable, impersonationFor(r, ctx.cluster)); err != nil {
		h.recordWrite(r, ctx, audit.ActionNodeCordoned, "", body.Name, audit.ResultError, nil)
		writeResourceError(w, r, err)
		return
	}

	h.recordWrite(r, ctx, audit.ActionNodeCordoned, "", body.Name, audit.ResultSuccess, nil)
	writeJSON(w, http.StatusOK, map[string]any{"unschedulable": body.Unschedulable})
}

// planDrain says what a drain would do, before it does any of it.
func (h *resourceHandlers) planDrain(w http.ResponseWriter, r *http.Request) {
	c, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}

	plan, err := h.svc.PlanDrain(r.Context(), c, chi.URLParam(r, "name"), impersonationFor(r, c))
	if err != nil {
		writeResourceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, plan)
}

func (h *resourceHandlers) drainNode(w http.ResponseWriter, r *http.Request) {
	var body cordonBody
	if !decodeJSON(w, r, &body) {
		return
	}

	nodeType, err := cluster.LookupType("nodes")
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "nodes are not registered")
		return
	}
	// Draining evicts pods, so that is the permission the cluster is asked for on top of
	// patching the node itself.
	ctx, ok := h.authoriseWrite(w, r, nodeType, "", body.Name, cluster.VerbPatch)
	if !ok {
		return
	}

	results, err := h.svc.Drain(r.Context(), ctx.cluster, body.Name, impersonationFor(r, ctx.cluster))
	if err != nil {
		h.recordWrite(r, ctx, audit.ActionNodeDrained, "", body.Name, audit.ResultError, nil)
		writeResourceError(w, r, err)
		return
	}

	h.recordWrite(r, ctx, audit.ActionNodeDrained, "", body.Name, audit.ResultSuccess, nil)
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

// ---------------------------------------------------------------- rollout

func (h *resourceHandlers) rolloutHistory(w http.ResponseWriter, r *http.Request) {
	c, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}

	namespace := chi.URLParam(r, "namespace")
	name := chi.URLParam(r, "name")

	revisions, err := h.svc.RolloutHistory(r.Context(), c, namespace, name, impersonationFor(r, c))
	if err != nil {
		writeResourceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"revisions": revisions})
}

type rollbackBody struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Revision  int64  `json:"revision"`
}

func (h *resourceHandlers) rollback(w http.ResponseWriter, r *http.Request) {
	var body rollbackBody
	if !decodeJSON(w, r, &body) {
		return
	}

	deployType, err := cluster.LookupType("apps/deployments")
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "deployments are not registered")
		return
	}
	ctx, ok := h.authoriseWrite(w, r, deployType, body.Namespace, body.Name, cluster.VerbPatch)
	if !ok {
		return
	}

	if err := h.svc.Rollback(r.Context(), ctx.cluster, body.Namespace, body.Name, body.Revision, impersonationFor(r, ctx.cluster)); err != nil {
		h.recordWrite(r, ctx, audit.ActionObjectRolledBack, body.Namespace, body.Name, audit.ResultError, nil)
		writeResourceError(w, r, err)
		return
	}

	h.recordWrite(r, ctx, audit.ActionObjectRolledBack, body.Namespace, body.Name, audit.ResultSuccess, nil)
	writeJSON(w, http.StatusOK, map[string]any{"revision": body.Revision})
}
