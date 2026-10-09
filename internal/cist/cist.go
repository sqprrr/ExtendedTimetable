// Package cist reads class schedules from CIST, the KHNURE timetable system
// (https://cist.nure.ua).
//
// Group events come from the CSV export that the CIST website offers for
// calendars: unlike the JSON API (P_API_EVEN_JSON) it needs no client key. It
// carries the subject's short name, the lesson type, the room and the groups,
// but no teachers or full subject names. Callers depend on the Source
// interface, so another implementation can replace it.
package cist

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	_ "time/tzdata" // CIST times are in Kyiv; do not depend on the host's zoneinfo

	"golang.org/x/text/encoding/charmap"
)

// Event is one class in a group's timetable.
type Event struct {
	Start time.Time
	End   time.Time
	// Subject is the subject's short name, e.g. "ООПро".
	Subject string
	// Type is CIST's lesson type abbreviation, e.g. "Лк", "Пз", "Лб".
	Type string
	// Room is the room, or "DL" for distance learning.
	Room string
	// Groups lists the groups that attend, as CIST writes them:
	// "КІУКІ-25-1,2,3;ІСТ-25-1".
	Groups string
}

// Group is a group as listed by CIST.
type Group struct {
	ID   int64
	Name string
}

// Source returns a group's events. It is what the schedule sync depends on.
type Source interface {
	GroupEvents(ctx context.Context, cistGroupID int64, from, to time.Time) ([]Event, error)
}

// DefaultBaseURLs are the CIST servers, tried in order. cist2 is a mirror
// that often answers when the main server does not.
var DefaultBaseURLs = []string{"https://cist.nure.ua", "https://cist2.nure.ua"}

// Client talks to CIST over HTTP. The zero value is not usable; use New.
type Client struct {
	baseURLs []string
	http     *http.Client
	kyiv     *time.Location
	// preferred is the index of the server that answered last, tried first.
	preferred atomic.Int32
}

// New returns a client for the given servers (DefaultBaseURLs if empty).
// timeout bounds each request to one server.
func New(baseURLs []string, timeout time.Duration) *Client {
	if len(baseURLs) == 0 {
		baseURLs = DefaultBaseURLs
	}
	kyiv, err := time.LoadLocation("Europe/Kyiv")
	if err != nil {
		panic(err) // embedded by time/tzdata
	}
	return &Client{baseURLs: baseURLs, http: &http.Client{Timeout: timeout}, kyiv: kyiv}
}

// maxBody bounds a CIST response; a semester of one group is about 40 KB.
const maxBody = 8 << 20

// get fetches path from the first server that answers with a usable body,
// starting with the one that answered last time.
func (c *Client) get(ctx context.Context, path string) ([]byte, error) {
	var errs []error
	first := int(c.preferred.Load())
	for i := range c.baseURLs {
		idx := (first + i) % len(c.baseURLs)
		body, err := c.getFrom(ctx, c.baseURLs[idx]+path)
		if err == nil {
			c.preferred.Store(int32(idx))
			return body, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if i < len(c.baseURLs)-1 {
			slog.InfoContext(ctx, "CIST server unavailable, trying the next one", "err", err)
		}
		errs = append(errs, err)
	}
	return nil, errors.Join(errs...)
}

func (c *Client) getFrom(ctx context.Context, u string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "ExtendedTimetable (+https://github.com/sqprrr/ExtendedTimetable)")
	start := time.Now()
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	slog.DebugContext(ctx, "CIST request", "host", req.URL.Host, "path", req.URL.Path, "status", resp.StatusCode,
		"bytes", len(body), "duration_ms", time.Since(start).Milliseconds())
	if err != nil {
		return nil, fmt.Errorf("%s: %w", req.URL.Host, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %d", req.URL.Host, resp.StatusCode)
	}
	// A cut-off export would parse and replace the window with part of it.
	if len(body) > maxBody {
		return nil, fmt.Errorf("%s: response larger than %d bytes", req.URL.Host, maxBody)
	}
	// CIST reports its own errors with status 200 and an Oracle message.
	if msg := bytes.TrimSpace(body); bytes.HasPrefix(msg, []byte("ORA-")) {
		line, _, _ := bytes.Cut(msg, []byte("\n"))
		return nil, fmt.Errorf("%s: %s", req.URL.Host, decode(line))
	}
	return body, nil
}

// decode converts CIST's windows-1251 text to UTF-8.
func decode(b []byte) string {
	s, err := charmap.Windows1251.NewDecoder().Bytes(b)
	if err != nil {
		// Every byte maps to a character in windows-1251.
		panic(err)
	}
	return string(s)
}

// GroupEvents returns a group's events that start between from and to.
// CIST works in whole days, so from and to are widened to midnight in Kyiv
// and the result is filtered back.
func (c *Client) GroupEvents(ctx context.Context, cistGroupID int64, from, to time.Time) ([]Event, error) {
	q := url.Values{}
	q.Set("ATypeDoc", "3")
	q.Set("Aid_group", strconv.FormatInt(cistGroupID, 10))
	q.Set("Aid_potok", "0")
	q.Set("ADateStart", from.In(c.kyiv).Format("02.01.2006"))
	q.Set("ADateEnd", to.In(c.kyiv).Format("02.01.2006"))
	q.Set("AMultiWorkSheet", "0")
	body, err := c.get(ctx, "/ias/app/tt/WEB_IAS_TT_GNR_RASP.GEN_GROUP_POTOK_RASP?"+q.Encode())
	if err != nil {
		return nil, fmt.Errorf("cist: group %d events: %w", cistGroupID, err)
	}
	events, err := ParseEvents(body, c.kyiv)
	if err != nil {
		return nil, fmt.Errorf("cist: group %d events: %w", cistGroupID, err)
	}
	out := events[:0]
	for _, e := range events {
		if !e.Start.Before(from) && e.Start.Before(to) {
			out = append(out, e)
		}
	}
	return out, nil
}

// csvHeader is the first column of the export's header row.
const csvHeader = "Тема"

// ParseEvents reads the CSV export (windows-1251, one event per line, lines
// ending in a bare CR). Times are local to loc.
func ParseEvents(body []byte, loc *time.Location) ([]Event, error) {
	text := decode(body)
	text = strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(text)
	r := csv.NewReader(strings.NewReader(text))
	r.FieldsPerRecord = -1
	rows, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("read CSV: %w", err)
	}
	if len(rows) == 0 || len(rows[0]) == 0 || strings.TrimPrefix(rows[0][0], "\uFEFF") != csvHeader {
		return nil, errors.New("unexpected response: not a timetable export")
	}
	events := make([]Event, 0, len(rows)-1)
	for i, row := range rows[1:] {
		if len(row) == 1 && strings.TrimSpace(row[0]) == "" {
			continue
		}
		e, err := parseRow(row, loc)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", i+2, err)
		}
		events = append(events, e)
	}
	return events, nil
}

