package auth

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"
)

// Cookies builds the session and CSRF cookies.
type Cookies struct {
	// Secure marks cookies Secure and enables the __Host- name prefix.
	// Disable only for local development over plain HTTP.
	Secure bool
}

func (c Cookies) name(base string) string {
	if c.Secure {
		return "__Host-" + base
	}
	return base
}

// SessionName is the session cookie name.
func (c Cookies) SessionName() string { return c.name("extt_session") }

// CSRFName is the CSRF cookie name.
func (c Cookies) CSRFName() string { return c.name("extt_csrf") }

func (c Cookies) cookie(name, value string, expires time.Time) *http.Cookie {
	ck := &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   c.Secure,
		SameSite: http.SameSiteLaxMode,
	}
	if !expires.IsZero() {
		ck.Expires = expires
		ck.MaxAge = max(int(time.Until(expires).Seconds()), 1)
	}
	return ck
}

// SetSession writes the session cookie.
func (c Cookies) SetSession(w http.ResponseWriter, token string, expires time.Time) {
	http.SetCookie(w, c.cookie(c.SessionName(), token, expires))
}

// ClearSession deletes the session cookie.
func (c Cookies) ClearSession(w http.ResponseWriter) {
	ck := c.cookie(c.SessionName(), "", time.Time{})
	ck.MaxAge = -1
	http.SetCookie(w, ck)
}

// SessionToken returns the session token from the request, if any.
func (c Cookies) SessionToken(r *http.Request) string { return cookieValue(r, c.SessionName()) }

// cookieValue returns the value of the cookie named name, or "" if there is none.
func cookieValue(r *http.Request, name string) string {
	ck, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	return ck.Value
}

// LangName is the language cookie name.
func (c Cookies) LangName() string { return c.name("extt_lang") }

// langTTL is how long the language, theme and time zone choices are
// remembered.
const langTTL = 365 * 24 * time.Hour

// SetLang remembers the visitor's language.
func (c Cookies) SetLang(w http.ResponseWriter, lang string) {
	http.SetCookie(w, c.cookie(c.LangName(), lang, time.Now().Add(langTTL)))
}

// Lang returns the language cookie's value, if any.
func (c Cookies) Lang(r *http.Request) string { return cookieValue(r, c.LangName()) }

// ThemeName is the colour theme cookie name.
func (c Cookies) ThemeName() string { return c.name("extt_theme") }

// SetTheme remembers the visitor's colour theme.
func (c Cookies) SetTheme(w http.ResponseWriter, theme string) {
	http.SetCookie(w, c.cookie(c.ThemeName(), theme, time.Now().Add(langTTL)))
}

// Theme returns the colour theme cookie's value, if any.
func (c Cookies) Theme(r *http.Request) string { return cookieValue(r, c.ThemeName()) }

// TimeZoneName is the name of the cookie holding the time zone choice.
func (c Cookies) TimeZoneName() string { return c.name("extt_tz") }

// SetTimeZone remembers the visitor's time zone choice.
func (c Cookies) SetTimeZone(w http.ResponseWriter, tz string) {
	http.SetCookie(w, c.cookie(c.TimeZoneName(), tz, time.Now().Add(langTTL)))
}

// TimeZone returns the time zone choice cookie's value, if any.
func (c Cookies) TimeZone(r *http.Request) string { return cookieValue(r, c.TimeZoneName()) }

// DeviceTimeZoneName is the name of the cookie in which the browser's
// script reports the device's time zone. Unlike the others, the script
// writes it, so it is not HttpOnly.
func (c Cookies) DeviceTimeZoneName() string { return c.name("extt_device_tz") }

// DeviceTimeZone returns the time zone the device reported, if any.
func (c Cookies) DeviceTimeZone(r *http.Request) string {
	return cookieValue(r, c.DeviceTimeZoneName())
}

type renewedKey struct{}

// WithRenewedSession returns a context that tells LoadSession the session
// now lasts until expires, so it sends the cookie again.
func WithRenewedSession(ctx context.Context, expires time.Time) context.Context {
	return context.WithValue(ctx, renewedKey{}, expires)
}

// ErrNoSession is returned by an Authenticator when the token does not
// correspond to a live session.
var ErrNoSession = errors.New("auth: no valid session")

// Authenticator resolves a session token and returns a context carrying the
// signed-in user, marked with WithRenewedSession if it extended the session.
// It returns ErrNoSession for unknown or expired tokens.
type Authenticator func(ctx context.Context, token string) (context.Context, error)

// LoadSession resolves the session cookie on every request. Requests without
// a valid session continue anonymously and have the stale cookie cleared.
func (c Cookies) LoadSession(authn Authenticator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := c.SessionToken(r)
			if token == "" {
				next.ServeHTTP(w, r)
				return
			}
			ctx, err := authn(r.Context(), token)
			switch {
			case errors.Is(err, ErrNoSession):
				c.ClearSession(w)
			case err != nil:
				slog.ErrorContext(r.Context(), "load session", "err", err)
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				return
			default:
				if expires, ok := ctx.Value(renewedKey{}).(time.Time); ok {
					c.SetSession(w, token, expires)
				}
				r = r.WithContext(ctx)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ClientIP returns the client address. When trustProxy is set, the
// X-Real-IP header (set by nginx) is used instead of the TCP peer.
func ClientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if ip := strings.TrimSpace(r.Header.Get("X-Real-IP")); ip != "" {
			return ip
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
