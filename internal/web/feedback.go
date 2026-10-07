package web

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/sqprrr/ExtendedTimetable/internal/i18n"
	"github.com/sqprrr/ExtendedTimetable/internal/service"
	"github.com/sqprrr/ExtendedTimetable/internal/store"
)

// registerFeedbackRoutes adds the feedback form and the superadmins' inbox.
func (h *Handler) registerFeedbackRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /feedback", h.feedbackPage)
	mux.HandleFunc("POST /feedback", h.sendFeedback)
	mux.HandleFunc("GET /admin/feedback", h.feedbackInbox)
	mux.HandleFunc("POST /admin/feedback/{id}/resolve", h.resolveFeedback)
	mux.HandleFunc("POST /admin/feedback/{id}/delete", h.deleteFeedback)
}

func (h *Handler) feedbackPage(w http.ResponseWriter, r *http.Request) {
	d := pageData{}
	if r.URL.Query().Get("sent") == "1" {
		d.Notice = i18n.FromContext(r.Context()).T("feedback.thanks")
	}
	h.showFeedback(w, r, http.StatusOK, d)
}

// showFeedback renders the form with what the viewer has sent before.
func (h *Handler) showFeedback(w http.ResponseWriter, r *http.Request, status int, d pageData) {
	var err error
	if d.Feedback, err = h.svc.MyFeedback(r.Context()); err != nil {
		h.renderError(w, r, err)
		return
	}
	if v := service.ViewerFrom(r.Context()); v.IsSuperadmin {
		if d.FeedbackOpen, err = h.svc.OpenFeedbackCount(r.Context()); err != nil {
			h.renderError(w, r, err)
			return
		}
	}
	if d.Fields == nil {
		d.Fields = map[string]string{}
	}
	h.render(w, r, status, "feedback", d)
}

func (h *Handler) sendFeedback(w http.ResponseWriter, r *http.Request) {
	in := service.FeedbackInput{
		Kind:    store.FeedbackKind(r.PostFormValue("kind")),
		Message: r.PostFormValue("message"),
	}
	var err error
	if raw := strings.TrimSpace(r.PostFormValue("rating")); raw != "" {
		n, perr := strconv.ParseInt(raw, 10, 64)
		if perr != nil {
			err = &service.InputError{Field: "rating", Msg: i18n.M("err.rating")}
		}
		in.Rating = &n
	}
	if err == nil {
		_, err = h.svc.SendFeedback(r.Context(), in)
	}
	if err != nil {
		msg, status, ok := userMessage(r, err)
		if !ok {
			h.renderError(w, r, err)
			return
		}
		fields := map[string]string{
			"kind": r.PostFormValue("kind"), "rating": r.PostFormValue("rating"), "message": r.PostFormValue("message"),
		}
		h.showFeedback(w, r, status, pageData{Fields: fields, Error: msg})
		return
	}
	http.Redirect(w, r, "/feedback?sent=1", http.StatusSeeOther)
}

// feedbackInbox lists the open feedback, or all of it with ?show=all.
func (h *Handler) feedbackInbox(w http.ResponseWriter, r *http.Request) {
	all := r.URL.Query().Get("show") == "all"
	inbox, err := h.svc.Inbox(r.Context(), !all)
	if err != nil {
		h.renderError(w, r, err)
		return
	}
	h.render(w, r, http.StatusOK, "feedback_inbox", pageData{Feedback: inbox.Items, FeedbackOpen: inbox.Open, ShowAll: all})
}

func (h *Handler) resolveFeedback(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err == nil {
		err = h.svc.ResolveFeedback(r.Context(), id, r.PostFormValue("resolved") == "1")
	}
	h.backToInbox(w, r, err)
}

func (h *Handler) deleteFeedback(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err == nil {
		err = h.svc.DeleteFeedback(r.Context(), id)
	}
	h.backToInbox(w, r, err)
}

// backToInbox returns to the inbox view the form was on, or shows err.
func (h *Handler) backToInbox(w http.ResponseWriter, r *http.Request, err error) {
	if err != nil {
		h.renderError(w, r, err)
		return
	}
	back := "/admin/feedback"
	if r.PostFormValue("show") == "all" {
		back += "?show=all"
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}
