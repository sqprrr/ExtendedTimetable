// Package api serves the JSON API under /api/v1. It calls the same service
// methods as the HTML handlers so a future SPA can replace the templates.
package api

import (
	"cmp"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/sqprrr/ExtendedTimetable/internal/auth"
	"github.com/sqprrr/ExtendedTimetable/internal/i18n"
	"github.com/sqprrr/ExtendedTimetable/internal/service"
)

// Handler serves the JSON API.
type Handler struct {
	svc *service.Service
	// loc is the time zone that schedule dates in queries are read in.
	loc *time.Location
}

// New returns an API handler. loc is the time zone of dates in queries; UTC
// if nil.
func New(svc *service.Service, loc *time.Location) *Handler {
	if loc == nil {
		loc = time.UTC
	}
	return &Handler{svc: svc, loc: loc}
}

// Register adds the API routes to mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/me", h.me)
	mux.HandleFunc("PUT /api/v1/me", h.updateMe)
	h.registerGroupRoutes(mux)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, http.StatusNotFound, "not found")
	})
}

type groupJSON struct {
	ID   int64  `json:"id"`
	Code string `json:"code"`
	Name string `json:"name"`
	Role string `json:"role,omitempty"`
	// CanEdit: may change the group's content (leader, editors, superadmins).
	CanEdit bool `json:"can_edit"`
	// CanManage: may handle the invite link and members (leader, superadmins).
	CanManage bool `json:"can_manage"`
}

type meJSON struct {
	Username     string `json:"username"`
	IsSuperadmin bool   `json:"is_superadmin"`
	// Locale is the language the user sees: their choice, else the
	// language cookie, else the default.
	Locale string `json:"locale"`
	// Theme is the colour theme the user chose: "light", "dark" or
	// "system" (the device's, also when they have not chosen).
	Theme  string      `json:"theme"`
	Groups []groupJSON `json:"groups"`
	// CSRFToken must be sent as the X-CSRF-Token header on unsafe requests.
	CSRFToken string `json:"csrf_token"`
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	v := service.ViewerFrom(r.Context())
	if v == nil {
		h.fail(w, r, service.ErrUnauthenticated)
		return
	}
	groups, err := h.svc.MyGroups(r.Context())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	out := meJSON{
		Username:     v.Username,
		IsSuperadmin: v.IsSuperadmin,
		Locale:       i18n.FromContext(r.Context()).Lang(),
		Theme:        cmp.Or(v.Theme, service.ThemeSystem),
		Groups:       make([]groupJSON, 0, len(groups)),
		CSRFToken:    auth.CSRFToken(r.Context()),
	}
	for _, g := range groups {
		out.Groups = append(out.Groups, groupJSON{
			ID: g.ID, Code: g.Code, Name: g.Name, Role: string(g.Role), CanEdit: g.CanEdit, CanManage: g.CanManage,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// updateMe changes the viewer's settings; the body is {"locale": "uk"|"en",
// "theme": "light"|"dark"|"system"}, either field optional.
func (h *Handler) updateMe(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Locale *string `json:"locale"`
		Theme  *string `json:"theme"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Locale != nil {
		if err := h.svc.SetLocale(r.Context(), *in.Locale); err != nil {
			h.fail(w, r, err)
			return
		}
		r = r.WithContext(i18n.WithLang(r.Context(), *in.Locale))
	}
	if in.Theme != nil {
		if err := h.svc.SetTheme(r.Context(), *in.Theme); err != nil {
			h.fail(w, r, err)
			return
		}
	}
	h.me(w, r)
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	var ie *service.InputError
	switch {
	case errors.As(err, &ie):
		// "code" is the message ID, for a client that translates on its own.
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{
			"error": i18n.English(ie.Msg), "code": ie.Msg.ID, "field": ie.Field,
		})
	case errors.Is(err, service.ErrSubjectInUse), errors.Is(err, service.ErrNoCISTGroup), errors.Is(err, service.ErrSyncDisabled):
		writeError(w, r, http.StatusConflict, err.Error())
	case errors.Is(err, service.ErrSyncTooSoon), errors.Is(err, service.ErrSyncRunning):
		writeError(w, r, http.StatusTooManyRequests, err.Error())
	case errors.As(err, new(*service.SyncError)):
		writeError(w, r, http.StatusBadGateway, err.Error())
	case errors.Is(err, service.ErrUnauthenticated):
		writeError(w, r, http.StatusUnauthorized, err.Error())
	case errors.Is(err, service.ErrForbidden):
		writeError(w, r, http.StatusForbidden, err.Error())
	case errors.Is(err, service.ErrNotFound):
		writeError(w, r, http.StatusNotFound, err.Error())
	default:
		slog.ErrorContext(r.Context(), "api request failed", "method", r.Method, "path", r.URL.Path, "err", err)
		writeError(w, r, http.StatusInternalServerError, "internal error")
	}
}

func writeError(w http.ResponseWriter, _ *http.Request, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
