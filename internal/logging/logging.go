// Package logging configures log/slog for the app and carries per-request
// details in the context, so that every line logged while serving a request
// says which request and which user it belongs to.
//
// Never log passwords, session or CSRF tokens, cookies, request bodies, or
// students' homework statuses and grades.
package logging

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync/atomic"
)

// ParseLevel reads a level name: debug, info, warn or error.
func ParseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info", "":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	}
	return 0, fmt.Errorf("unknown log level %q (want debug, info, warn or error)", s)
}

// New returns a logger writing to w in format "text" or "json". At debug
// level each line also says which source line logged it.
func New(w io.Writer, level slog.Level, format string) (*slog.Logger, error) {
	opts := &slog.HandlerOptions{Level: level, AddSource: level <= slog.LevelDebug}
	var h slog.Handler
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "text", "":
		h = slog.NewTextHandler(w, opts)
	case "json":
		h = slog.NewJSONHandler(w, opts)
	default:
		return nil, fmt.Errorf("unknown log format %q (want text or json)", format)
	}
	return slog.New(&contextHandler{h}), nil
}

// request is what the context carries about the request being served.
type request struct {
	id string
	// user is set once the session is loaded, after the request started.
	user atomic.Pointer[string]
}

type requestKey struct{}

// WithRequest returns a context for a request with the given ID.
func WithRequest(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestKey{}, &request{id: id})
}

func requestFrom(ctx context.Context) *request {
	r, _ := ctx.Value(requestKey{}).(*request)
	return r
}

// RequestID returns the ID of the request ctx belongs to, or "".
func RequestID(ctx context.Context) string {
	if r := requestFrom(ctx); r != nil {
		return r.id
	}
	return ""
}

// SetUser records the signed-in user of the request ctx belongs to; lines
// logged afterwards with ctx (or a context derived from it) carry it.
func SetUser(ctx context.Context, username string) {
	if r := requestFrom(ctx); r != nil {
		r.user.Store(&username)
	}
}

// NewRequestID returns a random request ID: 16 hex characters.
func NewRequestID() string {
	var b [8]byte
	rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// ValidRequestID reports whether an ID from elsewhere (X-Request-ID set by a
// proxy) is safe to put in logs: 1–64 letters, digits, '-', '_' or '.'.
func ValidRequestID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.') {
			return false
		}
	}
	return true
}

// contextHandler adds request_id and user from the context to every record.
type contextHandler struct {
	h slog.Handler
}

func (c *contextHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return c.h.Enabled(ctx, level)
}

func (c *contextHandler) Handle(ctx context.Context, rec slog.Record) error {
	if r := requestFrom(ctx); r != nil {
		rec = rec.Clone()
		rec.AddAttrs(slog.String("request_id", r.id))
		if u := r.user.Load(); u != nil {
			rec.AddAttrs(slog.String("user", *u))
		}
	}
	return c.h.Handle(ctx, rec)
}

func (c *contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &contextHandler{c.h.WithAttrs(attrs)}
}

func (c *contextHandler) WithGroup(name string) slog.Handler {
	return &contextHandler{c.h.WithGroup(name)}
}
