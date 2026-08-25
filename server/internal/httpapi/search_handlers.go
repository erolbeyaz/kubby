package httpapi

import (
	"net/http"
	"strings"

	"github.com/erolbeyaz/kubby/internal/cluster"
	"github.com/erolbeyaz/kubby/internal/rbac"
	"github.com/erolbeyaz/kubby/internal/store"
)

// search looks for an object by name across every cluster the reader may see.
//
// Scoped to their grants, not to the fleet: searching a cluster somebody was never given
// would tell them what is in it, which is the whole thing a grant withholds.
func (h *resourceHandlers) search(w http.ResponseWriter, r *http.Request) {
	_, user := principal(r)

	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(query) < minSearchLength {
		// Below two characters nearly everything matches, which costs every API server in
		// the fleet a list call to produce a screen of noise.
		writeJSON(w, http.StatusOK, cluster.SearchResult{})
		return
	}

	// The same rule the cluster list uses: an administrator sees the fleet, everyone else
	// sees what they were granted. Getting this wrong in either direction is serious —
	// too narrow and search silently finds nothing, too wide and it reports the contents
	// of a cluster somebody was deliberately not given.
	var (
		clusters []*store.Cluster
		err      error
	)
	if rbac.Role(user.Role).Can(rbac.PermClusterManage) {
		clusters, err = h.clusters.List(r.Context())
	} else {
		clusters, err = h.clusters.ListForUser(r.Context(), user.ID)
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "could not read the cluster list")
		return
	}

	result := h.svc.Search(r.Context(), cluster.SearchRequest{
		Query:    query,
		Clusters: clusters,
	}, func(c *store.Cluster) *cluster.ImpersonationConfig {
		return impersonationFor(r, c)
	})

	writeJSON(w, http.StatusOK, result)
}

const minSearchLength = 2
