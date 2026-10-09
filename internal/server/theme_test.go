package server_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/sqprrr/ExtendedTimetable/internal/store"
)

// htmlTag returns the page's <html …> start tag.
func htmlTag(body string) string {
	i := strings.Index(body, "<html")
	if i < 0 {
		return ""
	}
	return body[i : i+strings.Index(body[i:], ">")+1]
}

func TestThemeFollowsTheDeviceByDefault(t *testing.T) {
	e := newEnv(t)
	b := e.browser(t)
	_, body, _ := b.get("/login")
	if tag := htmlTag(body); strings.Contains(tag, "data-theme") {
		t.Fatalf("no choice should leave the theme to the device: %s", tag)
	}
	// The switch offers all three, the device's pressed.
	for _, want := range []string{
		`name="theme" value="light" aria-pressed="false"`,
		`name="theme" value="dark" aria-pressed="false"`,
		`name="theme" value="system" aria-pressed="true"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("switch: missing %s", want)
		}
	}
}

func TestThemeSwitchForVisitors(t *testing.T) {
	e := newEnv(t)
	b := e.browser(t)

	code, _, h := b.submit("/login", "/theme", url.Values{"theme": {"dark"}, "back": {"/register"}})
	if code != http.StatusSeeOther || h.Get("Location") != "/register" {
		t.Fatalf("switch: %d %s", code, h.Get("Location"))
	}
	_, body, _ := b.get("/login")
	if tag := htmlTag(body); !strings.Contains(tag, `data-theme="dark"`) {
		t.Fatalf("the cookie should switch the site to dark: %s", tag)
	}
	if !strings.Contains(body, `value="dark" aria-pressed="true"`) {
		t.Error("the switch should show dark as chosen")
	}

	// "system" hands the choice back to the device.
	b.submit("/login", "/theme", url.Values{"theme": {"system"}, "back": {"/login"}})
	if _, body, _ := b.get("/login"); strings.Contains(htmlTag(body), "data-theme") {
		t.Fatal("system should drop data-theme")
	}

	// Unknown themes fall back to the device's; the switch only returns to
	// pages on this site.
	code, _, h = b.submit("/login", "/theme", url.Values{"theme": {"neon"}, "back": {"https://evil.example/"}})
	if code != http.StatusSeeOther || h.Get("Location") != "/" {
		t.Fatalf("bad back: %d %s", code, h.Get("Location"))
	}
	if _, body, _ := b.get("/login"); strings.Contains(htmlTag(body), "data-theme") {
		t.Fatal("an unknown theme should fall back to the device's")
	}
}

func TestThemeIsKeptInTheAccount(t *testing.T) {
	e := newEnv(t)

	// Chosen before registering: kept in the account.
	b := e.browser(t)
	b.submit("/login", "/theme", url.Values{"theme": {"light"}, "back": {"/login"}})
	b.register("KIUKI-25-3", "alice")
	other := e.browser(t)
	other.submit("/login", "/login", url.Values{"username": {"alice"}, "password": {"correct horse"}})
	if _, body, _ := other.get("/"); !strings.Contains(htmlTag(body), `data-theme="light"`) {
		t.Fatal("the account's theme should follow the user to another browser")
	}

	// Switching while signed in updates the account, and the account wins
	// over another browser's cookie.
	other.submit("/", "/theme", url.Values{"theme": {"dark"}, "back": {"/"}})
	third := e.browser(t)
	third.submit("/login", "/theme", url.Values{"theme": {"light"}, "back": {"/login"}})
	third.submit("/login", "/login", url.Values{"username": {"alice"}, "password": {"correct horse"}})
	if _, body, _ := third.get("/"); !strings.Contains(htmlTag(body), `data-theme="dark"`) {
		t.Fatal("the account's theme should win over the browser cookie")
	}
	// After logging out the browser keeps the account's theme.
	third.submit("/", "/logout", url.Values{})
	if _, body, _ := third.get("/login"); !strings.Contains(htmlTag(body), `data-theme="dark"`) {
		t.Fatal("login should store the account's theme in the cookie")
	}

	// "system" is a choice too: it wins over a browser's dark cookie.
	other.submit("/", "/theme", url.Values{"theme": {"system"}, "back": {"/"}})
	fourth := e.browser(t)
	fourth.submit("/login", "/theme", url.Values{"theme": {"dark"}, "back": {"/login"}})
	fourth.submit("/login", "/login", url.Values{"username": {"alice"}, "password": {"correct horse"}})
	if _, body, _ := fourth.get("/"); strings.Contains(htmlTag(body), "data-theme") {
		t.Fatal("an account set to system should follow the device")
	}
}

func TestAPITheme(t *testing.T) {
	e := newEnv(t)
	b := e.signUp(t, "api", store.RoleStudent)
	tok := b.csrf()
	me := func(method, body string) (int, map[string]any) {
		t.Helper()
		req, _ := http.NewRequest(method, e.srv.URL+"/api/v1/me", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-CSRF-Token", tok)
		resp, err := b.c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}
	if code, out := me(http.MethodGet, ""); code != http.StatusOK || out["theme"] != "system" {
		t.Fatalf("GET: %d %v", code, out)
	}
	if code, out := me(http.MethodPut, `{"theme": "dark"}`); code != http.StatusOK || out["theme"] != "dark" {
		t.Fatalf("PUT dark: %d %v", code, out)
	}
	if _, body, _ := b.get("/"); !strings.Contains(htmlTag(body), `data-theme="dark"`) {
		t.Fatal("the API's choice should apply to the pages")
	}
	if code, out := me(http.MethodPut, `{"theme": "neon"}`); code != http.StatusUnprocessableEntity || out["code"] != "err.theme" {
		t.Fatalf("PUT bad theme: %d %v", code, out)
	}
}

// The shell: every page of a signed-in user links to the group's sections,
// the current one marked, and the phone's More page holds the rest.
func TestShellNavigation(t *testing.T) {
	e := newEnv(t)
	stud := e.signUp(t, "stud", store.RoleStudent)

	_, body, _ := stud.get("/g/KIUKI-25-3/schedule")
	for _, want := range []string{
		`<a class="nav-item" href="/g/KIUKI-25-3/schedule" aria-current="page">`,
		`<a class="nav-item" href="/g/KIUKI-25-3/homework">`,
		`<a href="/g/KIUKI-25-3/schedule" aria-current="page">`, // bottom bar
		`<a href="/g/KIUKI-25-3/more">`,
		`action="/logout"`,
		`action="/theme"`,
		`action="/lang"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("schedule page: missing %s", want)
		}
	}
	if strings.Contains(body, `class="tabs"`) {
		t.Error("the old section tabs should be gone")
	}

	// Pages outside a group lead to the user's group.
	if _, body, _ := stud.get("/feedback"); !strings.Contains(body, `href="/g/KIUKI-25-3/homework"`) {
		t.Error("feedback page: the navigation should lead to the user's group")
	}

	code, body, _ := stud.get("/g/KIUKI-25-3/more")
	if code != http.StatusOK {
		t.Fatalf("more: %d", code)
	}
	for _, want := range []string{`href="/g/KIUKI-25-3/notes"`, `href="/g/KIUKI-25-3/subjects"`, `href="/feedback"`, `<a href="/g/KIUKI-25-3/more" aria-current="page">`} {
		if !strings.Contains(body, want) {
			t.Errorf("more page: missing %s", want)
		}
	}
	// Navigation loads pages in place with htmx and a skeleton meanwhile;
	// the settings forms and the htmx history cache stay out of it.
	_, body, _ = stud.get("/g/KIUKI-25-3/schedule")
	for _, want := range []string{
		`<aside class="sidebar" hx-boost="true" hx-indicator="#main">`,
		`<nav class="bottom-nav" aria-label="Group sections" hx-boost="true" hx-indicator="#main">`,
		`<main class="page page-schedule" id="main" tabindex="-1">`,
		`<div class="page-skeleton" aria-hidden="true">`,
		`action="/theme" class="theme-switch" hx-boost="false"`,
		`action="/logout" hx-boost="false"`,
		`"historyCacheSize": 0`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("skeleton loading: missing %s", want)
		}
	}
	if strings.Contains(body, `action="/lang" class="inline">`) {
		t.Error("the language switch must not be boosted")
	}

	if code, _, h := e.browser(t).get("/more"); code != http.StatusSeeOther || h.Get("Location") != "/login" {
		t.Fatalf("anonymous more: %d %s", code, h.Get("Location"))
	}
}
