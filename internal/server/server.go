// Package server wires the transports and middleware into one http.Handler.
package server

import (
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/sqprrr/ExtendedTimetable/internal/api"
	"github.com/sqprrr/ExtendedTimetable/internal/auth"
	"github.com/sqprrr/ExtendedTimetable/internal/i18n"
	"github.com/sqprrr/ExtendedTimetable/internal/metrics"
	"github.com/sqprrr/ExtendedTimetable/internal/service"
	"github.com/sqprrr/ExtendedTimetable/internal/web"
)

// Config configures the HTTP server.
type Config struct {
	// SecureCookies marks cookies Secure. Disable only for local HTTP development.
	SecureCookies bool
	// TrustProxy takes the client IP from X-Real-IP (set by nginx).
	TrustProxy bool
	// BaseURL is the site's public address for invite links; if empty, it
	// is taken from each request.
	BaseURL string
}

// New returns the application's root handler.
func New(svc *service.Service, cfg Config) (http.Handler, error) {
	cookies := auth.Cookies{Secure: cfg.SecureCookies}
	webH, err := web.New(svc, web.Config{Cookies: cookies, TrustProxy: cfg.TrustProxy, BaseURL: cfg.BaseURL})
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok\n")) })
	webH.Register(mux)
	api.New(svc).Register(mux)

	// Outermost last. requestLog must wrap recoverer to log panics as 500s,
	// and recordUser must run after the session is loaded.
	var h http.Handler = mux
	h = language(cookies)(h)
	h = timeZone(cookies, svc)(h)
	h = recordUser(h)
	h = cookies.LoadSession(svc.Authenticate)(h)
	h = readSnapshot(svc)(h)
	h = cookies.CSRF(h)
	h = limitBody(h)
	h = securityHeaders(h)
	h = recoverer(h)
	h = requestLog(cfg.TrustProxy, mux, h)
	return h, nil
}

// language picks the request's language: the signed-in user's choice, else
// the language cookie, else the default (Ukrainian).
func language(cookies auth.Cookies) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			lang := cookies.Lang(r)
			if v := service.ViewerFrom(r.Context()); v != nil && v.Locale != "" {
				lang = v.Locale
			}
			if !i18n.IsSupported(lang) {
				lang = i18n.Default
			}
			next.ServeHTTP(w, r.WithContext(i18n.WithLang(r.Context(), lang)))
		})
	}
}

// timeZone puts the request's time zone in its context: the signed-in
// user's choice, else the time zone cookie's; for TimeZoneAuto (also when
// nothing was chosen), the zone the device reported, else the site's.
func timeZone(cookies auth.Cookies, svc *service.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			choice := service.TimeZoneChoice(r.Context(), cookies.TimeZone(r))
			next.ServeHTTP(w, r.WithContext(svc.WithTimeZone(r.Context(), choice, cookies.DeviceTimeZone(r))))
		})
	}
}

// maxBodyBytes bounds request bodies, like nginx's client_max_body_size:
// forms and JSON are a few kilobytes, and the CSRF check reads form bodies
// before anyone is known to be signed in.
const maxBodyBytes = 1 << 20

// limitBody refuses bodies over maxBodyBytes: at once when the request says
// its length, else when reading reaches the limit.
func limitBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength > maxBodyBytes {
			http.Error(w, http.StatusText(http.StatusRequestEntityTooLarge), http.StatusRequestEntityTooLarge)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		next.ServeHTTP(w, r)
	})
}

// readSnapshot runs the reads of a GET request, the session's included, in
// one read transaction (service.WithSnapshot).
func readSnapshot(svc *service.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet || r.Method == http.MethodHead {
				ctx, end := svc.WithSnapshot(r.Context())
				defer end()
				r = r.WithContext(ctx)
			}
			next.ServeHTTP(w, r)
		})
	}
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hd := w.Header()
		hd.Set("Content-Security-Policy",
			"default-src 'self'; img-src 'self' data:; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		hd.Set("X-Content-Type-Options", "nosniff")
		hd.Set("X-Frame-Options", "DENY")
		hd.Set("Referrer-Policy", "same-origin")
		// Pages and API answers are a user's own (grades, invite links, the
		// CSRF token), so no cache may keep them, not even a shared cache set
		// to cache everything. Static files set their own Cache-Control.
		hd.Set("Cache-Control", "no-store")
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
				metrics.Panic()
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