func parseRow(row []string, loc *time.Location) (Event, error) {
	if len(row) < 5 {
		return Event{}, fmt.Errorf("want at least 5 columns, got %d", len(row))
	}
	var e Event
	var err error
	if e.Subject, e.Type, e.Room, e.Groups, err = parseTitle(row[0]); err != nil {
		return Event{}, err
	}
	if e.Start, err = time.ParseInLocation("02.01.2006 15:04:05", row[1]+" "+row[2], loc); err != nil {
		return Event{}, fmt.Errorf("start: %w", err)
	}
	if e.End, err = time.ParseInLocation("02.01.2006 15:04:05", row[3]+" "+row[4], loc); err != nil {
		return Event{}, fmt.Errorf("end: %w", err)
	}
	if !e.End.After(e.Start) {
		return Event{}, fmt.Errorf("event ends before it starts: %q", row[0])
	}
	return e, nil
}

// knownTypes are CIST's lesson type abbreviations.
var knownTypes = map[string]bool{
	"Лк": true, "Пз": true, "Лб": true, "Конс": true, "Зал": true, "Екз": true, "ІспКомб": true, "КП/КР": true,
}

// parseTitle splits an event title such as "ООПро Лк DL КІУКІ-25-1,2,3" into
// subject, type, room and groups. The type is found by name, so a subject or
// room containing spaces still parses; an unknown type falls back to the
// "subject type room groups" layout.
func parseTitle(title string) (subject, typ, room, groups string, err error) {
	f := strings.Fields(title)
	if len(f) < 4 {
		return "", "", "", "", fmt.Errorf("unexpected event title %q", title)
	}
	at := len(f) - 3
	for i := 1; i < len(f)-2; i++ {
		if knownTypes[f[i]] {
			at = i
			break
		}
	}
	return strings.Join(f[:at], " "), f[at], strings.Join(f[at+1:len(f)-1], " "), f[len(f)-1], nil
}

// FindGroups returns the CIST groups whose name contains query, ignoring
// case. It is how an admin looks up a group's CIST id.
func (c *Client) FindGroups(ctx context.Context, query string) ([]Group, error) {
	body, err := c.get(ctx, "/ias/app/tt/P_API_GROUP_JSON")
	if err != nil {
		return nil, fmt.Errorf("cist: groups: %w", err)
	}
	type group struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	var data struct {
		University struct {
			Faculties []struct {
				Directions []struct {
					Groups       []group `json:"groups"`
					Specialities []struct {
						Groups []group `json:"groups"`
					} `json:"specialities"`
				} `json:"directions"`
			} `json:"faculties"`
		} `json:"university"`
	}
	if err := json.Unmarshal([]byte(decode(body)), &data); err != nil {
		return nil, fmt.Errorf("cist: groups: %w", err)
	}
	query = strings.ToLower(strings.TrimSpace(query))
	seen := map[int64]bool{}
	var out []Group
	add := func(gs []group) {
		for _, g := range gs {
			if !seen[g.ID] && strings.Contains(strings.ToLower(g.Name), query) {
				seen[g.ID] = true
				out = append(out, Group{ID: g.ID, Name: g.Name})
			}
		}
	}
	for _, f := range data.University.Faculties {
		for _, d := range f.Directions {
			add(d.Groups)
			for _, s := range d.Specialities {
				add(s.Groups)
			}
		}
	}
	return out, nil
}
