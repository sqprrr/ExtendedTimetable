// Package web serves the server-rendered HTML pages. Handlers only parse
// input, call the service and render templates.
package web

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/sqprrr/ExtendedTimetable/internal/auth"
	"github.com/sqprrr/ExtendedTimetable/internal/service"
	assets "github.com/sqprrr/ExtendedTimetable/web"
)

// Handler serves the HTML UI.
type Handler struct {
	svc        *service.Service
	cookies    auth.Cookies
	trustProxy bool
	pages      map[string]*template.Template
}

// Config configures the HTML handler.
type Config struct {
	Cookies auth.Cookies
	// TrustProxy uses X-Real-IP for the client address (behind nginx).
	TrustProxy bool
}

// New parses the templates and returns a Handler.
func New(svc *service.Service, cfg Config) (*Handler, error) {
	h := &Handler{svc: svc, cookies: cfg.Cookies, trustProxy: cfg.TrustProxy, pages: map[string]*template.Template{}}
	for _, page := range []string{"home", "login", "register", "error"} {
		t, err := template.ParseFS(assets.Templates, "templates/layout.html", "templates/"+page+".html")
		if err != nil {
			return nil, fmt.Errorf("parse template %s: %w", page, err)
		}
		h.pages[page] = t
	}
	return h, nil
}

// Register adds the HTML routes to mux.
func (h *Handler) Register(mux *http.ServeMux) {
	static, _ := fs.Sub(assets.Static, "static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(static)))

	mux.HandleFunc("GET /{$}", h.home)
	mux.HandleFunc("GET /login", h.loginForm)
	mux.HandleFunc("POST /login", h.login)
	mux.HandleFunc("GET /register", h.registerForm)
	mux.HandleFunc("POST /register", h.register)
	mux.HandleFunc("POST /logout", h.logout)
	mux.HandleFunc("POST /groups/{id}/invite-code", h.regenerateInviteCode)
}

// pageData is passed to every template.
type pageData struct {
	Viewer    *service.Viewer
	CSRFToken string
	Error     string
	Status    string
	Form      formValues
	Groups    []service.GroupSummary
}

// formValues echoes non-secret fields back into a form after an error.
type formValues struct {
	Username   string
	InviteCode string
}

func (h *Handler) render(w http.ResponseWriter, r *http.Request, status int, page string, data pageData) {
	data.Viewer = service.ViewerFrom(r.Context())
	data.CSRFToken = auth.CSRFToken(r.Context())
	var buf bytes.Buffer
	if err := h.pages[page].ExecuteTemplate(&buf, "layout", data); err != nil {
		slog.ErrorContext(r.Context(), "render template", "page", page, "err", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	buf.WriteTo(w)
}

// renderError renders the error page for service errors.
func (h *Handler) renderError(w http.ResponseWriter, r *http.Request, err error) {
	status, msg := http.StatusInternalServerError, "Something went wrong. Please try again later."
	switch {
	case errors.Is(err, service.ErrUnauthenticated):
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	case errors.Is(err, service.ErrForbidden):
		status, msg = http.StatusForbidden, "You do not have permission to do that."
	case errors.Is(err, service.ErrNotFound):
		status, msg = http.StatusNotFound, "Page not found."
	default:
		slog.ErrorContext(r.Context(), "request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	}
	h.render(w, r, status, "error", pageData{Status: strconv.Itoa(status) + " " + http.StatusText(status), Error: msg})
}

// userMessage turns expected service errors into a form error message.
// Unexpected errors return ok=false.
func userMessage(err error) (msg string, status int, ok bool) {
	var ie *service.InputError
	switch {
	case errors.As(err, &ie):
		return capitalize(ie.Msg), http.StatusUnprocessableEntity, true
	case errors.Is(err, service.ErrInvalidCredentials),
		errors.Is(err, service.ErrInvalidInviteCode),
		errors.Is(err, service.ErrUsernameTaken):
		return capitalize(err.Error()), http.StatusUnprocessableEntity, true
	case errors.Is(err, service.ErrRateLimited):
		return capitalize(err.Error()), http.StatusTooManyRequests, true
	}
	return "", 0, false
}

func capitalize(s string) string {
	if s == "" || s[0] < 'a' || s[0] > 'z' {
		return s
	}
	return string(s[0]-'a'+'A') + s[1:] + "."
}
