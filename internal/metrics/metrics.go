// Package metrics holds the app's Prometheus metrics. `serve` exposes them at
// /metrics on a separate, loopback-only address (--metrics-addr) that
// Prometheus scrapes; nginx never proxies it.
//
// Labels must have few values: never put a username, group code from a URL,
// raw path or anything else a client chooses in a label.
package metrics

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/sqprrr/ExtendedTimetable/internal/store"
)

// Registry holds every metric of the app, the Go runtime and the process.
var Registry = prometheus.NewRegistry()

var (
	httpRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "extt_http_requests_total",
		Help: "HTTP requests served, by method, route pattern and status code.",
	}, []string{"method", "route", "code"})
	httpDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "extt_http_request_duration_seconds",
		Help:    "Time to serve an HTTP request, by method and route pattern.",
		Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
	}, []string{"method", "route"})
	httpInFlight = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "extt_http_requests_in_flight",
		Help: "HTTP requests being served.",
	})
	panics = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "extt_http_panics_total",
		Help: "Handler panics recovered and answered with 500.",
	})
	logins = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "extt_logins_total",
		Help: "Login attempts by result: success, failure or rate_limited.",
	}, []string{"result"})
	registrations = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "extt_registrations_total",
		Help: "Accounts created through invite links (success), and attempts refused by the rate limit (rate_limited).",
	}, []string{"result"})
	csrfRejections = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "extt_csrf_rejections_total",
		Help: "Unsafe requests rejected by the CSRF or cross-origin check, by reason.",
	}, []string{"reason"})
	scheduleSyncs = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "extt_schedule_syncs_total",
		Help: "Schedule syncs from CIST by result: success or failure.",
	}, []string{"result"})
	scheduleSyncDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "extt_schedule_sync_duration_seconds",
		Help:    "Time to sync one group's schedule from CIST, failures included.",
		Buckets: []float64{.25, .5, 1, 2.5, 5, 10, 15, 25, 40},
	})
)

func init() {
	Registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		collectors.NewBuildInfoCollector(),
		httpRequests, httpDuration, httpInFlight, panics,
		logins, registrations, csrfRejections,
		scheduleSyncs, scheduleSyncDuration,
	)
	// Show every series from the start, so that rate() and the dashboard's
	// panels work before the first failure happens.
	for _, r := range []string{"success", "failure", "rate_limited"} {
		logins.WithLabelValues(r)
	}
	for _, r := range []string{"success", "rate_limited"} {
		registrations.WithLabelValues(r)
	}
	for _, r := range []string{CSRFCrossOrigin, CSRFNoCookie, CSRFNoToken, CSRFMismatch} {
		csrfRejections.WithLabelValues(r)
	}
	for _, r := range []string{"success", "failure"} {
		scheduleSyncs.WithLabelValues(r)
	}
}

// Handler serves the metrics in the Prometheus text format.
func Handler() http.Handler {
	return promhttp.HandlerFor(Registry, promhttp.HandlerOpts{ErrorLog: slogErrorLog{}})
}

// RequestStarted counts a request in flight; call the returned function
// when it is done.
func RequestStarted() (done func()) {
	httpInFlight.Inc()
	return httpInFlight.Dec
}

// ObserveRequest records a served request. route is the ServeMux pattern
// that matched ("GET /g/{code}/homework"), or "" when none did.
func ObserveRequest(method, route string, status int, d time.Duration) {
	method = knownMethod(method)
	route = routeLabel(route)
	httpRequests.WithLabelValues(method, route, strconv.Itoa(status)).Inc()
	httpDuration.WithLabelValues(method, route).Observe(d.Seconds())
}

// Panic counts a recovered handler panic.
func Panic() { panics.Inc() }

// Login results.
const (
	Success     = "success"
	Failure     = "failure"
	RateLimited = "rate_limited"
)

// Login counts a login attempt with result Success, Failure or RateLimited.
func Login(result string) { logins.WithLabelValues(result).Inc() }

// Registration counts an account created (Success) or refused by the rate
// limit (RateLimited).
func Registration(result string) { registrations.WithLabelValues(result).Inc() }

// Reasons a request fails the CSRF check.
const (
	CSRFCrossOrigin = "cross_origin"
	CSRFNoCookie    = "no_cookie"
	CSRFNoToken     = "no_token"
	CSRFMismatch    = "mismatch"
)

// CSRFRejected counts a request rejected by the CSRF check.
func CSRFRejected(reason string) { csrfRejections.WithLabelValues(reason).Inc() }

// ScheduleSync records one group's sync from CIST.
func ScheduleSync(ok bool, d time.Duration) {
	result := Success
	if !ok {
		result = Failure
	}
	scheduleSyncs.WithLabelValues(result).Inc()
	scheduleSyncDuration.Observe(d.Seconds())
}

// knownMethod keeps the method label to the methods the app serves; a
// client can send any token as a method.
func knownMethod(m string) string {
	switch m {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions:
		return m
	}
	return "OTHER"
}

