package web

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/sqprrr/ExtendedTimetable/internal/auth"
	"github.com/sqprrr/ExtendedTimetable/internal/i18n"
	"github.com/sqprrr/ExtendedTimetable/internal/service"
)

// registerMemberRoutes adds the invite pages, the members page with its
// leader and member actions, and the superadmins' group panel.
func (h *Handler) registerMemberRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /join/{token}", h.joinPage)
	mux.HandleFunc("POST /join/{token}", h.join)
	mux.HandleFunc("POST /join/{token}/register", h.joinRegister)
	mux.HandleFunc("POST /join/{token}/login", h.joinLogin)

	mux.HandleFunc("GET /g/{code}/members", h.membersPage)
	mux.HandleFunc("POST /g/{code}/invite", h.groupAction(func(ctx context.Context, h *Handler, g *service.GroupView, _ *http.Request) error {
		_, err := h.svc.RegenerateInvite(ctx, g.ID)
		return err
	}))
	mux.HandleFunc("POST /g/{code}/leader/claim", h.groupAction(func(ctx context.Context, h *Handler, g *service.GroupView, _ *http.Request) error {
		return h.svc.ClaimLeadership(ctx, g.ID)
	}))
	mux.HandleFunc("POST /g/{code}/leader/resign", h.groupAction(func(ctx context.Context, h *Handler, g *service.GroupView, _ *http.Request) error {
		return h.svc.ResignLeadership(ctx, g.ID)
	}))
	mux.HandleFunc("POST /g/{code}/leader/remove", h.groupAction(func(ctx context.Context, h *Handler, g *service.GroupView, _ *http.Request) error {
		return h.svc.RemoveLeader(ctx, g.ID)
	}))
	mux.HandleFunc("POST /g/{code}/leader", h.groupAction(func(ctx context.Context, h *Handler, g *service.GroupView, r *http.Request) error {
		id, err := strconv.ParseInt(r.PostFormValue("user_id"), 10, 64)
		if err != nil {
			return service.ErrNotFound
		}
		return h.svc.SetLeader(ctx, g.ID, id)
	}))
	mux.HandleFunc("POST /g/{code}/members/{id}/remove", h.groupAction(func(ctx context.Context, h *Handler, g *service.GroupView, r *http.Request) error {
		id, err := pathID(r)
		if err != nil {
			return err
		}
		return h.svc.RemoveMember(ctx, g.ID, id)
	}))
	mux.HandleFunc("POST /g/{code}/leave", h.leaveGroup)

	mux.HandleFunc("GET /admin/groups", h.adminGroups)
	mux.HandleFunc("POST /admin/groups", h.createGroup)
}

