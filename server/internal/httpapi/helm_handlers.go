package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// listHelmReleases answers what charts are installed.
//
// Read through the Kubernetes API like everything else, not by running the helm binary:
// shelling out would mean a second credential path and an answer that depends on which
// helm happens to be on PATH.
func (h *resourceHandlers) listHelmReleases(w http.ResponseWriter, r *http.Request) {
	c, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}

	releases, err := h.svc.ListHelmReleases(r.Context(), c,
		r.URL.Query().Get("namespace"), impersonationFor(r, c))
	if err != nil {
		writeResourceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"releases": releases})
}

// helmRelease answers what one release was installed with, which is the question people
// actually open a release to ask.
func (h *resourceHandlers) helmRelease(w http.ResponseWriter, r *http.Request) {
	c, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}

	detail, err := h.svc.HelmReleaseDetails(r.Context(), c,
		chi.URLParam(r, "namespace"), chi.URLParam(r, "name"), impersonationFor(r, c))
	if err != nil {
		writeResourceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}
