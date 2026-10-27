package server_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"testing"

	"github.com/sqprrr/ExtendedTimetable/internal/store"
)

const csrfCookie = "__Host-extt_csrf"

// pageTokenRe matches the CSRF token wherever a page embeds it: form fields
// and the htmx header on <body>.
var pageTokenRe = regexp.MustCompile(`name="csrf_token" value="([^"]+)"|"X-CSRF-Token": "([^"]+)"`)

// getWithoutCSRF fetches path with b's cookies except the CSRF cookie, as a
// browser does whose cookie is gone, and returns the response and body.
func (b *browser) getWithoutCSRF(path string) (*http.Response, string) {
	b.t.Helper()
	req, _ := http.NewRequest("GET", b.e.srv.URL+path, nil)
	u, _ := url.Parse(b.e.srv.URL)
	for _, c := range b.c.Jar.Cookies(u) {
		if c.Name != csrfCookie {
			req.AddCookie(c)
		}
	}
	c := *b.e.srv.Client()
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := c.Do(req)
	if err != nil {
		b.t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp, string(body)
}

func csrfSetCookies(resp *http.Response) []*http.Cookie {
	var out []*http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == csrfCookie {
			out = append(out, c)
		}
	}
	return out
}

// Every page that embeds the token sets the cookie it matches when the
// request had none, or the page's forms would all fail with "no CSRF
// cookie".
func TestPagesWithFormsSetTheirCSRFCookie(t *testing.T) {
	e := newEnv(t)
	const g = "/g/KIUKI-25-3"
	lead := e.signUp(t, "lead", store.RoleLeader)
	hwURL := setupHomework(t, e, lead)
	_, subjects, _ := lead.get(g + "/subjects")
	sm := regexp.MustCompile(`href="(` + g + `/subjects/\d+)"`).FindStringSubmatch(subjects)
	if sm == nil {
		t.Fatal("no subject link")
	}
	if err := e.svc.AdminCreateSuperadmin(context.Background(), "root", "correct horse"); err != nil {
		t.Fatal(err)
	}
	root := e.browser(t)
	root.submit("/login", "/login", url.Values{"username": {"root"}, "password": {"correct horse"}})

	pages := map[*browser][]string{
		e.browser(t): {"/login", "/request", e.joinPath(t, "KIUKI-25-3"), "/join/no-such-invite"},
		lead: {
			"/", "/more", "/feedback", g, g + "/more", g + "/schedule", g + "/members",
			hwURL, hwURL + "/edit", sm[1],
		},
		root: {"/admin/groups", "/admin/requests", "/admin/feedback"},
	}
	for _, s := range []string{"subjects", "links", "homework", "notes", "resources"} {
		pages[lead] = append(pages[lead], g+"/"+s, g+"/"+s+"/new")
	}
	for b, paths := range pages {
		for _, path := range paths {
			resp, body := b.getWithoutCSRF(path)
			set := csrfSetCookies(resp)
			if len(set) != 1 {
				t.Errorf("%s: %d: %d CSRF cookies set, want 1", path, resp.StatusCode, len(set))
				continue
			}
			found := pageTokenRe.FindAllStringSubmatch(body, -1)
			if len(found) == 0 {
				t.Errorf("%s: %d: no token on the page", path, resp.StatusCode)
			}
			for _, m := range found {
				if tok := m[1] + m[2]; tok != set[0].Value {
					t.Errorf("%s: page token %q, cookie %q", path, tok, set[0].Value)
				}
			}
		}
	}

	// The API hands the token to apps the same way.
	resp, body := lead.getWithoutCSRF("/api/v1/me")
	var me struct {
		CSRFToken string `json:"csrf_token"`
	}
	set := csrfSetCookies(resp)
	if json.Unmarshal([]byte(body), &me) != nil || len(set) != 1 || me.CSRFToken != set[0].Value {
		t.Errorf("/api/v1/me: token %q, CSRF cookies %+v", me.CSRFToken, set)
	}
}

// Responses that embed no token set no cookie: iOS fetches icons without
// the page's cookies and would keep a new one, breaking the open page's
// forms.
func TestResponsesWithoutTokenSetNoCSRFCookie(t *testing.T) {
	e := newEnv(t)
	b := e.browser(t)
	_, page, _ := b.get("/login")
	asset := regexp.MustCompile(`"(/static/site\.css\?v=[0-9a-f]+)"`).FindStringSubmatch(page)
	if asset == nil {
		t.Fatal("no versioned stylesheet on /login")
	}
	for _, path := range []string{
		asset[1], "/static/site.css", "/static/favicon.svg", "/favicon.ico", "/apple-touch-icon.png",
		"/healthz", "/no-such-page", "/xmlrpc.php", "/static/no-such-file.js",
	} {
		resp, _ := b.getWithoutCSRF(path)
		if set := csrfSetCookies(resp); len(set) != 0 {
			t.Errorf("%s: %d set a CSRF cookie", path, resp.StatusCode)
		}
	}
}
