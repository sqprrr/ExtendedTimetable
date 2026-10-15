package server_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/text/encoding/charmap"

	"github.com/sqprrr/ExtendedTimetable/internal/cist"
	"github.com/sqprrr/ExtendedTimetable/internal/service"
	"github.com/sqprrr/ExtendedTimetable/internal/store"
)

// cistExport is a CIST timetable export (windows-1251, CR line ends) with
// one lecture tomorrow, or with the header alone when empty.
func cistExport(t *testing.T, empty bool) []byte {
	t.Helper()
	kyiv, err := time.LoadLocation("Europe/Kyiv")
	if err != nil {
		t.Fatal(err)
	}
	text := `"Тема","Дата начала","Время начала","Дата завершения","Время завершения"` + "\r"
	if !empty {
		day := time.Now().In(kyiv).AddDate(0, 0, 1).Format("02.01.2006")
		text += `"ОБД Лк DL КІУКІ-25-3","` + day + `","13:10:00","` + day + `","14:45:00"` + "\r"
	}
	b, err := charmap.Windows1251.NewEncoder().Bytes([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// The site keeps working, from the cached copy, whatever CIST does: every
// page loads, shows the classes synced before and says the sync failed.
func TestSiteSurvivesCISTOutage(t *testing.T) {
	outages := map[string]http.HandlerFunc{
		"no answer":    func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() },
		"server error": func(w http.ResponseWriter, r *http.Request) { http.Error(w, "down", http.StatusServiceUnavailable) },
		"html page": func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("<html>Технічні роботи</html>"))
		},
		"oracle error":  func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ORA-12541: TNS:no listener\n")) },
		"empty export":  func(w http.ResponseWriter, r *http.Request) { w.Write(cistExport(t, true)) },
		"dropped conn":  func(w http.ResponseWriter, r *http.Request) { c, _, _ := w.(http.Hijacker).Hijack(); c.Close() },
		"truncated csv": func(w http.ResponseWriter, r *http.Request) { w.Write(cistExport(t, false)[:120]) },
	}
	for name, outage := range outages {
		t.Run(name, func(t *testing.T) {
			var down atomic.Bool
			good := cistExport(t, false)
			fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if down.Load() {
					outage(w, r)
					return
				}
				w.Write(good)
			}))
			defer fake.Close()
			e := newEnvWith(t, service.Config{CIST: cist.New([]string{fake.URL, fake.URL}, 300*time.Millisecond)})
			cistID := int64(1)
			if err := e.svc.AdminSetCISTID(context.Background(), "KIUKI-25-3", &cistID); err != nil {
				t.Fatal(err)
			}
			stud := e.signUp(t, "stud", store.RoleStudent)
			if rec, err := e.svc.AdminSyncSchedule(context.Background(), "KIUKI-25-3", false); err != nil || rec.EventCount != 1 {
				t.Fatalf("first sync: %v %+v", err, rec)
			}

			down.Store(true)
			if _, err := e.svc.AdminSyncSchedule(context.Background(), "KIUKI-25-3", false); err == nil {
				t.Fatal("the sync should fail")
			}
			tomorrow := time.Now().AddDate(0, 0, 1).Format(time.DateOnly)
			for _, path := range []string{"/", "/g/KIUKI-25-3", "/g/KIUKI-25-3/schedule?week=" + tomorrow, "/g/KIUKI-25-3/homework"} {
				if code, _, _ := stud.get(path); code != http.StatusOK {
					t.Errorf("%s: %d", path, code)
				}
			}
			_, body, _ := stud.get("/g/KIUKI-25-3/schedule?week=" + tomorrow)
			for _, want := range []string{"ОБД", "alert-warning", "Showing the last good copy"} {
				if !strings.Contains(body, want) {
					t.Errorf("schedule page missing %q", want)
				}
			}
			if strings.Contains(body, "/ias/app/") {
				t.Error("the error on the page should not carry CIST's URL")
			}
			code, body, _ := stud.get("/api/v1/groups/KIUKI-25-3/schedule?from=" + tomorrow)
			if code != http.StatusOK || !strings.Contains(body, `"subject_brief":"ОБД"`) || !strings.Contains(body, `"last_error":"`) {
				t.Errorf("API schedule: %d %s", code, body)
			}
		})
	}
}

// A leader who syncs while CIST is down goes back to the schedule, which
// says the sync failed; it is not an error page.
func TestLeaderSyncDuringCISTOutage(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusBadGateway)
	}))
	defer fake.Close()
	e := newEnvWith(t, service.Config{CIST: cist.New([]string{fake.URL}, time.Second)})
	cistID := int64(1)
	if err := e.svc.AdminSetCISTID(context.Background(), "KIUKI-25-3", &cistID); err != nil {
		t.Fatal(err)
	}
	lead := e.signUp(t, "lead", store.RoleLeader)
	const g = "/g/KIUKI-25-3"
	code, _, h := lead.submit(g+"/schedule", g+"/schedule/sync", url.Values{})
	if code != http.StatusSeeOther || h.Get("Location") != g+"/schedule" {
		t.Fatalf("sync: %d %s", code, h.Get("Location"))
	}
	if code, body, _ := lead.get(g + "/schedule"); code != http.StatusOK || !strings.Contains(body, "HTTP 502") {
		t.Fatalf("schedule after a failed sync: %d", code)
	}
}
