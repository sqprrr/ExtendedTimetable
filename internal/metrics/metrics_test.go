package metrics_test

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/sqprrr/ExtendedTimetable/internal/metrics"
	"github.com/sqprrr/ExtendedTimetable/internal/store"
	"github.com/sqprrr/ExtendedTimetable/migrations"
)

func TestObserveRequestLabels(t *testing.T) {
	for _, tc := range []struct {
		method, pattern  string
		wantM, wantRoute string
	}{
		{"GET", "GET /g/{code}/homework", "GET", "/g/{code}/homework"},
		{"HEAD", "GET /login", "HEAD", "/login"},
		{"GET", "GET /{$}", "GET", "/"},
		{"POST", "/api/", "POST", "/api/"},
		{"GET", "", "GET", "unmatched"},
		{"BREW", "", "OTHER", "unmatched"},
	} {
		metrics.ObserveRequest(tc.method, tc.pattern, 200, time.Millisecond)
		want := `extt_http_requests_total{code="200",method="` + tc.wantM + `",route="` + tc.wantRoute + `"}`
		if got := scrape(t, metrics.Registry); !strings.Contains(got, want) {
			t.Errorf("%s %q: no series %s", tc.method, tc.pattern, want)
		}
	}
}

func TestEverySeriesExistsFromTheStart(t *testing.T) {
	got := scrape(t, metrics.Registry)
	for _, want := range []string{
		`extt_logins_total{result="rate_limited"} `,
		`extt_registrations_total{result="success"} `,
		`extt_csrf_rejections_total{reason="cross_origin"} `,
		`extt_schedule_syncs_total{result="failure"} `,
		`extt_http_panics_total `,
		`go_goroutines `,
		`process_resident_memory_bytes `,
		`go_build_info{`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s", want)
		}
	}
}

func TestStoreCollector(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(ctx, migrations.FS); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	cist := int64(11881842)
	g := &store.Group{Code: "KIUKI-25-3", CISTGroupID: &cist, CreatedAt: now}
	if err := st.CreateGroup(ctx, g); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateGroup(ctx, &store.Group{Code: "NO-CIST", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	u := &store.User{Username: "alice", PasswordHash: "x", CreatedAt: now}
	if err := st.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	for i, exp := range []time.Time{now.Add(time.Hour), now.Add(2 * time.Hour), now.Add(-time.Hour)} {
		s := &store.Session{ID: string(rune('a' + i)), UserID: u.ID, ExpiresAt: exp, CreatedAt: now}
		if err := st.CreateSession(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	success := now.Add(-time.Hour)
	if err := st.SaveScheduleSync(ctx, &store.ScheduleSync{GroupID: g.ID, LastAttemptAt: now, LastSuccessAt: &success, EventCount: 42}); err != nil {
		t.Fatal(err)
	}

	reg := prometheus.NewRegistry()
	reg.MustRegister(metrics.NewStoreCollector(st, func() time.Time { return now }))
	got := scrape(t, reg)
	for _, want := range []string{
		"extt_db_stats_up 1\n",
		"extt_users 1\n",
		"extt_groups 2\n",
		"extt_sessions 2\n",
		"extt_signed_in_users 1\n",
		"extt_homework 0\n",
		"extt_feedback_open 0\n",
		`extt_schedule_events{group="KIUKI-25-3"} 42` + "\n",
		`extt_schedule_last_attempt_timestamp_seconds{group="KIUKI-25-3"} 1.7914608e+09` + "\n",
		`extt_schedule_last_success_timestamp_seconds{group="KIUKI-25-3"} 1.7914572e+09` + "\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if strings.Contains(got, "NO-CIST") {
		t.Error("a group not linked to CIST should have no sync series")
	}
	if !strings.Contains(got, "extt_db_size_bytes ") || strings.Contains(got, "extt_db_size_bytes 0\n") {
		t.Error("the database size should be reported")
	}

	st.Close()
	if got := scrape(t, reg); !strings.Contains(got, "extt_db_stats_up 0\n") {
		t.Errorf("a failing database should report extt_db_stats_up 0:\n%s", got)
	}
}

// scrape returns what Prometheus would read from g, in the text format.
func scrape(t *testing.T, g prometheus.Gatherer) string {
	t.Helper()
	rec := httptest.NewRecorder()
	promhttp.HandlerFor(g, promhttp.HandlerOpts{}).ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	return rec.Body.String()
}
