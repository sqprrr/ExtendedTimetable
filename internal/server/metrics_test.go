package server_test

import (
	"bufio"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/sqprrr/ExtendedTimetable/internal/metrics"
)

// metricValues scrapes the app's metrics: series → value.
func metricValues(t *testing.T) map[string]float64 {
	t.Helper()
	rec := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	vals := map[string]float64{}
	sc := bufio.NewScanner(rec.Body)
	for sc.Scan() {
		line := sc.Text()
		i := strings.LastIndexByte(line, ' ')
		if strings.HasPrefix(line, "#") || i < 0 {
			continue
		}
		v, err := strconv.ParseFloat(line[i+1:], 64)
		if err != nil {
			t.Fatalf("metric line %q: %v", line, err)
		}
		vals[line[:i]] = v
	}
	return vals
}

func TestRequestsAreCountedByRoute(t *testing.T) {
	e := newEnv(t)
	b := e.browser(t)
	before := metricValues(t)

	b.register("KIUKI-25-3", "alice")
	b.get("/g/KIUKI-25-3/homework")
	b.get("/g/KIUKI-25-3/homework")
	b.get("/no/such/page")
	b.submit("/", "/logout", url.Values{})
	b.submit("/login", "/login", url.Values{"username": {"alice"}, "password": {"wrong password"}})
	b.submit("/login", "/login", url.Values{"username": {"alice"}, "password": {"correct horse"}})
	b.post("/logout", url.Values{}) // no CSRF token

	after := metricValues(t)
	for series, want := range map[string]float64{
		`extt_http_requests_total{code="200",method="GET",route="/g/{code}/homework"}`:      2,
		`extt_http_requests_total{code="404",method="GET",route="unmatched"}`:               1,
		`extt_http_requests_total{code="403",method="POST",route="/logout"}`:                1,
		`extt_registrations_total{result="success"}`:                                        1,
		`extt_logins_total{result="failure"}`:                                               1,
		`extt_logins_total{result="success"}`:                                               1,
		`extt_csrf_rejections_total{reason="no_token"}`:                                     1,
		`extt_http_request_duration_seconds_count{method="GET",route="/g/{code}/homework"}`: 2,
	} {
		if got := after[series] - before[series]; got != want {
			t.Errorf("%s went up by %v, want %v", series, got, want)
		}
	}
	for series := range after {
		if strings.Contains(series, "KIUKI") || strings.Contains(series, "/no/such") || strings.Contains(series, "alice") {
			t.Errorf("a label holds a value from the request: %s", series)
		}
	}
	if after["extt_http_requests_in_flight"] != 0 {
		t.Errorf("in flight after all requests finished: %v", after["extt_http_requests_in_flight"])
	}
}
