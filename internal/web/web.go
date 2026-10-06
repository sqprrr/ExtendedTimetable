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
	"time"

	"github.com/sqprrr/ExtendedTimetable/internal/auth"
	"github.com/sqprrr/ExtendedTimetable/internal/markdown"
	"github.com/sqprrr/ExtendedTimetable/internal/service"
	"github.com/sqprrr/ExtendedTimetable/internal/store"
	assets "github.com/sqprrr/ExtendedTimetable/web"
)

// Handler serves the HTML UI.
type Handler struct {
	svc        *service.Service
	cookies    auth.Cookies
	trustProxy bool
	loc        *time.Location
	pages      map[string]*template.Template
}

// Config configures the HTML handler.
type Config struct {
	Cookies auth.Cookies
	// TrustProxy uses X-Real-IP for the client address (behind nginx).
	TrustProxy bool
	// Location is the time zone dates are shown and entered in; UTC if nil.
	Location *time.Location
}

// pages lists the page templates; each is parsed with layout.html and
// partials.html.
var pages = []string{
	"home", "login", "register", "error",
	"group", "subjects", "links", "homework", "homework_detail", "notes", "resources", "grades",
}

// New parses the templates and returns a Handler.
func New(svc *service.Service, cfg Config) (*Handler, error) {
	h := &Handler{svc: svc, cookies: cfg.Cookies, trustProxy: cfg.TrustProxy, loc: cfg.Location, pages: map[string]*template.Template{}}
	if h.loc == nil {
		h.loc = time.UTC
	}
	funcs := h.templateFuncs()
	for _, page := range pages {
		t, err := template.New(page).Funcs(funcs).ParseFS(assets.Templates,
			"templates/layout.html", "templates/partials.html", "templates/"+page+".html")
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
	h.registerGroupRoutes(mux)
}

// pageData is passed to every template.
type pageData struct {
	Viewer    *service.Viewer
	CSRFToken string
	Error     string
	Status    string
	Form      formValues
	Groups    []service.GroupSummary
	// JoinableGroups fills the group picker on the registration form.
	JoinableGroups []service.JoinableGroup

	// Group pages.
	Group *service.GroupView
	// Section is the active group tab.
	Section string
	// Fields fills the create or edit form of a group section.
	Fields map[string]string
	// EditID is the item being edited; 0 shows the list and the create form.
	EditID       int64
	Subjects     []*store.Subject
	ClassLinks   []*store.ClassLink
	HomeworkList []*service.Homework
	Homework     *service.Homework
	Notes        []*store.Note
	Resources    []*store.ResourceLink
	Grades       *service.Grades
	// ProgressPanel replaces the progress panel of the homework page, to show
	// a rejected grade with its error.
	ProgressPanel *hwItem
}

// hwItem is what the homeworkItem and progressPanel templates render: one
// assignment with the viewer's progress.
type hwItem struct {
	Code string
	CSRF string
	HW   *service.Homework
	// From is the page the item is on (overview, list, detail), to come back
	// to after a form post without JavaScript.
	From string
	// Error and GradeInput redisplay a rejected grade in the progress panel.
	Error      string
	GradeInput string
	// UpdateBadge also updates the Overdue badge on the homework page when
	// the panel is swapped in by htmx.
	UpdateBadge bool
}

func (h *Handler) templateFuncs() template.FuncMap {
	return template.FuncMap{
		// markdown renders sanitized HTML, so it is safe to mark as such.
		"markdown": func(s string) template.HTML { return template.HTML(markdown.ToHTML(s)) },
		"datetime": func(t *time.Time) string {
			if t == nil {
				return ""
			}
			return t.In(h.loc).Format("Mon 02.01.2006 15:04")
		},
		"date": func(t time.Time) string { return t.In(h.loc).Format("02.01.2006") },
		"points": func(p *float64) string {
			if p == nil {
				return ""
			}
			return formatPoints(*p)
		},
		"lessonTypes": func() []store.LessonType { return service.LessonTypes },
		"hwItem": func(d pageData, hw *service.Homework, from string) hwItem {
			return hwItem{Code: d.Group.Code, CSRF: d.CSRFToken, HW: hw, From: from}
		},
		"statuses":   func() []store.ProgressStatus { return service.Statuses },
		"nextStatus": service.NextStatus,
		"statusLabel": func(st store.ProgressStatus) string {
			switch st {
			case store.StatusNotStarted:
				return "Not started"
			case store.StatusInProgress:
				return "In progress"
			case store.StatusDone:
				return "Done"
			}
			return string(st)
		},
		"num": formatPoints,
		"percent": func(earned, max float64) string {
			if max <= 0 {
				return "—"
			}
			return strconv.FormatFloat(earned/max*100, 'f', 0, 64) + "%"
		},
		"lessonLabel": func(t store.LessonType) string {
			switch t {
			case store.LessonLecture:
				return "Lecture"
			case store.LessonPractice:
				return "Practice"
			case store.LessonLab:
				return "Lab"
			}
			return string(t)
		},
		"kindLabel": func(k store.ResourceKind) string {
			switch k {
			case store.ResourceRecording:
				return "Recording"
			case store.ResourceSolution:
				return "Solution"
			}
			return string(k)
		},
		"idstr": func(id int64) string { return strconv.FormatInt(id, 10) },
	}
}

// formValues echoes non-secret fields back into a form after an error.
type formValues struct {
	Username string
	Group    string
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

// renderFragment renders one shared template on its own, as the answer to an
// htmx request.
func (h *Handler) renderFragment(w http.ResponseWriter, r *http.Request, name string, data any) {
	var buf bytes.Buffer
	// Every page set holds partials.html; any of them will do.
	if err := h.pages["homework"].ExecuteTemplate(&buf, name, data); err != nil {
		slog.ErrorContext(r.Context(), "render fragment", "name", name, "err", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
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
