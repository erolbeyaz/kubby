package httpapi

import (
	"net/http"
	"strings"
	"sync"

	"github.com/erolbeyaz/kubby/internal/audit"
	"github.com/erolbeyaz/kubby/internal/store"
)

// transcriptLimit caps what one session contributes to the audit stream. A session that
// cats a large file should not be able to push megabytes into the trail; the record says
// it was truncated rather than quietly holding half a session.
const transcriptLimit = 64 * 1024

// sessionRecorder keeps what was typed during an interactive session.
//
// This is a compliance requirement, not a feature (ADR-013 #5): a tool that hands out
// shells with cluster-wide credentials and keeps no record of what was typed cannot be
// audited, and the record is the thing that makes the feature defensible at all.
//
// Keystrokes only, never output: the transcript is evidence of what a person did, and
// container output can contain anything the container has, including secrets.
type sessionRecorder struct {
	handlers *resourceHandlers
	request  *http.Request
	cluster  *store.Cluster
	action   string
	subject  string

	mu        sync.Mutex
	typedText strings.Builder
	truncated bool
}

func (h *resourceHandlers) newSessionRecorder(r *http.Request, c *store.Cluster, action, subject string) *sessionRecorder {
	recorder := &sessionRecorder{handlers: h, request: r, cluster: c, action: action, subject: subject}
	recorder.record(action, audit.ResultSuccess, nil)
	return recorder
}

// typed appends keystrokes.
func (s *sessionRecorder) typed(text string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.typedText.Len()+len(text) > transcriptLimit {
		s.truncated = true
		return
	}
	s.typedText.WriteString(text)
}

// close writes the transcript once the session ends.
func (s *sessionRecorder) close(err error) {
	s.mu.Lock()
	transcript := s.typedText.String()
	truncated := s.truncated
	s.mu.Unlock()

	details := map[string]any{
		"transcript": transcript,
		"truncated":  truncated,
	}
	if err != nil {
		details["error"] = err.Error()
	}

	result := audit.ResultSuccess
	if err != nil {
		result = audit.ResultError
	}
	s.record(audit.ActionShellTranscript, result, details)
}

func (s *sessionRecorder) record(action, result string, details map[string]any) {
	if s.handlers.audit == nil {
		return
	}
	_, user := principal(s.request)

	s.handlers.audit.Record(s.request.Context(), audit.Event{
		Action:       action,
		Result:       result,
		ActorID:      &user.ID,
		ActorEmail:   user.Email,
		ClusterID:    &s.cluster.ID,
		ResourceKind: kindFor(s.action),
		ResourceName: s.subject,
		Details:      details,
	})
}

func kindFor(action string) string {
	if action == audit.ActionNodeShellOpened {
		return "Node"
	}
	return "Pod"
}
