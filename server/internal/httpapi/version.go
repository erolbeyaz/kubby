package httpapi

import (
	"net/http"
	"runtime"
)

// Build metadata injected at link time via -ldflags. Defaults mark a non-release build.
//
// Compliance requirement: a running deployment must be able to answer "which build is
// this" without access to the build pipeline.
var (
	Version   = "dev"
	CommitSHA = "unknown"
	BuildDate = "unknown"
)

type versionResponse struct {
	Version   string `json:"version"`
	CommitSHA string `json:"commitSha"`
	BuildDate string `json:"buildDate"`
	GoVersion string `json:"goVersion"`
}

func handleVersion() http.HandlerFunc {
	body := versionResponse{
		Version:   Version,
		CommitSHA: CommitSHA,
		BuildDate: BuildDate,
		GoVersion: runtime.Version(),
	}
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, body)
	}
}