// baseURL is the site's address for links shown to be shared: the
// configured one, or else the one this request came to.
func (h *Handler) baseURL(r *http.Request) string {
	if h.base != "" {
		return h.base
	}
	scheme := "http"
	if r.TLS != nil || h.cookies.Secure {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func (h *Handler) joinPage(w http.ResponseWriter, r *http.Request) {
	h.renderJoin(w, r, http.StatusOK, formValues{}, "")
}

// renderJoin renders the invite page: the forms to register or log in, or
// for a signed-in user the button to join. A member of the group is sent to
// it; a dead link shows why.
func (h *Handler) renderJoin(w http.ResponseWriter, r *http.Request, status int, form formValues, errMsg string) {
	token := r.PathValue("token")
	inv, err := h.svc.Invite(r.Context(), token)
	if errors.Is(err, service.ErrInviteInvalid) {
		h.render(w, r, http.StatusNotFound, "join", pageData{})
		return
	} else if err != nil {
		h.renderError(w, r, err)
		return
	}
	if inv.Member {
		http.Redirect(w, r, "/g/"+inv.Group.Code, http.StatusSeeOther)
		return
	}
	h.render(w, r, status, "join", pageData{JoinInvite: inv, Token: token, Form: form, Error: errMsg})
}

func (h *Handler) join(w http.ResponseWriter, r *http.Request) {
	g, err := h.svc.JoinGroup(r.Context(), r.PathValue("token"))
	if err != nil {
		h.joinError(w, r, formValues{}, err)
		return
	}
	http.Redirect(w, r, "/g/"+g.Code, http.StatusSeeOther)
}

func (h *Handler) joinRegister(w http.ResponseWriter, r *http.Request) {
	form := formValues{Username: r.PostFormValue("username")}
	password := r.PostFormValue("password")
	if password != r.PostFormValue("password_confirm") {
		h.renderJoin(w, r, http.StatusUnprocessableEntity, form, i18n.FromContext(r.Context()).T("err.password_mismatch"))
		return
	}
	sess, err := h.svc.Register(r.Context(), service.RegisterInput{
		Username:    form.Username,
		Password:    password,
		InviteToken: r.PathValue("token"),
		ClientIP:    auth.ClientIP(r, h.trustProxy),
		// Keep the language the visitor picked with the switch, if any.
		Locale: h.cookies.Lang(r),
	})
	if err != nil {
		h.joinError(w, r, form, err)
		return
	}
	h.startSession(w, r, sess, "/")
}

// joinLogin signs in and joins the group in one step. If the user cannot
// join (they are in another group), they stay signed in and the invite page
// says why.
func (h *Handler) joinLogin(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	form := formValues{Username: r.PostFormValue("login_username")}
	sess, err := h.svc.Login(r.Context(), service.LoginInput{
		Username: form.Username,
		Password: r.PostFormValue("login_password"),
		ClientIP: auth.ClientIP(r, h.trustProxy),
	})
	if err != nil {
		h.joinError(w, r, form, err)
		return
	}
	ctx, err := h.svc.Authenticate(r.Context(), sess.Token)
	if err != nil {
		h.renderError(w, r, err)
		return
	}
	to := "/join/" + token
	if g, err := h.svc.JoinGroup(ctx, token); err == nil {
		to = "/g/" + g.Code
	}
	h.startSession(w, r, sess, to)
}

// joinError shows the invite page again with the error.
func (h *Handler) joinError(w http.ResponseWriter, r *http.Request, form formValues, err error) {
	msg, status, ok := userMessage(r, err)
	if !ok {
		h.renderError(w, r, err)
		return
	}
	h.renderJoin(w, r, status, form, msg)
}

func (h *Handler) membersPage(w http.ResponseWriter, r *http.Request) {
	if g := h.groupPage(w, r); g != nil {
		h.showMembers(w, r, g, http.StatusOK, "")
	}
}

// showMembers renders the members page: the leader, the invite link for
// those who manage the group, the members and, for superadmins, the log.
func (h *Handler) showMembers(w http.ResponseWriter, r *http.Request, g *service.GroupView, status int, errMsg string) {
	ctx := r.Context()
	d := pageData{Group: g, Section: "members", Error: errMsg, BaseURL: h.baseURL(r)}
	var err error
	if d.Members, err = h.svc.Members(ctx, g.ID); err != nil {
		h.renderError(w, r, err)
		return
	}
	if g.CanManage {
		if d.Invite, err = h.svc.GroupInvite(ctx, g.ID); err != nil {
			h.renderError(w, r, err)
			return
		}
	}
	if g.IsSuperadmin {
		if d.GroupLog, err = h.svc.GroupLog(ctx, g.ID); err != nil {
			h.renderError(w, r, err)
			return
		}
	}
	h.render(w, r, status, "members", d)
}

// groupAction runs a members-page action and returns to the members page,
// or shows it with the error the user can act on.
func (h *Handler) groupAction(act func(ctx context.Context, h *Handler, g *service.GroupView, r *http.Request) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		g := h.groupPage(w, r)
		if g == nil {
			return
		}
		if err := act(r.Context(), h, g, r); err != nil {
			h.membersError(w, r, err)
			return
		}
		http.Redirect(w, r, sectionURL(g, "members"), http.StatusSeeOther)
	}
}

// membersError shows the members page with err, reloading the group since
// the viewer's role may have changed.
func (h *Handler) membersError(w http.ResponseWriter, r *http.Request, err error) {
	msg, status, ok := userMessage(r, err)
	if !ok {
		h.renderError(w, r, err)
		return
	}
	if g := h.groupPage(w, r); g != nil {
		h.showMembers(w, r, g, status, msg)
	}
}

// leaveGroup takes the viewer out of the group and back to the home page.
func (h *Handler) leaveGroup(w http.ResponseWriter, r *http.Request) {
	g := h.groupPage(w, r)
	if g == nil {
		return
	}
	if err := h.svc.LeaveGroup(r.Context(), g.ID); err != nil {
		h.membersError(w, r, err)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *Handler) adminGroups(w http.ResponseWriter, r *http.Request) {
	h.showAdminGroups(w, r, http.StatusOK, pageData{})
}

func (h *Handler) showAdminGroups(w http.ResponseWriter, r *http.Request, status int, d pageData) {
	var err error
	if d.AdminGroups, err = h.svc.AdminGroups(r.Context()); err != nil {
		h.renderError(w, r, err)
		return
	}
	if d.Fields == nil {
		d.Fields = map[string]string{}
	}
	h.render(w, r, status, "admin_groups", d)
}

// createGroup creates a group from the panel and opens its members page,
// which shows the new invite link.
func (h *Handler) createGroup(w http.ResponseWriter, r *http.Request) {
	in := service.CreateGroupInput{Code: r.PostFormValue("code"), Name: r.PostFormValue("name")}
	var err error
	if raw := strings.TrimSpace(r.PostFormValue("cist_id")); raw != "" {
		id, perr := strconv.ParseInt(raw, 10, 64)
		if perr != nil {
			err = &service.InputError{Field: "cist_id", Msg: i18n.M("err.cist_id")}
		}
		in.CISTGroupID = &id
	}
	if err == nil {
		g, cerr := h.svc.CreateGroup(r.Context(), in)
		if cerr == nil {
			http.Redirect(w, r, "/g/"+g.Code+"/members", http.StatusSeeOther)
			return
		}
		err = cerr
	}
	msg, status, ok := userMessage(r, err)
	if !ok {
		h.renderError(w, r, err)
		return
	}
	fields := map[string]string{
		"code": r.PostFormValue("code"), "name": r.PostFormValue("name"), "cist_id": r.PostFormValue("cist_id"),
	}
	h.showAdminGroups(w, r, status, pageData{Fields: fields, Error: msg})
}
