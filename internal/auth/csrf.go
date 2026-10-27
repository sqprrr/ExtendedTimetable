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

// csrfToken is a request's CSRF token. A request without the cookie gets a
// new token, sent in a cookie only once the response embeds it.
type csrfToken struct {
	value string
	issue func() // sets the cookie for a new token; nil once sent or not needed
}

// CSRFToken returns the CSRF token for the request context, for embedding in
// forms and htmx headers. Call it before the response is written: for a
// request without the cookie, it sets the cookie.
func CSRFToken(ctx context.Context) string {
	t, _ := ctx.Value(csrfKey{}).(*csrfToken)
	if t == nil {
		return ""
	}
	if t.issue != nil {
		t.issue()
		t.issue = nil
	}
	return t.value
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
		t := &csrfToken{value: token}
		if token == "" {
			// Only responses that embed the token set the cookie. Were every
			// response without it to set one, a request sent without cookies
			// (iOS fetching the touch icon or favicon) would replace the
			// cookie the open page's token matches, and its next form post
			// would fail.
			t.value = NewToken()
			t.issue = func() {
				http.SetCookie(w, c.cookie(c.CSRFName(), t.value, time.Now().AddDate(1, 0, 0)))
			}
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), csrfKey{}, t)))
	})
	return cop.Handler(check)
}

func isSafeMethod(m string) bool {
	return m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions
}
