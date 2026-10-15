package server_test

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sqprrr/ExtendedTimetable/internal/cist"
	"github.com/sqprrr/ExtendedTimetable/internal/server"
	"github.com/sqprrr/ExtendedTimetable/internal/store"
)

// TestPerfPages measures the main pages of a group with a term's worth of
// content. It is a measurement, not a check, so it runs only on request:
//
//	EXTT_PERF=1 go test ./internal/server -run TestPerfPages -v [-cpuprofile cpu.out]
func TestPerfPages(t *testing.T) {
	if os.Getenv("EXTT_PERF") == "" {
		t.Skip("set EXTT_PERF=1 to measure the pages")
	}
	now := time.Now().UTC()
	monday := now.AddDate(0, 0, -int(now.Weekday()+6)%7).Truncate(24 * time.Hour)
	src := &fakeCIST{}
	for d := range 5 {
		for c := range 4 {
			start := monday.AddDate(0, 0, d).Add(time.Duration(6+2*c) * time.Hour)
			src.events = append(src.events, cist.Event{Start: start, End: start.Add(95 * time.Minute),
				Subject: fmt.Sprintf("S%d", c), Type: "Лк", Room: "DL", Groups: "КІУКІ-25-3"})
		}
	}
	e := newScheduleEnv(t, src)
	lead := e.signUp(t, "lead", store.RoleLeader)
	stud := e.signUp(t, "stud", store.RoleStudent)
	const g = "/g/KIUKI-25-3"
	lead.submit(g+"/schedule", g+"/schedule/sync", url.Values{})

	_, body, _ := lead.get(g + "/homework/new")
	m := regexpOption("S0").FindStringSubmatch(body)
	if m == nil {
		t.Fatal("no subject S0")
	}
	md := strings.Repeat("Some **bold** text with a [link](https://example.org) and `code`.\n\n- item one\n- item two\n\n", 4)
	for i := range 30 {
		lead.submit(g+"/homework", g+"/homework", url.Values{"subject_id": {m[1]}, "title": {fmt.Sprintf("Lab %d", i)},
			"description": {md}, "max_points": {"10"}, "due_at": {now.AddDate(0, 0, i-5).Format("2006-01-02T15:04")}})
	}
	for i := range 20 {
		lead.submit(g+"/notes", g+"/notes", url.Values{"title": {fmt.Sprintf("Note %d", i)}, "body": {md}})
	}

	// Measure the pages, not writing the request log to a terminal.
	defer slog.SetDefault(slog.Default())
	slog.SetDefault(slog.New(slog.DiscardHandler))

	h, err := server.New(e.svc, server.Config{SecureCookies: true})
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(e.srv.URL)
	cookies := stud.c.Jar.Cookies(u)
	pages := []string{g, g + "/homework", g + "/notes", g + "/schedule", g + "/resources", "/api/v1/groups/KIUKI-25-3/homework"}
	get := func(page string) int {
		r := httptest.NewRequest(http.MethodGet, page, nil)
		for _, c := range cookies {
			r.AddCookie(c)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	for _, page := range pages {
		res := testing.Benchmark(func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if code := get(page); code != http.StatusOK {
					b.Fatalf("%s: %d", page, code)
				}
			}
		})
		t.Logf("%-40s %s %s", page, res, res.MemString())
	}

	// Many requests at once, some of them writing, must neither fail nor
	// wait on each other for good (the connections are few).
	var token string
	for _, c := range cookies {
		if strings.HasSuffix(c.Name, "extt_session") {
			token = c.Value
		}
	}
	start := time.Now()
	var wg sync.WaitGroup
	for i := range 16 {
		wg.Go(func() {
			for j := range 50 {
				if i%4 == 0 {
					ctx, err := e.svc.Authenticate(t.Context(), token)
					if err == nil {
						err = e.svc.SetTheme(ctx, []string{"light", "dark"}[j%2])
					}
					if err != nil {
						t.Errorf("write: %v", err)
						return
					}
					continue
				}
				if page := pages[(i+j)%len(pages)]; get(page) != http.StatusOK {
					t.Errorf("%s failed under load", page)
					return
				}
			}
		})
	}
	wg.Wait()
	t.Logf("%d requests from 16 goroutines in %v", 16*50, time.Since(start).Round(time.Millisecond))
}
