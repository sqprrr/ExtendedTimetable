// Package api serves the JSON API under /api/v1. It calls the same service
// methods as the HTML handlers so a future SPA can replace the templates.
package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/sqprrr/ExtendedTimetable/internal/auth"
	"github.com/sqprrr/ExtendedTimetable/internal/service"
)

// Handler serves the JSON API.
type Handler struct {
	svc *service.Service
}

// New returns an API handler.
func New(svc *service.Service) *Handler { return &Handler{svc: svc} }

// Register adds the API routes to mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/me", h.me)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, http.StatusNotFound, "not found")
	})
}

type groupJSON struct {
	ID        int64  `json:"id"`
	Code      string `json:"code"`
	Name      string `json:"name"`
	Role      string `json:"role,omitempty"`
	CanManage bool   `json:"can_manage"`
}

type meJSON struct {
	Username     string      `json:"username"`
	IsSuperadmin bool        `json:"is_superadmin"`
	Locale       string      `json:"locale"`
	Groups       []groupJSON `json:"groups"`
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
		Locale:       v.Locale,
		Groups:       make([]groupJSON, 0, len(groups)),
		CSRFToken:    auth.CSRFToken(r.Context()),
	}
	for _, g := range groups {
		out.Groups = append(out.Groups, groupJSON{
			ID: g.ID, Code: g.Code, Name: g.Name, Role: string(g.Role), CanManage: g.CanManage,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
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
