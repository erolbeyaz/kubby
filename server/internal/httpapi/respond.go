package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"

	"github.com/erolbeyaz/kubby/internal/logging"
)

type errorResponse struct {
	Error     string `json:"error"`
	RequestID string `json:"requestId,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	// The response is already committed, so a late encoding failure cannot be reported
	// to the client; the access log records the status that was sent.
	_ = json.NewEncoder(w).Encode(body)
}

// writeError returns a generic message plus the request id. Detail belongs in the log,
// not in a response that may reach an unauthenticated caller.
func writeError(w http.ResponseWriter, r *http.Request, status int, message string) {
	writeJSON(w, status, errorResponse{
		Error:     message,
		RequestID: logging.RequestIDFrom(r.Context()),
	})
}

func randomID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "unknown"
	}
	return hex.EncodeToString(b)
}
