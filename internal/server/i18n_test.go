package server_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestUkrainianByDefault(t *testing.T) {
	e := newEnv(t)
	b := e.browserIn(t, "")

	code, body, h := b.get("/login")
	if code != http.StatusOK || !strings.Contains(body, `<html lang="uk">`) || !strings.Contains(body, "<h1>Вхід</h1>") {
		t.Fatalf("login page should be Ukrainian by default: %d\n%s", code, body)
	}
	if h.Get("Content-Language") != "uk" {
		t.Fatalf("Content-Language: %q", h.Get("Content-Language"))
	}
	// Only the other language is offered.
	if !strings.Contains(body, `value="en"`) || strings.Contains(body, `name="lang" value="uk"`) {
		t.Fatal("the switch should offer English")
	}

	// Errors from the service are translated too.
	join := e.joinPath(t, "KIUKI-25-3")
	code, body, _ = b.submit(join, join+"/register", url.Values{
		"username": {"ab"}, "password": {"correct horse"}, "password_confirm": {"correct horse"},
	})
	if code != http.StatusUnprocessableEntity || !strings.Contains(body, "Ім’я користувача має містити 3–32 символи.") {
		t.Fatalf("Ukrainian validation error: %d\n%s", code, body)
	}
	code, body, _ = b.submit(join, join+"/register", url.Values{
		"username": {"bob"}, "password": {"short"}, "password_confirm": {"short"},
	})
	if code != http.StatusUnprocessableEntity || !strings.Contains(body, "щонайменше 8 символів") {
		t.Fatalf("Ukrainian plural form: %d\n%s", code, body)
	}
}

func TestLanguageSwitchForVisitors(t *testing.T) {
	e := newEnv(t)
	b := e.browserIn(t, "")

	code, _, h := b.submit("/login", "/lang", url.Values{"lang": {"en"}, "back": {"/login?x=1"}})
	if code != http.StatusSeeOther || h.Get("Location") != "/login?x=1" {
		t.Fatalf("switch: %d %s", code, h.Get("Location"))
	}
	if _, body, _ := b.get("/login"); !strings.Contains(body, `<html lang="en">`) || !strings.Contains(body, "<h1>Log in</h1>") {
		t.Fatal("the cookie should switch the site to English")
	}

	// The switch only returns to pages on this site.
	for _, back := range []string{"https://evil.example/", "//evil.example/", `/\evil.example`, ""} {
		code, _, h := b.submit("/login", "/lang", url.Values{"lang": {"uk"}, "back": {back}})
		if code != http.StatusSeeOther || h.Get("Location") != "/" {
			t.Fatalf("back=%q: %d %s", back, code, h.Get("Location"))
		}
	}

	// An unknown language falls back to Ukrainian.
	b.submit("/login", "/lang", url.Values{"lang": {"de"}, "back": {"/login"}})
	if _, body, _ := b.get("/login"); !strings.Contains(body, `<html lang="uk">`) {
		t.Fatal("unknown language should fall back to Ukrainian")
	}
}

func TestLanguageIsKeptInTheAccount(t *testing.T) {
	e := newEnv(t)

	// Registering with English chosen keeps it in the account.
	b := e.browser(t)
	b.register("KIUKI-25-3", "alice")
	other := e.browserIn(t, "")
	other.submit("/login", "/login", url.Values{"username": {"alice"}, "password": {"correct horse"}})
	if _, body, _ := other.get("/"); !strings.Contains(body, "Hi, alice") {
		t.Fatal("the account's language should follow the user to another browser")
	}

	// Switching while signed in updates the account.
	other.submit("/", "/lang", url.Values{"lang": {"uk"}, "back": {"/"}})
	third := e.browser(t) // English cookie, but the account says Ukrainian
	third.submit("/login", "/login", url.Values{"username": {"alice"}, "password": {"correct horse"}})
	if _, body, _ := third.get("/"); !strings.Contains(body, "Привіт, alice") || !strings.Contains(body, "<strong>студент</strong>") {
		t.Fatal("the account's language should win over the browser cookie")
	}
	// After logging out the browser stays in the account's language.
	third.submit("/", "/logout", url.Values{})
	if _, body, _ := third.get("/login"); !strings.Contains(body, "<h1>Вхід</h1>") {
		t.Fatal("login should store the account's language in the cookie")
	}

	// A user who never chose follows the browser.
	d := e.browserIn(t, "")
	d.register("KIUKI-25-3", "dana")
	en := e.browser(t)
	en.submit("/login", "/login", url.Values{"username": {"dana"}, "password": {"correct horse"}})
	if _, body, _ := en.get("/"); !strings.Contains(body, "Hi, dana") {
		t.Fatal("a user without a chosen language should see the browser's language")
	}
}

func TestAPILocale(t *testing.T) {
	e := newEnv(t)
	b := e.browserIn(t, "")
	b.register("KIUKI-25-3", "api")
	me := func(method, body string) (int, map[string]any) {
		t.Helper()
		_, page, _ := b.get("/")
		token := csrfRe.FindStringSubmatch(page)[1]
		req, _ := http.NewRequest(method, e.srv.URL+"/api/v1/me", bytes.NewBufferString(body))
		req.Header.Set("X-CSRF-Token", token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := b.c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}
	if code, out := me(http.MethodGet, ""); code != http.StatusOK || out["locale"] != "uk" {
		t.Fatalf("GET me: %d %v", code, out)
	}
	if code, out := me(http.MethodPut, `{"locale": "en"}`); code != http.StatusOK || out["locale"] != "en" {
		t.Fatalf("PUT me: %d %v", code, out)
	}
	if _, body, _ := b.get("/"); !strings.Contains(body, "Hi, api") {
		t.Fatal("the API should change the account's language")
	}
	// API errors are English, with the message ID as a code.
	if code, out := me(http.MethodPut, `{"locale": "de"}`); code != http.StatusUnprocessableEntity ||
		out["code"] != "err.locale" || out["field"] != "locale" || !strings.HasPrefix(out["error"].(string), "Choose Ukrainian") {
		t.Fatalf("PUT bad locale: %d %v", code, out)
	}
}
