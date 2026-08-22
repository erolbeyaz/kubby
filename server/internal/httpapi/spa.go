package httpapi

import (
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"strings"

	"github.com/go-chi/chi/v5"
)

// mountSPA serves the embedded frontend. Unknown paths fall back to index.html so
// client-side routes survive a page reload, but API paths never do.
func mountSPA(r chi.Router, webFS fs.FS, logger *slog.Logger) {
	if webFS == nil {
		r.NotFound(func(w http.ResponseWriter, req *http.Request) {
			writeError(w, req, http.StatusNotFound, "frontend is not embedded in this build")
		})
		return
	}

	fileServer := http.FileServer(http.FS(webFS))

	r.NotFound(func(w http.ResponseWriter, req *http.Request) {
		if strings.HasPrefix(req.URL.Path, "/api/") {
			writeError(w, req, http.StatusNotFound, "endpoint not found")
			return
		}

		clean := path.Clean(strings.TrimPrefix(req.URL.Path, "/"))
		if clean == "." || clean == "/" {
			serveIndex(w, req, webFS, logger)
			return
		}
		if _, err := fs.Stat(webFS, clean); err != nil {
			serveIndex(w, req, webFS, logger)
			return
		}
		fileServer.ServeHTTP(w, req)
	})
}

func serveIndex(w http.ResponseWriter, r *http.Request, webFS fs.FS, logger *slog.Logger) {
	data, err := fs.ReadFile(webFS, "index.html")
	if err != nil {
		logger.ErrorContext(r.Context(), "read embedded index.html", slog.String("error", err.Error()))
		writeError(w, r, http.StatusInternalServerError, "frontend assets are unavailable")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
