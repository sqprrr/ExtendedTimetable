package auth

import (
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
		seen = CSRFToken(r.Context())
	}))

	// A GET issues the token cookie.
	rec := httptest.NewRecorder()
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
