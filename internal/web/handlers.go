package web

import (
	"net/http"
	"strconv"

	"github.com/sqprrr/ExtendedTimetable/internal/auth"
	"github.com/sqprrr/ExtendedTimetable/internal/service"
)

func (h *Handler) home(w http.ResponseWriter, r *http.Request) {
	if service.ViewerFrom(r.Context()) == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	groups, err := h.svc.MyGroups(r.Context())
	if err != nil {
		h.renderError(w, r, err)
		return
	}
	h.render(w, r, http.StatusOK, "home", pageData{Groups: groups})
}

func (h *Handler) loginForm(w http.ResponseWriter, r *http.Request) {
	if service.ViewerFrom(r.Context()) != nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	h.render(w, r, http.StatusOK, "login", pageData{})
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	form := formValues{Username: r.PostFormValue("username")}
	sess, err := h.svc.Login(r.Context(), service.LoginInput{
		Username: form.Username,
		Password: r.PostFormValue("password"),
		ClientIP: auth.ClientIP(r, h.trustProxy),
	})
	if err != nil {
		h.formError(w, r, "login", form, err)
		return
	}
	h.startSession(w, r, sess)
}

func (h *Handler) registerForm(w http.ResponseWriter, r *http.Request) {
	if service.ViewerFrom(r.Context()) != nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	// Allow sharing a prefilled link: /register?code=XXXX-XXXX-XXXX
	h.render(w, r, http.StatusOK, "register", pageData{Form: formValues{InviteCode: r.URL.Query().Get("code")}})
}

func (h *Handler) register(w http.ResponseWriter, r *http.Request) {
	form := formValues{Username: r.PostFormValue("username"), InviteCode: r.PostFormValue("invite_code")}
	password := r.PostFormValue("password")
	if password != r.PostFormValue("password_confirm") {
		h.render(w, r, http.StatusUnprocessableEntity, "register", pageData{Form: form, Error: "Passwords do not match."})
		return
	}
	sess, err := h.svc.Register(r.Context(), service.RegisterInput{
		Username:   form.Username,
		Password:   password,
		InviteCode: form.InviteCode,
		ClientIP:   auth.ClientIP(r, h.trustProxy),
	})
	if err != nil {
		h.formError(w, r, "register", form, err)
		return
	}
	h.startSession(w, r, sess)
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Logout(r.Context(), h.cookies.SessionToken(r)); err != nil {
		h.renderError(w, r, err)
		return
	}
	h.cookies.ClearSession(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (h *Handler) regenerateInviteCode(w http.ResponseWriter, r *http.Request) {
	groupID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		h.renderError(w, r, service.ErrNotFound)
		return
	}
	if _, err := h.svc.RegenerateInviteCode(r.Context(), groupID); err != nil {
		h.renderError(w, r, err)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *Handler) startSession(w http.ResponseWriter, r *http.Request, sess *service.NewSession) {
	// Drop any session this browser already had so it does not linger.
	if old := h.cookies.SessionToken(r); old != "" {
		_ = h.svc.Logout(r.Context(), old)
	}
	h.cookies.SetSession(w, sess.Token, sess.ExpiresAt)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *Handler) formError(w http.ResponseWriter, r *http.Request, page string, form formValues, err error) {
	msg, status, ok := userMessage(err)
	if !ok {
		h.renderError(w, r, err)
		return
	}
	h.render(w, r, status, page, pageData{Form: form, Error: msg})
}