// routeLabel drops the method from a pattern ("GET /login" → "/login"),
// since the method has a label of its own, and the end-of-path marker
// ("GET /{$}" → "/"). No match (404, 405) is "unmatched".
func routeLabel(pattern string) string {
	if pattern == "" {
		return "unmatched"
	}
	if i := strings.IndexByte(pattern, ' '); i >= 0 {
		pattern = strings.TrimLeft(pattern[i+1:], " \t")
	}
	return strings.TrimSuffix(pattern, "{$}")
}

// RegisterStore adds the gauges of NewStoreCollector to Registry.
func RegisterStore(st *store.Store, now func() time.Time) {
	Registry.MustRegister(NewStoreCollector(st, now))
}

// NewStoreCollector returns gauges read from the database on every scrape:
// row counts, the database size and how each group's schedule sync went.
// now decides which sessions are still valid.
func NewStoreCollector(st *store.Store, now func() time.Time) prometheus.Collector {
	return &storeCollector{st: st, now: now}
}

var (
	descUsers         = prometheus.NewDesc("extt_users", "Accounts.", nil, nil)
	descGroups        = prometheus.NewDesc("extt_groups", "Groups.", nil, nil)
	descSignedIn      = prometheus.NewDesc("extt_signed_in_users", "Users with at least one unexpired session.", nil, nil)
	descSessions      = prometheus.NewDesc("extt_sessions", "Unexpired sessions.", nil, nil)
	descHomework      = prometheus.NewDesc("extt_homework", "Homework assignments in all groups.", nil, nil)
	descNotes         = prometheus.NewDesc("extt_notes", "Notes in all groups.", nil, nil)
	descOpenFeedback  = prometheus.NewDesc("extt_feedback_open", "Feedback not resolved yet.", nil, nil)
	descDBBytes       = prometheus.NewDesc("extt_db_size_bytes", "Size of the SQLite database file, without the WAL.", nil, nil)
	descStatsUp       = prometheus.NewDesc("extt_db_stats_up", "1 if the database answered the stats query of this scrape, else 0.", nil, nil)
	descSyncAttempt   = prometheus.NewDesc("extt_schedule_last_attempt_timestamp_seconds", "When the group's schedule sync last ran, as a Unix time.", []string{"group"}, nil)
	descSyncSuccess   = prometheus.NewDesc("extt_schedule_last_success_timestamp_seconds", "When the group's schedule last synced successfully, as a Unix time.", []string{"group"}, nil)
	descSyncEvents    = prometheus.NewDesc("extt_schedule_events", "Classes stored by the group's last successful sync.", []string{"group"}, nil)
	storeQueryTimeout = 5 * time.Second
)

// storeCollector reads its gauges from the database when scraped. The group
// label is a group's code, which only superadmins choose; the series
// exist only for groups linked to CIST.
type storeCollector struct {
	st  *store.Store
	now func() time.Time
}

func (c *storeCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range []*prometheus.Desc{descUsers, descGroups, descSignedIn, descSessions, descHomework, descNotes,
		descOpenFeedback, descDBBytes, descStatsUp, descSyncAttempt, descSyncSuccess, descSyncEvents} {
		ch <- d
	}
}

func (c *storeCollector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), storeQueryTimeout)
	defer cancel()
	s, err := c.st.Stats(ctx, c.now())
	if err != nil {
		slog.Error("metrics: database stats", "err", err)
		ch <- prometheus.MustNewConstMetric(descStatsUp, prometheus.GaugeValue, 0)
		return
	}
	gauge := func(d *prometheus.Desc, v float64, labels ...string) {
		ch <- prometheus.MustNewConstMetric(d, prometheus.GaugeValue, v, labels...)
	}
	gauge(descStatsUp, 1)
	gauge(descUsers, float64(s.Users))
	gauge(descGroups, float64(s.Groups))
	gauge(descSignedIn, float64(s.SignedInUsers))
	gauge(descSessions, float64(s.Sessions))
	gauge(descHomework, float64(s.Homework))
	gauge(descNotes, float64(s.Notes))
	gauge(descOpenFeedback, float64(s.OpenFeedback))
	gauge(descDBBytes, float64(s.DBBytes))
	for _, g := range s.Syncs {
		if g.LastAttemptAt != nil {
			gauge(descSyncAttempt, float64(g.LastAttemptAt.Unix()), g.Code)
		}
		if g.LastSuccessAt != nil {
			gauge(descSyncSuccess, float64(g.LastSuccessAt.Unix()), g.Code)
			gauge(descSyncEvents, float64(g.EventCount), g.Code)
		}
	}
}

// slogErrorLog sends promhttp's errors to the app's log.
type slogErrorLog struct{}

func (slogErrorLog) Println(v ...any) {
	slog.Error("metrics: serve", "err", fmt.Sprint(v...))
}
