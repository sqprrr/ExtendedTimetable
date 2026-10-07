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
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/sqprrr/ExtendedTimetable/internal/auth"
	"github.com/sqprrr/ExtendedTimetable/internal/i18n"
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
	icons      iconSet
	// pages holds the parsed templates per language, then per page.
	pages map[string]map[string]*template.Template
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
	"group", "subjects", "links", "homework", "homework_detail", "notes", "resources", "grades", "schedule",
	"feedback", "feedback_inbox",
}

// New parses the templates and returns a Handler.
func New(svc *service.Service, cfg Config) (*Handler, error) {
	h := &Handler{svc: svc, cookies: cfg.Cookies, trustProxy: cfg.TrustProxy, loc: cfg.Location, pages: map[string]map[string]*template.Template{}}
	if h.loc == nil {
		h.loc = time.UTC
	}
	icons, err := loadIcons(assets.Icons)
	if err != nil {
		return nil, fmt.Errorf("load icons: %w", err)
	}
	h.icons = icons
	// Each language gets its own template set, so the translation functions
	// are bound once at startup instead of per request.
	for _, lang := range i18n.Languages {
		funcs := h.templateFuncs(i18n.For(lang))
		h.pages[lang] = map[string]*template.Template{}
		for _, page := range pages {
			t, err := template.New(page).Funcs(funcs).ParseFS(assets.Templates,
				"templates/layout.html", "templates/partials.html", "templates/"+page+".html")
			if err != nil {
				return nil, fmt.Errorf("parse template %s: %w", page, err)
			}
			h.pages[lang][page] = t
		}
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
	mux.HandleFunc("POST /lang", h.setLang)
	h.registerGroupRoutes(mux)
	h.registerFeedbackRoutes(mux)
}

// pageData is passed to every template.
type pageData struct {
	Viewer    *service.Viewer
	CSRFToken string
	// Back is the page the language switch returns to.
	Back   string
	Error  string
	Status string
	Form   formValues
	Groups []service.GroupSummary
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
	// HomeworkFilter is the homework list's subject and status filter.
	HomeworkFilter service.HomeworkFilter
	// Query is the page's query string.
	Query     url.Values
	Homework  *service.Homework
	Notes     []*store.Note
	Resources []*store.ResourceLink
	Grades    *service.Grades
	// ProgressPanel replaces the progress panel of the homework page, to show
	// a rejected grade with its error.
	ProgressPanel *hwItem
	// Feedback is the viewer's own feedback, or the superadmins' inbox.
	Feedback []*store.Feedback
	// FeedbackOpen counts the unresolved feedback (superadmins only).
	FeedbackOpen int
	// ShowAll shows resolved feedback in the inbox too.
	ShowAll bool
	// Notice is a confirmation shown at the top of the page.
	Notice string
	// Schedule is the schedule page's week, or the overview's today.
	Schedule *service.Schedule
	Week     *weekView
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
	// Filter is the homework list's filter query, to keep it on that trip.
	Filter string
	// Error and GradeInput redisplay a rejected grade in the progress panel.
	Error      string
	GradeInput string
	// UpdateBadge also updates the Overdue badge on the homework page when
	// the panel is swapped in by htmx.
	UpdateBadge bool
}

func (h *Handler) templateFuncs(l *i18n.Localizer) template.FuncMap {
	return template.FuncMap{
		"lang": l.Lang,
		"t":    l.T,
		"icon": h.icons.html,
		// th is for translations that hold markup (links, <code>). The
		// messages are ours; the values put into them are escaped.
		"th": func(id string, kv ...any) template.HTML {
			for i := 1; i < len(kv); i += 2 {
				kv[i] = template.HTMLEscapeString(fmt.Sprint(kv[i]))
			}
			return template.HTML(l.T(id, kv...))
		},
		// markdown renders sanitized HTML, so it is safe to mark as such.
		"markdown": func(s string) template.HTML { return template.HTML(markdown.ToHTML(s)) },
		"datetime": func(t *time.Time) string {
			if t == nil {
				return ""
			}
			return h.when(l, *t)
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
			return hwItem{Code: d.Group.Code, CSRF: d.CSRFToken, HW: hw, From: from, Filter: homeworkFilterQuery(d.HomeworkFilter)}
		},
		"statuses":   func() []store.ProgressStatus { return service.Statuses },
		"nextStatus": service.NextStatus,
		"statusLabel": func(st store.ProgressStatus) string {
			return label(l, "status.", string(st))
		},
		"roleLabel": func(r store.Role) string { return label(l, "role.", string(r)) },
		"num":       formatPoints,
		"clock":     func(t time.Time) string { return t.In(h.loc).Format("15:04") },
		"when":      func(t time.Time) string { return h.when(l, t) },
		"dayName": func(t time.Time) string {
			t = t.In(h.loc)
			return l.T("weekday.long."+weekdayKey(t)) + ", " + t.Format("02.01")
		},
		"classType": func(e *service.ScheduleEvent) string {
			if e.LessonType == "" {
				return e.CISTType
			}
			return label(l, "lesson.", string(e.LessonType))
		},
		"classItem": func(s *service.Schedule, e *service.ScheduleEvent) classItem {
			return classItem{E: e, Next: !e.Now && s.IsUpcoming(e)}
		},
		// nextLater is the next class when it is not among s.Events (after
		// today on the overview, after this week on the schedule page).
		"nextLater": func(s *service.Schedule) *service.ScheduleEvent {
			if len(s.Upcoming) == 0 {
				return nil
			}
			for _, e := range s.Events {
				if e.ID == s.Upcoming[0].ID {
					return nil
				}
			}
			return s.Upcoming[0]
		},
		"percent": func(earned, max float64) string {
			if max <= 0 {
				return "—"
			}
			return strconv.FormatFloat(earned/max*100, 'f', 0, 64) + "%"
		},
		// The label funcs take any so templates can pass both typed values
		// and string literals.
		"lessonLabel":   func(t any) string { return label(l, "lesson.", fmt.Sprint(t)) },
		"kindLabel":     func(k any) string { return label(l, "kind.", fmt.Sprint(k)) },
		"idstr":         func(id int64) string { return strconv.FormatInt(id, 10) },
		"feedbackKinds": func() []store.FeedbackKind { return service.FeedbackKinds },
		"feedbackKind":  func(k store.FeedbackKind) string { return label(l, "feedback.kind.", string(k)) },
		"ratings":       func() []int64 { return []int64{5, 4, 3, 2, 1} },
		"stars": func(n int64) string {
			n = max(0, min(n, 5))
			return strings.Repeat("★", int(n)) + strings.Repeat("☆", 5-int(n))
		},
	}
}

// when formats a moment with a short weekday: "Пн 01.09.2026 09:30".
func (h *Handler) when(l *i18n.Localizer, t time.Time) string {
	t = t.In(h.loc)
	return l.T("weekday.short."+weekdayKey(t)) + " " + t.Format("02.01.2006 15:04")
}

// weekdayKey is the locale key of t's weekday: "mon", "tue", …
func weekdayKey(t time.Time) string {
	return strings.ToLower(t.Weekday().String()[:3])
}

// label translates an enum value (a status, lesson type, role), or returns
// it as is when it is empty.
func label(l *i18n.Localizer, prefix, value string) string {
	if value == "" {
		return ""
	}
	return l.T(prefix + value)
}

// formValues echoes non-secret fields back into a form after an error.
type formValues struct {
	Username string
	Group    string
}

func (h *Handler) render(w http.ResponseWriter, r *http.Request, status int, page string, data pageData) {
	data.Viewer = service.ViewerFrom(r.Context())
	data.CSRFToken = auth.CSRFToken(r.Context())
	data.Back = backPath(r)
	l := i18n.FromContext(r.Context())
	var buf bytes.Buffer
	if err := h.pages[l.Lang()][page].ExecuteTemplate(&buf, "layout", data); err != nil {
		slog.ErrorContext(r.Context(), "render template", "page", page, "err", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Language", l.Lang())
	w.WriteHeader(status)
	buf.WriteTo(w)
}

// renderFragment renders one shared template on its own, as the answer to an
// htmx request.
func (h *Handler) renderFragment(w http.ResponseWriter, r *http.Request, name string, data any) {
	var buf bytes.Buffer
	// Every page set holds partials.html; any of them will do.
	if err := h.pages[i18n.FromContext(r.Context()).Lang()]["homework"].ExecuteTemplate(&buf, name, data); err != nil {
		slog.ErrorContext(r.Context(), "render fragment", "name", name, "err", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	buf.WriteTo(w)
}

// renderError renders the error page for service errors.
func (h *Handler) renderError(w http.ResponseWriter, r *http.Request, err error) {
	l := i18n.FromContext(r.Context())
	status, msg := http.StatusInternalServerError, "error.internal"
	switch {
	case errors.Is(err, service.ErrUnauthenticated):
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	case errors.Is(err, service.ErrForbidden):
		status, msg = http.StatusForbidden, "error.forbidden"
	case errors.Is(err, service.ErrNotFound):
		status, msg = http.StatusNotFound, "error.not_found"
	default:
		slog.ErrorContext(r.Context(), "request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	}
	title := strconv.Itoa(status) + " " + l.T("error.status."+strconv.Itoa(status))
	h.render(w, r, status, "error", pageData{Status: title, Error: l.T(msg)})
}

// knownErrors are the service errors a user can act on, with the message
// shown for them and the response status.
var knownErrors = []struct {
	err    error
	msg    i18n.Message
	status int
}{
	{service.ErrInvalidCredentials, i18n.M("err.invalid_credentials"), http.StatusUnprocessableEntity},
	{service.ErrUsernameTaken, i18n.M("err.username_taken"), http.StatusUnprocessableEntity},
	{service.ErrRateLimited, i18n.M("err.rate_limited"), http.StatusTooManyRequests},
	{service.ErrSyncTooSoon, i18n.M("err.sync_too_soon", "Count", service.SyncCooldownMinutes), http.StatusTooManyRequests},
	{service.ErrSyncRunning, i18n.M("err.sync_running"), http.StatusTooManyRequests},
	{service.ErrNoCISTGroup, i18n.M("err.no_cist_group"), http.StatusConflict},
	{service.ErrSyncDisabled, i18n.M("err.sync_disabled"), http.StatusConflict},
	{service.ErrSubjectInUse, i18n.M("err.subject_in_use"), http.StatusConflict},
}

// userMessage turns expected service errors into a translated form error
// message. Unexpected errors return ok=false.
func userMessage(r *http.Request, err error) (msg string, status int, ok bool) {
	l := i18n.FromContext(r.Context())
	var ie *service.InputError
	if errors.As(err, &ie) {
		return l.Msg(ie.Msg), http.StatusUnprocessableEntity, true
	}
	for _, k := range knownErrors {
		if errors.Is(err, k.err) {
			return l.Msg(k.msg), k.status, true
		}
	}
	return "", 0, false
}
