package server_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/sqprrr/ExtendedTimetable/internal/auth"
	"github.com/sqprrr/ExtendedTimetable/internal/server"
	"github.com/sqprrr/ExtendedTimetable/internal/service"
	"github.com/sqprrr/ExtendedTimetable/internal/store"
	"github.com/sqprrr/ExtendedTimetable/migrations"
)

func TestMain(m *testing.M) {
	auth.BcryptCost = bcrypt.MinCost
	os.Exit(m.Run())
}

type env struct {
	srv    *httptest.Server
	svc    *service.Service
	invite string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(ctx, migrations.FS); err != nil {
		t.Fatal(err)
	}
	svc := service.New(st, service.Config{})
	g, err := svc.AdminCreateGroup(ctx, "KIUKI-25-3", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	h, err := server.New(svc, server.Config{SecureCookies: true})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewTLSServer(h)
	t.Cleanup(srv.Close)
	return &env{srv: srv, svc: svc, invite: g.InviteCode}
}

// browser is a client with a cookie jar that does not follow redirects.
type browser struct {
	t *testing.T
	c *http.Client
	e *env
}

func (e *env) browser(t *testing.T) *browser {
	jar, _ := cookiejar.New(nil)
	c := *e.srv.Client() // copy: srv.Client() is shared between callers
	c.Jar = jar
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &browser{t: t, c: &c, e: e}
}

var csrfRe = regexp.MustCompile(`name="csrf_token" value="([^"]+)"`)

func (b *browser) get(path string) (int, string, http.Header) {
	b.t.Helper()
	resp, err := b.c.Get(b.e.srv.URL + path)
	if err != nil {
		b.t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body), resp.Header
}

// submit loads formPage to obtain a CSRF token, then posts fields to action.
func (b *browser) submit(formPage, action string, fields url.Values) (int, string, http.Header) {
	b.t.Helper()
	_, page, _ := b.get(formPage)
	m := csrfRe.FindStringSubmatch(page)
	if m == nil {
		b.t.Fatalf("no CSRF token on %s", formPage)
	}
	fields.Set("csrf_token", m[1])
	return b.post(action, fields)
}

func (b *browser) post(action string, fields url.Values) (int, string, http.Header) {
	b.t.Helper()
	resp, err := b.c.PostForm(b.e.srv.URL+action, fields)
	if err != nil {
		b.t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body), resp.Header
}

func TestRegisterLoginLogoutFlow(t *testing.T) {
	e := newEnv(t)
	b := e.browser(t)

	// Anonymous users are sent to the login page.
	if code, _, h := b.get("/"); code != http.StatusSeeOther || h.Get("Location") != "/login" {
		t.Fatalf("GET / anonymous: %d %s", code, h.Get("Location"))
	}

	// Mismatched passwords keep the user on the form.
	code, body, _ := b.submit("/register", "/register", url.Values{
		"invite_code": {e.invite}, "username": {"alice"}, "password": {"correct horse"}, "password_confirm": {"different"},
	})
	if code != http.StatusUnprocessableEntity || !strings.Contains(body, "Passwords do not match") {
		t.Fatalf("mismatched passwords: %d", code)
	}

	code, _, h := b.submit("/register", "/register", url.Values{
		"invite_code": {strings.ToLower(e.invite)}, "username": {"alice"}, "password": {"correct horse"}, "password_confirm": {"correct horse"},
	})
	if code != http.StatusSeeOther || h.Get("Location") != "/" {
		t.Fatalf("register: %d %s", code, h.Get("Location"))
	}

	code, body, _ = b.get("/")
	if code != http.StatusOK || !strings.Contains(body, "Hi, alice") || !strings.Contains(body, "student") {
		t.Fatalf("home after register: %d\n%s", code, body)
	}
	if strings.Contains(body, e.invite) {
		t.Fatal("student must not see the invite code")
	}

	// Logged-in users skip the login form.
	if code, _, _ := b.get("/login"); code != http.StatusSeeOther {
		t.Fatalf("GET /login while signed in: %d", code)
	}

	code, _, h = b.submit("/", "/logout", url.Values{})
	if code != http.StatusSeeOther || h.Get("Location") != "/login" {
		t.Fatalf("logout: %d %s", code, h.Get("Location"))
	}
	if code, _, _ := b.get("/"); code != http.StatusSeeOther {
		t.Fatalf("GET / after logout: %d", code)
	}

	code, body, _ = b.submit("/login", "/login", url.Values{"username": {"alice"}, "password": {"wrong password"}})
	if code != http.StatusUnprocessableEntity || !strings.Contains(body, "Invalid username or password") {
		t.Fatalf("bad login: %d", code)
	}
	if !strings.Contains(body, `value="alice"`) {
		t.Fatal("username should be kept in the form after a failed login")
	}

	code, _, _ = b.submit("/login", "/login", url.Values{"username": {"alice"}, "password": {"correct horse"}})
	if code != http.StatusSeeOther {
		t.Fatalf("login: %d", code)
	}
	if code, body, _ := b.get("/"); code != http.StatusOK || !strings.Contains(body, "Hi, alice") {
		t.Fatalf("home after login: %d", code)
	}
}

func TestPostWithoutCSRFTokenIsRejected(t *testing.T) {
	e := newEnv(t)
	b := e.browser(t)
	b.get("/login") // obtain the CSRF cookie, but do not send the token
	code, _, _ := b.post("/login", url.Values{"username": {"x"}, "password": {"y"}})
	if code != http.StatusForbidden {
		t.Fatalf("got %d, want 403", code)
	}
}

func TestLeaderRegeneratesInviteCode(t *testing.T) {
	e := newEnv(t)
	leader := e.browser(t)
	leader.submit("/register", "/register", url.Values{
		"invite_code": {e.invite}, "username": {"lead"}, "password": {"correct horse"}, "password_confirm": {"correct horse"},
	})
	student := e.browser(t)
	student.submit("/register", "/register", url.Values{
		"invite_code": {e.invite}, "username": {"stud"}, "password": {"correct horse"}, "password_confirm": {"correct horse"},
	})
	if err := e.svc.AdminSetRole(context.Background(), "lead", "KIUKI-25-3", store.RoleLeader); err != nil {
		t.Fatal(err)
	}

	_, body, _ := leader.get("/")
	if !strings.Contains(body, e.invite) {
		t.Fatal("leader should see the invite code")
	}
	action := regexp.MustCompile(`action="(/groups/\d+/invite-code)"`).FindStringSubmatch(body)
	if action == nil {
		t.Fatal("no regenerate form on leader's home page")
	}

	if code, _, _ := student.submit("/", action[1], url.Values{}); code != http.StatusForbidden {
		t.Fatalf("student regenerate: %d, want 403", code)
	}
	if code, _, _ := leader.submit("/", action[1], url.Values{}); code != http.StatusSeeOther {
		t.Fatalf("leader regenerate: %d", code)
	}
	if _, body, _ := leader.get("/"); strings.Contains(body, e.invite) {
		t.Fatal("invite code did not change")
	}
}

func TestAPIMe(t *testing.T) {
	e := newEnv(t)
	b := e.browser(t)

	resp, err := b.c.Get(e.srv.URL + "/api/v1/me")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous /api/v1/me: %d", resp.StatusCode)
	}

	b.submit("/register", "/register", url.Values{
		"invite_code": {e.invite}, "username": {"api"}, "password": {"correct horse"}, "password_confirm": {"correct horse"},
	})
	resp, err = b.c.Get(e.srv.URL + "/api/v1/me")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var me struct {
		Username  string `json:"username"`
		CSRFToken string `json:"csrf_token"`
		Groups    []struct {
			Code string `json:"code"`
			Role string `json:"role"`
		} `json:"groups"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&me); err != nil {
		t.Fatal(err)
	}
	if me.Username != "api" || me.CSRFToken == "" || len(me.Groups) != 1 || me.Groups[0].Role != "student" {
		t.Fatalf("unexpected /api/v1/me: %+v", me)
	}
}

func TestSecurityHeadersAndCookies(t *testing.T) {
	e := newEnv(t)
	b := e.browser(t)
	resp, err := b.c.Get(e.srv.URL + "/login")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if csp := resp.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'self'") {
		t.Errorf("CSP = %q", csp)
	}
	for _, c := range resp.Cookies() {
		if !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode || !strings.HasPrefix(c.Name, "__Host-") {
			t.Errorf("cookie %s: HttpOnly=%v Secure=%v SameSite=%v", c.Name, c.HttpOnly, c.Secure, c.SameSite)
		}
	}
	if code, _, _ := b.get("/static/style.css"); code != http.StatusOK {
		t.Errorf("static asset: %d", code)
	}
}
