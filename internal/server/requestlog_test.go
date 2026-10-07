package server_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/sqprrr/ExtendedTimetable/internal/logging"
	"github.com/sqprrr/ExtendedTimetable/internal/server"
	"github.com/sqprrr/ExtendedTimetable/internal/store"
)

// logBuffer collects JSON log lines; the server logs from its own goroutines.
type logBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *logBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// records returns the logged lines with message msg.
func (b *logBuffer) records(t *testing.T, msg string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(b.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("log line is not JSON: %q", line)
		}
		if m["msg"] == msg {
			out = append(out, m)
		}
	}
	return out
}

// captureLogs sends the default logger to a buffer for the rest of the test.
func captureLogs(t *testing.T, level slog.Level) *logBuffer {
	t.Helper()
	b := &logBuffer{}
	log, err := logging.New(b, level, "json")
	if err != nil {
		t.Fatal(err)
	}
	old := slog.Default()
	slog.SetDefault(log)
	t.Cleanup(func() { slog.SetDefault(old) })
	return b
}

func TestRequestLog(t *testing.T) {
	e := newEnv(t)
	b := e.signUp(t, "alice", store.RoleStudent)
	logs := captureLogs(t, slog.LevelInfo)

	resp, err := b.c.Get(e.srv.URL + "/g/KIUKI-25-3/homework?secret=1")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	id := resp.Header.Get("X-Request-ID")
	if !logging.ValidRequestID(id) {
		t.Fatalf("X-Request-ID = %q", id)
	}
	b.get("/static/style.css") // debug level: not logged at info

	recs := logs.records(t, "request")
	if len(recs) != 1 {
		t.Fatalf("got %d request lines, want 1:\n%s", len(recs), logs.String())
	}
	r := recs[0]
	if r["level"] != "INFO" || r["method"] != "GET" || r["path"] != "/g/KIUKI-25-3/homework" || r["status"] != float64(200) ||
		r["user"] != "alice" || r["request_id"] != id {
		t.Fatalf("request line: %v", r)
	}
	if _, ok := r["duration_ms"]; !ok {
		t.Error("no duration_ms")
	}
	if strings.Contains(logs.String(), "secret") {
		t.Error("the query string must not be logged")
	}
}

func TestRequestLogLevels(t *testing.T) {
	e := newEnv(t)
	logs := captureLogs(t, slog.LevelDebug)
	b := e.browser(t)
	b.get("/static/style.css")
	b.get("/nope")
	byPath := map[string]map[string]any{}
	for _, r := range logs.records(t, "request") {
		byPath[r["path"].(string)] = r
	}
	if r := byPath["/static/style.css"]; r == nil || r["level"] != "DEBUG" || r["user"] != nil {
		t.Errorf("static file: %v", r)
	}
	if r := byPath["/nope"]; r == nil || r["level"] != "INFO" || r["status"] != float64(404) {
		t.Errorf("not found: %v", r)
	}
}

func TestServerErrorsCarryRequestID(t *testing.T) {
	e := newEnv(t)
	b := e.signUp(t, "alice", store.RoleStudent)
	logs := captureLogs(t, slog.LevelInfo)
	e.st.Close() // every query fails from now on

	resp, err := b.c.Get(e.srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	id := resp.Header.Get("X-Request-ID")
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status %d", resp.StatusCode)
	}
	errs := logs.records(t, "load session")
	reqs := logs.records(t, "request")
	if len(errs) != 1 || errs[0]["request_id"] != id || errs[0]["err"] == nil {
		t.Fatalf("error line: %v\n%s", errs, logs.String())
	}
	if len(reqs) != 1 || reqs[0]["level"] != "ERROR" || reqs[0]["status"] != float64(500) || reqs[0]["request_id"] != id {
		t.Fatalf("request line: %v", reqs)
	}
}

func TestRequestIDFromProxy(t *testing.T) {
	e := newEnv(t)
	get := func(trustProxy bool, sent string) string {
		h, err := server.New(e.svc, server.Config{SecureCookies: true, TrustProxy: trustProxy})
		if err != nil {
			t.Fatal(err)
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/healthz", nil)
		if sent != "" {
			req.Header.Set("X-Request-ID", sent)
		}
		h.ServeHTTP(rec, req)
		return rec.Header().Get("X-Request-ID")
	}
	if got := get(true, "nginx-abc.123"); got != "nginx-abc.123" {
		t.Errorf("behind the proxy its ID should be kept, got %q", got)
	}
	if got := get(false, "nginx-abc.123"); got == "nginx-abc.123" || !logging.ValidRequestID(got) {
		t.Errorf("without a trusted proxy the client's ID must be ignored, got %q", got)
	}
	if got := get(true, "evil\" level=ERROR"); strings.Contains(got, "evil") {
		t.Errorf("an unsafe ID must be replaced, got %q", got)
	}
}

func TestSecurityEventsAreLogged(t *testing.T) {
	e := newEnv(t)
	e.signUp(t, "alice", store.RoleStudent)
	logs := captureLogs(t, slog.LevelInfo)
	b := e.browser(t)

	// CSRF: cookie present, token missing.
	b.get("/login")
	if code, _, _ := b.post("/login", url.Values{"username": {"alice"}, "password": {"x"}}); code != http.StatusForbidden {
		t.Fatalf("no CSRF token: %d", code)
	}
	csrf := logs.records(t, "CSRF check failed")
	if len(csrf) != 1 || csrf[0]["level"] != "WARN" || csrf[0]["reason"] != "no token sent" || csrf[0]["request_id"] == nil {
		t.Fatalf("CSRF line: %v", csrf)
	}

	// Failed logins: a real account is named; what was typed for an unknown
	// one is not, since it may be a password.
	b.submit("/login", "/login", url.Values{"username": {"alice"}, "password": {"wrong-pass-1"}})
	b.submit("/login", "/login", url.Values{"username": {"hunter2-typed-in-the-wrong-box"}, "password": {"wrong-pass-2"}})
	failed := logs.records(t, "login failed")
	if len(failed) != 2 || failed[0]["username"] != "alice" || failed[1]["username"] != nil || failed[1]["reason"] != "unknown username" {
		t.Fatalf("failed logins: %v", failed)
	}
	code, _, _ := b.submit("/login", "/login", url.Values{"username": {"alice"}, "password": {"correct horse"}})
	if code != http.StatusSeeOther {
		t.Fatalf("login: %d", code)
	}
	if ok := logs.records(t, "login"); len(ok) != 1 || ok[0]["username"] != "alice" {
		t.Fatalf("login line: %v", ok)
	}

	out := logs.String()
	for _, secret := range []string{"wrong-pass-1", "wrong-pass-2", "correct horse", "hunter2"} {
		if strings.Contains(out, secret) {
			t.Errorf("%q leaked into the logs", secret)
		}
	}
	for _, c := range b.c.Jar.Cookies(mustURL(e.srv.URL)) {
		if strings.Contains(out, c.Value) {
			t.Errorf("cookie %s leaked into the logs", c.Name)
		}
	}
}

func mustURL(s string) *url.URL {
	u, err := url.Parse(s)
	if err != nil {
		panic(err)
	}
	return u
}
