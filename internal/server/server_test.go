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
	srv *httptest.Server
	svc *service.Service
	st  *store.Store
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
	if _, err := svc.AdminCreateGroup(ctx, "KIUKI-25-3", "", nil); err != nil {
		t.Fatal(err)
	}
	h, err := server.New(svc, server.Config{SecureCookies: true})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewTLSServer(h)
	t.Cleanup(srv.Close)
	return &env{srv: srv, svc: svc, st: st}
}

// browser is a client with a cookie jar that does not follow redirects.
type browser struct {
	t *testing.T
	c *http.Client
	e *env
}

// browser returns a browser that has switched the site to English, which
// the tests check pages against.
func (e *env) browser(t *testing.T) *browser { return e.browserIn(t, "en") }

// browserIn returns a browser without a language cookie when lang is "".
func (e *env) browserIn(t *testing.T, lang string) *browser {
	jar, _ := cookiejar.New(nil)
	c := *e.srv.Client() // copy: srv.Client() is shared between callers
	c.Jar = jar
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	b := &browser{t: t, c: &c, e: e}
	if lang != "" {
		u, _ := url.Parse(e.srv.URL)
		jar.SetCookies(u, []*http.Cookie{{Name: "__Host-extt_lang", Value: lang, Path: "/", Secure: true}})
	}
	return b
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

	// The only group is offered and preselected.
	_, body, _ := b.get("/register")
	if !strings.Contains(body, `<option value="KIUKI-25-3" selected>`) {
		t.Fatalf("register form should preselect the only group:\n%s", body)
	}

	// Mismatched passwords keep the user on the form.
	code, body, _ := b.submit("/register", "/register", url.Values{
		"group": {"KIUKI-25-3"}, "username": {"alice"}, "password": {"correct horse"}, "password_confirm": {"different"},
	})
	if code != http.StatusUnprocessableEntity || !strings.Contains(body, "Passwords do not match") {
		t.Fatalf("mismatched passwords: %d", code)
	}
	if !strings.Contains(body, `<option value="KIUKI-25-3" selected>`) || !strings.Contains(body, `value="alice"`) {
		t.Fatal("group and username should be kept in the form after an error")
	}

	code, _, h := b.submit("/register", "/register", url.Values{
		"group": {"kiuki-25-3"}, "username": {"alice"}, "password": {"correct horse"}, "password_confirm": {"correct horse"},
	})
	if code != http.StatusSeeOther || h.Get("Location") != "/" {
		t.Fatalf("register: %d %s", code, h.Get("Location"))
	}

	code, body, _ = b.get("/")
	if code != http.StatusOK || !strings.Contains(body, "Hi, alice") || !strings.Contains(body, "student") {
		t.Fatalf("home after register: %d\n%s", code, body)
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

func TestRegisterIntoChosenGroup(t *testing.T) {
	e := newEnv(t)
	if _, err := e.svc.AdminCreateGroup(context.Background(), "OTHER-1", "Other group", nil); err != nil {
		t.Fatal(err)
	}
	b := e.browser(t)

	// With several groups nothing is preselected unless the link asks for one.
	_, body, _ := b.get("/register")
	if strings.Contains(body, " selected>") || !strings.Contains(body, `<option value="">`) {
		t.Fatalf("no group should be preselected:\n%s", body)
	}
	_, body, _ = b.get("/register?group=other-1")
	if !strings.Contains(body, `<option value="OTHER-1" selected>Other group (OTHER-1)</option>`) {
		t.Fatalf("?group= should preselect the group:\n%s", body)
	}

	code, body, _ := b.submit("/register", "/register", url.Values{
		"group": {""}, "username": {"bob"}, "password": {"correct horse"}, "password_confirm": {"correct horse"},
	})
	if code != http.StatusUnprocessableEntity || !strings.Contains(body, "Choose your group") {
		t.Fatalf("missing group: %d", code)
	}

	code, _, _ = b.submit("/register", "/register", url.Values{
		"group": {"OTHER-1"}, "username": {"bob"}, "password": {"correct horse"}, "password_confirm": {"correct horse"},
	})
	if code != http.StatusSeeOther {
		t.Fatalf("register: %d", code)
	}
	if _, body, _ := b.get("/"); !strings.Contains(body, "Other group") || strings.Contains(body, "KIUKI-25-3") {
		t.Fatalf("home should show only the chosen group:\n%s", body)
	}
}

func TestRegisterWithoutGroups(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(ctx, migrations.FS); err != nil {
		t.Fatal(err)
	}
	h, err := server.New(service.New(st, service.Config{}), server.Config{SecureCookies: true})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewTLSServer(h)
	t.Cleanup(srv.Close)
	b := (&env{srv: srv}).browser(t)
	if _, body, _ := b.get("/register"); !strings.Contains(body, "no groups have been created") || strings.Contains(body, `name="group"`) {
		t.Fatalf("register without groups:\n%s", body)
	}
}

func TestLeaderIsAppointedOnlyByAdmin(t *testing.T) {
	e := newEnv(t)
	b := e.browser(t)
	b.submit("/register", "/register", url.Values{
		"group": {"KIUKI-25-3"}, "username": {"lead"}, "password": {"correct horse"}, "password_confirm": {"correct horse"},
	})
	if _, body, _ := b.get("/"); !strings.Contains(body, "<strong>student</strong>") {
		t.Fatal("self-registered users must start as students")
	}

	if err := e.svc.AdminSetRole(context.Background(), "lead", "KIUKI-25-3", store.RoleLeader); err != nil {
		t.Fatal(err)
	}
	if _, body, _ := b.get("/"); !strings.Contains(body, "<strong>leader</strong>") {
		t.Fatal("promoted user should be a leader")
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
		"group": {"KIUKI-25-3"}, "username": {"api"}, "password": {"correct horse"}, "password_confirm": {"correct horse"},
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
