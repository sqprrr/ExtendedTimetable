package server

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/sqprrr/ExtendedTimetable/internal/logging"
	"github.com/sqprrr/ExtendedTimetable/internal/metrics"
	"github.com/sqprrr/ExtendedTimetable/internal/service"
)

// maxLoggedPath keeps a client's absurdly long URL from flooding the log.
const maxLoggedPath = 200

// requestLog gives every request an ID (sent back as X-Request-ID) and logs
// one line when it is done: method, path, status, size and duration, plus
// the request ID and user that every line of the request carries. It also
// records the request in the metrics, by the mux pattern it matches: a
// pattern ("/g/{code}/homework") rather than the path keeps the number of
// series small, and is known even for requests that middleware rejects
// before they reach the mux. It must wrap everything else, recoverer
// included, so a panic is logged and counted as a 500.
//
// The query string and body are never logged: they can carry form values.
// Neither are invite tokens: anyone who reads one could join the group.
func requestLog(trustProxy bool, mux *http.ServeMux, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		id := ""
		if trustProxy {
			if v := r.Header.Get("X-Request-ID"); logging.ValidRequestID(v) {
				id = v
			}
		}
		if id == "" {
			id = logging.NewRequestID()
		}
		w.Header().Set("X-Request-ID", id)
		ctx := logging.WithRequest(r.Context(), id)
		sw := &statusWriter{ResponseWriter: w}
		done := metrics.RequestStarted()
		defer func() {
			done()
			elapsed := time.Since(start)
			status := sw.status
			if status == 0 {
				status = http.StatusOK
			}
			_, route := mux.Handler(r)
			metrics.ObserveRequest(r.Method, route, status, elapsed)
			path := loggedPath(r.URL.Path)
			if len(path) > maxLoggedPath {
				path = path[:maxLoggedPath] + "…"
			}
			level := slog.LevelInfo
			switch {
			case status >= 500:
				level = slog.LevelError
			case path == "/healthz" || strings.HasPrefix(path, "/static/"):
				level = slog.LevelDebug
			}
			slog.LogAttrs(ctx, level, "request",
				slog.String("method", r.Method),
				slog.String("path", path),
				slog.Int("status", status),
				slog.Int64("bytes", sw.bytes),
				slog.Float64("duration_ms", float64(elapsed.Microseconds())/1000),
			)
		}()
		next.ServeHTTP(sw, r.WithContext(ctx))
	})
}

// loggedPath hides the invite token in /join/<token>[/…].
func loggedPath(path string) string {
	rest, ok := strings.CutPrefix(path, "/join/")
	if !ok {
		return path
	}
	if _, tail, found := strings.Cut(rest, "/"); found {
		return "/join/…/" + tail
	}
	return "/join/…"
}

// recordUser notes the signed-in user for the request's log lines. It goes
// right after the session is loaded.
func recordUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if v := service.ViewerFrom(r.Context()); v != nil {
			logging.SetUser(r.Context(), v.Username)
		}
		next.ServeHTTP(w, r)
	})
}

// statusWriter remembers the status code and the number of body bytes.
type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 && code >= 200 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += int64(n)
	return n, err
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
