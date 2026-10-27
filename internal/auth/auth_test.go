package auth

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestPasswordHashing(t *testing.T) {
	BcryptCost = 4
	h, err := HashPassword("hunter22")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword(h, "hunter22") || CheckPassword(h, "hunter23") {
		t.Fatal("CheckPassword mismatch")
	}
	if _, err := HashPassword(strings.Repeat("x", 73)); err != ErrPasswordTooLong {
		t.Fatalf("got %v, want ErrPasswordTooLong", err)
	}
}

func TestLimiter(t *testing.T) {
	now := time.Unix(0, 0)
	l := NewLimiter(2, time.Minute)
	l.now = func() time.Time { return now }

	if !l.Allow("k") || !l.Allow("k") || l.Allow("k") {
		t.Fatal("expected 2 allowed then denied")
	}
	if !l.Allow("other") {
		t.Fatal("keys must be independent")
	}
	if n := l.Len(); n != 2 {
		t.Fatalf("Len = %d, want 2", n)
	}
	now = now.Add(time.Minute)
	if !l.Allow("k") {
		t.Fatal("window should have reset")
	}
	l.Reset("k")
	l.Prune()
	if len(l.buckets) != 0 {
		t.Fatalf("expected all buckets pruned, have %d", len(l.buckets))
	}
}

func TestCSRF(t *testing.T) {
	c := Cookies{Secure: true}
	var seen string
	h := c.CSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/icon.png" {
			seen = CSRFToken(r.Context())
		}
	}))

	// A response that does not embed the token (an icon) sets no cookie, so
	// it cannot replace the one an open page's token matches.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/icon.png", nil))
	if cookies := rec.Result().Cookies(); len(cookies) != 0 {
		t.Fatalf("icon set cookies: %+v", cookies)
	}

	// A page issues the token cookie.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != "__Host-extt_csrf" || !cookies[0].HttpOnly || !cookies[0].Secure {
		t.Fatalf("unexpected cookies: %+v", cookies)
	}
	token := cookies[0].Value
	if seen != token {
		t.Fatalf("context token %q != cookie %q", seen, token)
	}

	post := func(formToken, header string, withCookie bool) int {
		form := url.Values{CSRFField: {formToken}}
		req := httptest.NewRequest("POST", "/", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if header != "" {
			req.Header.Set(CSRFHeader, header)
		}
		if withCookie {
			req.AddCookie(&http.Cookie{Name: c.CSRFName(), Value: token})
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	if code := post(token, "", true); code != http.StatusOK {
		t.Errorf("valid form token: %d", code)
	}
	if code := post("", token, true); code != http.StatusOK {
		t.Errorf("valid header token: %d", code)
	}
	if code := post("wrong", "", true); code != http.StatusForbidden {
		t.Errorf("wrong token: %d", code)
	}
	if code := post(token, "", false); code != http.StatusForbidden {
		t.Errorf("missing cookie: %d", code)
	}

	// Cross-site requests are rejected even with a valid token.
	form := url.Values{CSRFField: {token}}
	req := httptest.NewRequest("POST", "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	req.AddCookie(&http.Cookie{Name: c.CSRFName(), Value: token})
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("cross-site: %d", rec.Code)
	}
}

func TestClientIP(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.1:1234"
	r.Header.Set("X-Real-IP", "203.0.113.7")
	if got := ClientIP(r, false); got != "10.0.0.1" {
		t.Errorf("untrusted: %q", got)
	}
	if got := ClientIP(r, true); got != "203.0.113.7" {
		t.Errorf("trusted: %q", got)
	}
}

func TestClientNetwork(t *testing.T) {
	for _, c := range []struct{ ip, want string }{
		{"203.0.113.7", "203.0.113.7"},
		{"::ffff:203.0.113.7", "203.0.113.7"},
		{"2001:db8:1:2:3:4:5:6", "2001:db8:1:2::/64"},
		{"2001:db8:1:2::ffff", "2001:db8:1:2::/64"},
		{"2001:db8:1:3::1", "2001:db8:1:3::/64"},
		{"fe80::1%eth0", "fe80::/64"},
		{"not an address", "not an address"},
		{"", ""},
	} {
		if got := ClientNetwork(c.ip); got != c.want {
			t.Errorf("ClientNetwork(%q) = %q, want %q", c.ip, got, c.want)
		}
	}
}

// A new token asked for after the response is written has lost its cookie:
// tests panic, production logs an error.
func TestCSRFTokenAfterWrite(t *testing.T) {
	c := Cookies{Secure: true}
	late := c.CSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("<form>"))
		CSRFToken(r.Context())
	}))
	func() {
		defer func() {
			if recover() == nil {
				t.Error("a late new token did not panic under go test")
			}
		}()
		late.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	}()

	lateCSRFPanics = false
	t.Cleanup(func() { lateCSRFPanics = true })
	var logged bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	late.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	if !strings.Contains(logged.String(), "level=ERROR") {
		t.Errorf("a late new token logged no error: %q", logged.String())
	}

	// Asking early and again after writing is fine, as is asking late when
	// the request had the cookie: there is nothing to set.
	for _, withCookie := range []bool{false, true} {
		rec := httptest.NewRecorder()
		c.CSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			first := ""
			if !withCookie {
				first = CSRFToken(r.Context())
			}
			w.WriteHeader(http.StatusOK)
			if again := CSRFToken(r.Context()); !withCookie && again != first {
				t.Errorf("token changed: %q, then %q", first, again)
			}
		})).ServeHTTP(rec, func() *http.Request {
			req := httptest.NewRequest("GET", "/", nil)
			if withCookie {
				req.AddCookie(&http.Cookie{Name: c.CSRFName(), Value: strings.Repeat("a", 43)})
			}
			return req
		}())
		if n := len(rec.Result().Cookies()); n != map[bool]int{false: 1, true: 0}[withCookie] {
			t.Errorf("with cookie %v: %d cookies set", withCookie, n)
		}
	}
}
