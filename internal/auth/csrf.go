package auth

import (
	"context"
	"crypto/subtle"
	"log/slog"
	"net/http"
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

// CSRFToken returns the CSRF token for the request context, for embedding in
// forms and htmx headers.
func CSRFToken(ctx context.Context) string {
	t, _ := ctx.Value(csrfKey{}).(string)
	return t
}

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
		if token == "" {
			token = NewToken()
			http.SetCookie(w, c.cookie(c.CSRFName(), token, time.Now().AddDate(1, 0, 0)))
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), csrfKey{}, token)))
	})
	return cop.Handler(check)
}

func isSafeMethod(m string) bool {
	return m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions
}
