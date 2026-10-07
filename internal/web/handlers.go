package web

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/sqprrr/ExtendedTimetable/internal/auth"
	"github.com/sqprrr/ExtendedTimetable/internal/i18n"
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
	// Allow sharing a link with the group preselected: /register?group=KIUKI-25-3
	h.renderRegister(w, r, http.StatusOK, formValues{Group: r.URL.Query().Get("group")}, "")
}

func (h *Handler) register(w http.ResponseWriter, r *http.Request) {
	form := formValues{Username: r.PostFormValue("username"), Group: r.PostFormValue("group")}
	password := r.PostFormValue("password")
	if password != r.PostFormValue("password_confirm") {
		h.renderRegister(w, r, http.StatusUnprocessableEntity, form, i18n.FromContext(r.Context()).T("err.password_mismatch"))
		return
	}
	sess, err := h.svc.Register(r.Context(), service.RegisterInput{
		Username:  form.Username,
		Password:  password,
		GroupCode: form.Group,
		ClientIP:  auth.ClientIP(r, h.trustProxy),
		// Keep the language the visitor picked with the switch, if any.
		Locale: h.cookies.Lang(r),
	})
	if err != nil {
		msg, status, ok := userMessage(r, err)
		if !ok {
			h.renderError(w, r, err)
			return
		}
		h.renderRegister(w, r, status, form, msg)
		return
	}
	h.startSession(w, r, sess)
}

// renderRegister renders the registration form with the group picker. With a
// single group, it is preselected.
func (h *Handler) renderRegister(w http.ResponseWriter, r *http.Request, status int, form formValues, errMsg string) {
	groups, err := h.svc.JoinableGroups(r.Context())
	if err != nil {
		h.renderError(w, r, err)
		return
	}
	form.Group = strings.ToUpper(strings.TrimSpace(form.Group))
	if form.Group == "" && len(groups) == 1 {
		form.Group = groups[0].Code
	}
	h.render(w, r, status, "register", pageData{Form: form, Error: errMsg, JoinableGroups: groups})
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Logout(r.Context(), h.cookies.SessionToken(r)); err != nil {
		h.renderError(w, r, err)
		return
	}
	h.cookies.ClearSession(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (h *Handler) startSession(w http.ResponseWriter, r *http.Request, sess *service.NewSession) {
	// Drop any session this browser already had so it does not linger.
	if old := h.cookies.SessionToken(r); old != "" {
		_ = h.svc.Logout(r.Context(), old)
	}
	h.cookies.SetSession(w, sess.Token, sess.ExpiresAt)
	// The browser keeps showing the user's language after they log out.
	if sess.Locale != "" {
		h.cookies.SetLang(w, sess.Locale)
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// setLang switches the language: in a cookie for this browser and, for a
// signed-in user, in their account.
func (h *Handler) setLang(w http.ResponseWriter, r *http.Request) {
	lang := r.PostFormValue("lang")
	if !i18n.IsSupported(lang) {
		lang = i18n.Default
	}
	if service.ViewerFrom(r.Context()) != nil {
		if err := h.svc.SetLocale(r.Context(), lang); err != nil {
			h.renderError(w, r, err)
			return
		}
	}
	h.cookies.SetLang(w, lang)
	back := r.PostFormValue("back")
	if !isLocalPath(back) {
		back = "/"
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}

// backPath is where the language switch on this page returns to: the page
// itself, or for a page rendered by a form post, the page the form was on.
func backPath(r *http.Request) string {
	if r.Method == http.MethodGet {
		return r.URL.RequestURI()
	}
	if ref, err := url.Parse(r.Referer()); err == nil && ref.Host == r.Host && isLocalPath(ref.RequestURI()) {
		return ref.RequestURI()
	}
	return "/"
}

// isLocalPath reports whether p is a path on this site, so redirecting to it
// cannot send the user elsewhere ("//evil.example" or "/\\evil.example").
func isLocalPath(p string) bool {
	return strings.HasPrefix(p, "/") && !strings.HasPrefix(p, "//") && !strings.HasPrefix(p, "/\\")
}

func (h *Handler) formError(w http.ResponseWriter, r *http.Request, page string, form formValues, err error) {
	msg, status, ok := userMessage(r, err)
	if !ok {
		h.renderError(w, r, err)
		return
	}
	h.render(w, r, status, page, pageData{Form: form, Error: msg})
}
