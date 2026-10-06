// Package server wires the transports and middleware into one http.Handler.
package server

import (
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/sqprrr/ExtendedTimetable/internal/api"
	"github.com/sqprrr/ExtendedTimetable/internal/auth"
	"github.com/sqprrr/ExtendedTimetable/internal/service"
	"github.com/sqprrr/ExtendedTimetable/internal/web"
)

// Config configures the HTTP server.
type Config struct {
	// SecureCookies marks cookies Secure. Disable only for local HTTP development.
	SecureCookies bool
	// TrustProxy takes the client IP from X-Real-IP (set by nginx).
	TrustProxy bool
	// Location is the time zone dates are shown and entered in; UTC if nil.
	Location *time.Location
}

// New returns the application's root handler.
func New(svc *service.Service, cfg Config) (http.Handler, error) {
	cookies := auth.Cookies{Secure: cfg.SecureCookies}
	webH, err := web.New(svc, web.Config{Cookies: cookies, TrustProxy: cfg.TrustProxy, Location: cfg.Location})
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok\n")) })
	webH.Register(mux)
	api.New(svc).Register(mux)

	var h http.Handler = mux
	h = cookies.LoadSession(svc.Authenticate)(h)
	h = cookies.CSRF(h)
	h = securityHeaders(h)
	h = recoverer(h)
	return h, nil
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hd := w.Header()
		hd.Set("Content-Security-Policy",
			"default-src 'self'; img-src 'self' data:; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		hd.Set("X-Content-Type-Options", "nosniff")
		hd.Set("X-Frame-Options", "DENY")
		hd.Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

func recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				slog.ErrorContext(r.Context(), "panic", "value", v, "stack", string(debug.Stack()))
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
