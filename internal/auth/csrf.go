package auth

import (
	"context"
	"crypto/subtle"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/sqprrr/ExtendedTimetable/internal/metrics"
)

const (
	// CSRFField is the form field carrying the CSRF token.
	CSRFField = "csrf_token"
	// CSRFHeader is the request header carrying the CSRF token (htmx, fetch).
	CSRFHeader = "X-CSRF-Token"
)

type csrfKey struct{}

// csrfToken is a request's CSRF token. A request without the cookie gets a
// new token, and the cookie for it is set only by a response that embeds
// it: were every such response to set one, a request sent without cookies
// (Safari fetching an icon or a stylesheet) would replace the cookie an
// open page's token matches, and that page's next form would fail.
type csrfToken struct {
	value string
	// w is the response a new token's cookie goes on, nil when the request
	// had the cookie or once it is set.
	w *csrfWriter
}

// CSRFToken returns the CSRF token for the request context, for embedding in
// forms and htmx headers. For a new token it sets the cookie, so it must be
// called before the response is written.
func CSRFToken(ctx context.Context) string {
	t, _ := ctx.Value(csrfKey{}).(*csrfToken)
	if t == nil {
		return ""
	}
	if w := t.w; w != nil {
		t.w = nil
		if w.wrote {
			lateCSRFToken(ctx)
		}
		http.SetCookie(w, w.cookies.cookie(w.cookies.CSRFName(), t.value, time.Now().AddDate(1, 0, 0)))
	}
	return t.value
}

// lateCSRFToken reports a new token asked for after the response headers
// went out: its cookie is lost, and the page's forms fail with "no CSRF
// cookie". Under go test it panics, so no such handler passes the tests; in
// production, where the response is half sent and a panic could no longer
// become a 500, it logs an error.
func lateCSRFToken(ctx context.Context) {
	const msg = "CSRF token asked for after the response was written; its cookie is lost"
	if lateCSRFPanics {
		panic(msg)
	}
	slog.ErrorContext(ctx, msg)
}

var lateCSRFPanics = testing.Testing()

// csrfWriter notes when the response headers go out, after which no cookie
// can be set.
type csrfWriter struct {
	http.ResponseWriter
	cookies Cookies
	wrote   bool
}

func (w *csrfWriter) WriteHeader(code int) {
	if code >= 200 { // 1xx answers are not the final headers
		w.wrote = true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *csrfWriter) Write(b []byte) (int, error) {
	w.wrote = true
	return w.ResponseWriter.Write(b)
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (w *csrfWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// CSRF protects state-changing requests with a double-submit token: a random
// value in an HttpOnly cookie that must be echoed in the form field or header.
// It also rejects cross-origin requests using Fetch metadata / Origin headers.
func (c Cookies) CSRF(next http.Handler) http.Handler {
	cop := http.NewCrossOriginProtection()
	cop.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slog.WarnContext(r.Context(), "cross-origin request rejected", "method", r.Method, "path", r.URL.Path,
			"origin", r.Header.Get("Origin"), "sec_fetch_site", r.Header.Get("Sec-Fetch-Site"))
		metrics.CSRFRejected(metrics.CSRFCrossOrigin)
		http.Error(w, "cross-origin request rejected", http.StatusForbidden)
	}))
	check := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := ""
		if ck, err := r.Cookie(c.CSRFName()); err == nil && len(ck.Value) >= 32 {
			token = ck.Value
		}
		if !isSafeMethod(r.Method) {
			sent := r.Header.Get(CSRFHeader)
			if sent == "" {
				sent = r.PostFormValue(CSRFField)
			}
			if token == "" || subtle.ConstantTimeCompare([]byte(sent), []byte(token)) != 1 {
				// The tokens themselves are never logged.
				reason, metric := "token mismatch", metrics.CSRFMismatch
				switch {
				case token == "":
					reason, metric = "no CSRF cookie", metrics.CSRFNoCookie
				case sent == "":
					reason, metric = "no token sent", metrics.CSRFNoToken
				}
				slog.WarnContext(r.Context(), "CSRF check failed", "method", r.Method, "path", r.URL.Path, "reason", reason)
				metrics.CSRFRejected(metric)
				http.Error(w, "invalid CSRF token, reload the page and try again", http.StatusForbidden)
				return
			}
		}
		t := &csrfToken{value: token}
		if token == "" {
			cw := &csrfWriter{ResponseWriter: w, cookies: c}
			t.value, t.w, w = NewToken(), cw, cw
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), csrfKey{}, t)))
	})
	return cop.Handler(check)
}

func isSafeMethod(m string) bool {
	return m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions
}
