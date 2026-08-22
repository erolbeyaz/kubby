// Package logging provides the structured JSON logger used across Kubby.
//
// Two streams share this implementation: application logs and the audit stream
// (ADR-010). Every value passes through the redaction layer before reaching a sink.
package logging

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
)

// Stream identifies which of the two logical log streams a record belongs to.
type Stream string

const (
	StreamApplication Stream = "application"
	StreamAudit       Stream = "audit"
)

type contextKey struct{ name string }

var requestIDKey = contextKey{"request_id"}

// WithRequestID returns a context carrying the request correlation id.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

// RequestIDFrom returns the correlation id stored in ctx, or "" when absent.
func RequestIDFrom(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey).(string); ok {
		return id
	}
	return ""
}

// New builds the application logger. Output is one JSON object per line.
func New(level string, out io.Writer) *slog.Logger {
	if out == nil {
		out = os.Stdout
	}
	handler := slog.NewJSONHandler(out, &slog.HandlerOptions{
		Level:       parseLevel(level),
		ReplaceAttr: replaceAttr,
	})
	return slog.New(&redactingHandler{inner: handler}).With(
		slog.String("service", "kubby"),
		slog.String("stream", string(StreamApplication)),
	)
}

func parseLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// replaceAttr renames slog's defaults to the field names required by ADR-010 and
// forces RFC 3339 UTC timestamps (ADR-026).
func replaceAttr(_ []string, a slog.Attr) slog.Attr {
	switch a.Key {
	case slog.TimeKey:
		return slog.String("timestamp", a.Value.Time().UTC().Format(TimestampLayout))
	case slog.LevelKey:
		return slog.String("level", strings.ToLower(a.Value.String()))
	case slog.MessageKey:
		return slog.String("message", a.Value.String())
	}
	return a
}

// redactingHandler enforces redaction for every attribute that reaches a sink.
type redactingHandler struct {
	inner slog.Handler
}

func (h *redactingHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.inner.Enabled(ctx, l)
}

func (h *redactingHandler) Handle(ctx context.Context, r slog.Record) error {
	clean := slog.NewRecord(r.Time, r.Level, RedactString(r.Message), r.PC)

	if id := RequestIDFrom(ctx); id != "" {
		clean.AddAttrs(slog.String("request_id", id))
	}
	r.Attrs(func(a slog.Attr) bool {
		clean.AddAttrs(redactAttr(a))
		return true
	})
	return h.inner.Handle(ctx, clean)
}

func (h *redactingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	redacted := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		redacted[i] = redactAttr(a)
	}
	return &redactingHandler{inner: h.inner.WithAttrs(redacted)}
}

func (h *redactingHandler) WithGroup(name string) slog.Handler {
	return &redactingHandler{inner: h.inner.WithGroup(name)}
}

func redactAttr(a slog.Attr) slog.Attr {
	if a.Value.Kind() == slog.KindGroup {
		attrs := a.Value.Group()
		out := make([]any, 0, len(attrs))
		for _, inner := range attrs {
			out = append(out, redactAttr(inner))
		}
		return slog.Group(a.Key, out...)
	}
	if IsSensitiveKey(a.Key) {
		return slog.String(a.Key, Redacted)
	}
	if a.Value.Kind() == slog.KindString {
		return slog.String(a.Key, RedactString(a.Value.String()))
	}
	return a
}
