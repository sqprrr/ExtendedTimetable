package cist

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/text/encoding/charmap"
)

func kyiv(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Kyiv")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func cp1251(t *testing.T, s string) []byte {
	t.Helper()
	b, err := charmap.Windows1251.NewEncoder().Bytes([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// testdata/group_events.csv is a real export for КІУКІ-25-3, byte for byte.
func TestParseEventsRealExport(t *testing.T) {
	body, err := os.ReadFile("testdata/group_events.csv")
	if err != nil {
		t.Fatal(err)
	}
	loc := kyiv(t)
	events, err := ParseEvents(body, loc)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 9 {
		t.Fatalf("got %d events, want 9", len(events))
	}
	var lab *Event
	for i := range events {
		if events[i].Type == "Лб" {
			lab = &events[i]
			break
		}
	}
	if lab == nil {
		t.Fatal("no lab in the export")
	}
	want := Event{
		Start:   time.Date(2026, 10, 6, 14, 55, 0, 0, loc),
		End:     time.Date(2026, 10, 6, 16, 30, 0, 0, loc),
		Subject: "МОАП", Type: "Лб", Room: "DL", Groups: "КІУКІ-25-3",
	}
	if !lab.Start.Equal(want.Start) || !lab.End.Equal(want.End) || lab.Subject != want.Subject || lab.Room != want.Room || lab.Groups != want.Groups {
		t.Fatalf("lab = %+v\nwant %+v", *lab, want)
	}
	// 14:55 in Kyiv in October (EEST, UTC+3).
	if got := lab.Start.UTC().Format("15:04"); got != "11:55" {
		t.Errorf("start in UTC = %s, want 11:55", got)
	}
	for _, e := range events {
		if e.Subject == "" || e.Type == "" || e.Groups == "" {
			t.Errorf("incomplete event %+v", e)
		}
	}
}

func TestParseTitle(t *testing.T) {
	for _, tc := range []struct {
		title, subject, typ, room, groups string
	}{
		{"ООПро Лк DL КІУКІ-25-1,2,3,4,5", "ООПро", "Лк", "DL", "КІУКІ-25-1,2,3,4,5"},
		{"Філ Лк DL КІУКІ-25-1;ІСТ-25-1;КІУКІ-25-2,3", "Філ", "Лк", "DL", "КІУКІ-25-1;ІСТ-25-1;КІУКІ-25-2,3"},
		{"Ін мова Пз 287 КІУКІ-25-3", "Ін мова", "Пз", "287", "КІУКІ-25-3"},
		{"ВМ Конс і 203 КІУКІ-25-3", "ВМ", "Конс", "і 203", "КІУКІ-25-3"},
		{"ВМ Невідомий 285 КІУКІ-25-3", "ВМ", "Невідомий", "285", "КІУКІ-25-3"},
	} {
		s, typ, room, groups, err := parseTitle(tc.title)
		if err != nil || s != tc.subject || typ != tc.typ || room != tc.room || groups != tc.groups {
			t.Errorf("parseTitle(%q) = %q %q %q %q %v", tc.title, s, typ, room, groups, err)
		}
	}
	if _, _, _, _, err := parseTitle("ВМ Лк КІУКІ-25-3"); err == nil {
		t.Error("a title without a room should be rejected")
	}
}

func TestParseEventsRejectsOtherResponses(t *testing.T) {
	loc := kyiv(t)
	for name, body := range map[string]string{
		"html":     "<html><body>Помилка</body></html>",
		"empty":    "",
		"bad time": "\"Тема\",\"Дата начала\",\"Время начала\",\"Дата завершения\",\"Время завершения\"\r\"ВМ Лк DL КІУКІ-25-3\",\"05.10.2026\",\"9:30\",\"05.10.2026\",\"11:05:00\"\r",
	} {
		if _, err := ParseEvents(cp1251(t, body), loc); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	// A header alone is a valid, empty timetable.
	events, err := ParseEvents(cp1251(t, "\"Тема\",\"Дата начала\"\r"), loc)
	if err != nil || len(events) != 0 {
		t.Errorf("header only: %v %v", events, err)
	}
}

func TestClientFailsOverAndRemembersServer(t *testing.T) {
	body, err := os.ReadFile("testdata/group_events.csv")
	if err != nil {
		t.Fatal(err)
	}
	var downHits, upHits atomic.Int32
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		downHits.Add(1)
		http.Error(w, "down", http.StatusBadGateway)
	}))
	defer down.Close()
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upHits.Add(1)
		if r.URL.Query().Get("Aid_group") != "11881842" || r.URL.Query().Get("ADateStart") != "05.10.2026" {
			t.Errorf("unexpected query %s", r.URL.RawQuery)
		}
		w.Write(body)
	}))
	defer up.Close()

	c := New([]string{down.URL, up.URL}, 5*time.Second)
	loc := kyiv(t)
	from := time.Date(2026, 10, 5, 0, 0, 0, 0, loc)
	to := time.Date(2026, 10, 12, 0, 0, 0, 0, loc)
	for range 2 {
		events, err := c.GroupEvents(context.Background(), 11881842, from, to)
		if err != nil {
			t.Fatal(err)
		}
		// Only the events of that week are kept: one practice and two labs
		// (the lectures in the file are later in October).
		if len(events) != 3 {
			t.Fatalf("got %d events in the week, want 3: %+v", len(events), events)
		}
	}
	if downHits.Load() != 1 || upHits.Load() != 2 {
		t.Errorf("hits: down=%d up=%d; the working server should be tried first the second time", downHits.Load(), upHits.Load())
	}
}

func TestClientReportsCISTErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(cp1251(t, "ORA-20001: You are not authorized to use this resource\n"))
	}))
	defer srv.Close()
	_, err := New([]string{srv.URL}, time.Second).GroupEvents(context.Background(), 1, time.Now(), time.Now().Add(time.Hour))
	if err == nil || !strings.Contains(err.Error(), "ORA-20001: You are not authorized") {
		t.Fatalf("got %v", err)
	}
}

func TestFindGroups(t *testing.T) {
	const groups = `{"university":{"short_name":"ХНУРЕ","faculties":[{"id":1,"directions":[
		{"id":2,"groups":[{"id":11881842,"name":"КІУКІ-25-3"},{"id":5,"name":"ПЗПІ-25-1"}],
		 "specialities":[{"id":3,"groups":[{"id":11881842,"name":"КІУКІ-25-3"},{"id":11881844,"name":"КІУКІ-25-4"}]}]}]}]}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(cp1251(t, groups))
	}))
	defer srv.Close()
	got, err := New([]string{srv.URL}, time.Second).FindGroups(context.Background(), "кіукі-25")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != (Group{ID: 11881842, Name: "КІУКІ-25-3"}) || got[1].ID != 11881844 {
		t.Fatalf("got %+v", got)
	}
}
