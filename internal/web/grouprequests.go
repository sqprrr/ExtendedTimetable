package web

import (
	"log/slog"
	"net/http"

	"github.com/sqprrr/ExtendedTimetable/internal/auth"
	"github.com/sqprrr/ExtendedTimetable/internal/i18n"
	"github.com/sqprrr/ExtendedTimetable/internal/service"
)

// registerGroupRequestRoutes adds the group request form, open to visitors,
// and the superadmins' list of requests.
func (h *Handler) registerGroupRequestRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /request", h.groupRequestPage)
	mux.HandleFunc("POST /request", h.sendGroupRequest)
	mux.HandleFunc("GET /admin/requests", h.groupRequestInbox)
	mux.HandleFunc("POST /admin/requests/{id}/resolve", h.resolveGroupRequest)
	mux.HandleFunc("POST /admin/requests/{id}/delete", h.deleteGroupRequest)
}

// trapField is the form's honeypot: a field people do not see, so whoever
// fills it in is a bot. Its name and label mean nothing to browsers'
// autofill, which would otherwise fill it in for people too.
const trapField = "extt_hp"

func (h *Handler) groupRequestPage(w http.ResponseWriter, r *http.Request) {
	d := pageData{Section: "request", Fields: map[string]string{}}
	if r.URL.Query().Get("sent") == "1" {
		d.Notice = i18n.FromContext(r.Context()).T("request.sent")
	}
	h.render(w, r, http.StatusOK, "request", d)
}

func (h *Handler) sendGroupRequest(w http.ResponseWriter, r *http.Request) {
	// A bot is told the request was sent, so it does not try again.
	if r.PostFormValue(trapField) != "" {
		slog.InfoContext(r.Context(), "group request dropped", "reason", "honeypot")
		http.Redirect(w, r, "/request?sent=1", http.StatusSeeOther)
		return
	}
	_, err := h.svc.RequestGroup(r.Context(), service.GroupRequestInput{
		University:      r.PostFormValue("university"),
		OtherUniversity: r.PostFormValue("other_university"),
		Group:           r.PostFormValue("group"),
		Telegram:        r.PostFormValue("telegram"),
		Comment:         r.PostFormValue("comment"),
		ClientIP:        auth.ClientIP(r, h.trustProxy),
	})
	if err != nil {
		msg, status, ok := userMessage(r, err)
		if !ok {
			h.renderError(w, r, err)
			return
		}
		fields := map[string]string{}
		for _, name := range []string{"university", "other_university", "group", "telegram", "comment"} {
			fields[name] = r.PostFormValue(name)
		}
		h.render(w, r, status, "request", pageData{Section: "request", Fields: fields, Error: msg})
		return
	}
	http.Redirect(w, r, "/request?sent=1", http.StatusSeeOther)
}

// groupRequestInbox lists the open group requests, or all of them with
// ?show=all.
func (h *Handler) groupRequestInbox(w http.ResponseWriter, r *http.Request) {
	all := r.URL.Query().Get("show") == "all"
	inbox, err := h.svc.GroupRequests(r.Context(), !all)
	if err != nil {
		h.renderError(w, r, err)
		return
	}
	h.render(w, r, http.StatusOK, "request_inbox", pageData{Section: "requests",
		GroupRequests: inbox.Items, GroupRequestsOpen: inbox.Open, ShowAll: all})
}

func (h *Handler) resolveGroupRequest(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err == nil {
		err = h.svc.ResolveGroupRequest(r.Context(), id, r.PostFormValue("resolved") == "1")
	}
	h.backToRequests(w, r, err)
}

func (h *Handler) deleteGroupRequest(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err == nil {
		err = h.svc.DeleteGroupRequest(r.Context(), id)
	}
	h.backToRequests(w, r, err)
}

// backToRequests returns to the list of requests the form was on, or shows
// err.
func (h *Handler) backToRequests(w http.ResponseWriter, r *http.Request, err error) {
	if err != nil {
		h.renderError(w, r, err)
		return
	}
	back := "/admin/requests"
	if r.PostFormValue("show") == "all" {
		back += "?show=all"
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}
