package service_test

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/sqprrr/ExtendedTimetable/internal/service"
	"github.com/sqprrr/ExtendedTimetable/internal/store"
	"github.com/sqprrr/ExtendedTimetable/internal/youtube"
	"github.com/sqprrr/ExtendedTimetable/migrations"
)

// fakeYouTube serves canned playlists.
type fakeYouTube struct {
	mu        sync.Mutex
	playlists map[string][]youtube.Video
	// errs makes a playlist (or, under "", every one) fail.
	errs  map[string]error
	added int
}

func (f *fakeYouTube) Playlist(_ context.Context, id string) (*youtube.Playlist, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := cmp.Or(f.errs[id], f.errs[""]); err != nil {
		return nil, err
	}
	videos, ok := f.playlists[id]
	if !ok {
		return nil, youtube.ErrPlaylistNotFound
	}
	return &youtube.Playlist{Title: "Title of " + id[:6], Videos: slices.Clone(videos)}, nil
}

// add puts a video in the test playlist.
func (f *fakeYouTube) add(id, title string, length time.Duration) {
	f.addTo(testPlaylist, id, title, length)
}

func (f *fakeYouTube) addTo(playlist, id, title string, length time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.added++
	f.playlists[playlist] = append(f.playlists[playlist], youtube.Video{ID: id, Title: title, Duration: length,
		PublishedAt: time.Date(2026, 10, 1, 12, 0, 0, f.added, time.UTC)})
}

func (f *fakeYouTube) fail(playlist string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.errs[playlist] = err
}

const (
	testPlaylist  = "PLNlrZymF62fMabcdefghijklmnopqrstu"
	otherPlaylist = "PLotherotherotherotherotherotherot"
)

type recordingsFixture struct {
	*fixture
	yt   *fakeYouTube
	now  *time.Time
	lead context.Context
	subs map[string]int64
}

func setupRecordings(t *testing.T) *recordingsFixture {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(ctx, migrations.FS); err != nil {
		t.Fatal(err)
	}
	yt := &fakeYouTube{playlists: map[string][]youtube.Video{testPlaylist: nil, otherPlaylist: nil}, errs: map[string]error{}}
	svc := service.New(st, service.Config{YouTube: yt})
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	service.SetClock(svc, func() time.Time { return now })
	g, err := svc.AdminCreateGroup(ctx, "KIUKI-25-3", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	f := &recordingsFixture{fixture: &fixture{svc: svc, group: g}, yt: yt, now: &now, subs: map[string]int64{}}
	f.lead = f.leader(t, "lead")
	for _, s := range [][2]string{{"Філософія", "Філ"}, {"Методи та засоби ЦОІ", "МЗЦОІ"}, {"Фізичне виховання", "ФВ"}} {
		sub, err := svc.CreateSubject(f.lead, g.ID, service.SubjectInput{Name: s[0], ShortName: s[1]})
		if err != nil {
			t.Fatal(err)
		}
		f.subs[s[1]] = sub.ID
	}
	return f
}

// recordings returns the group's recordings by URL.
func (f *recordingsFixture) recordings(t *testing.T) map[string]*store.ResourceLink {
	t.Helper()
	links, err := f.svc.ResourceLinks(f.lead, f.group.ID)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]*store.ResourceLink{}
	for _, l := range links {
		out[l.URL] = l
	}
	return out
}

func (f *recordingsFixture) sync(t *testing.T) *store.RecordingSync {
	t.Helper()
	rec, err := f.svc.AdminSyncRecordings(context.Background(), f.group.Code)
	if err != nil {
		t.Fatal(err)
	}
	return rec
}

func TestParseRecordingTitle(t *testing.T) {
	for title, want := range map[string]struct {
		date, subject string
		lt            store.LessonType
		n             int
	}{
		"КІУКІ-25 2026 10 07 Філ ЛК №4":         {"2026-10-07", "Філ", store.LessonLecture, 4},
		"КІКУІ-25 2026 09 11 КЕ ЛК №2":          {"2026-09-11", "КЕ", store.LessonLecture, 2},
		"КІУКІ-25 2026 09 23 ОБД ЛК №3 ПЕРЕНОС": {"2026-09-23", "ОБД", store.LessonLecture, 3},
		"КІУКІ-25 2026 09 07 ФВ №1":             {"2026-09-07", "ФВ", store.LessonPractice, 1},
		"КІУКІ-25 2026 09 01 сПро ЛК №1":        {"2026-09-01", "сПро", store.LessonLecture, 1},
		"2026-10-03 ООП лб №2":                  {"2026-10-03", "ООП", store.LessonLab, 2},
		"КІУКІ-25 2026.10.05 МОАП ПЗ":           {"2026-10-05", "МОАП", store.LessonPractice, 0},
	} {
		p, ok := service.ParseRecordingTitle(title)
		if !ok || p.Date != want.date || p.Subject != want.subject || p.LessonType != want.lt || p.Number != want.n {
			t.Errorf("%q: %+v %v", title, p, ok)
		}
	}
	for _, bad := range []string{"Лекція з філософії", "КІУКІ-25 2026 13 07 Філ ЛК №4", "КІУКІ-25 2026 10 07", "2026 10 07 №4"} {
		if p, ok := service.ParseRecordingTitle(bad); ok {
			t.Errorf("%q should not parse: %+v", bad, p)
		}
	}
}

func TestSyncRecordings(t *testing.T) {
	f := setupRecordings(t)
	ctx := context.Background()
	const h = time.Hour
	f.yt.add("aaaaaaaaaa1", "КІУКІ-25 2026 09 16 Філ ЛК №1", h)
	f.yt.add("aaaaaaaaaa2", "КІУКІ-25 2026 09 23 Філ ЛК №2", h)
	// The uploader numbered two lectures №4; the sync numbers by date.
	f.yt.add("bbbbbbbbbb1", "КІУКІ-25 2026 09 18 МЗЦОІ ЛК №4", h)
	f.yt.add("bbbbbbbbbb2", "КІУКІ-25 2026 09 25 МЗЦОІ ЛК №4", h)
	// Two lectures the same day go by their titles' numbers.
	f.yt.add("bbbbbbbbbb3", "КІУКІ-25 2026 09 04 МЗЦОІ ЛК №2", h)
	f.yt.add("bbbbbbbbbb4", "КІУКІ-25 2026 09 04 МЗЦОІ ЛК №1", h)
	f.yt.add("cccccccccc1", "КІУКІ-25 2026 09 07 ФВ №1", 8*time.Minute)
	f.yt.add("dddddddddd1", "КІУКІ-25 2026 09 21 Філ ЛК №3 НЕ УДАВШЕСЯ", 2*time.Minute) // failed recording
	f.yt.add("eeeeeeeeee1", "КІУКІ-25 2026 09 22 ОБД ЛК №1", h)                         // no such subject
	f.yt.add("eeeeeeeeee2", "Організаційні питання", h)                                 // no date

	// Adding the playlist syncs it right away; links work as well as ids.
	if err := f.svc.AddPlaylist(f.lead, f.group.ID, "https://youtube.com/playlist?list="+testPlaylist+"&si=x"); err != nil {
		t.Fatal(err)
	}
	src, err := f.svc.RecordingsSourceFor(f.lead, f.group.ID)
	if err != nil || len(src.Playlists) != 1 || src.Playlists[0].PlaylistID != testPlaylist ||
		src.Playlists[0].Title != "Title of PLNlrZ" || !src.Enabled || !src.CanAdd || src.Sync == nil {
		t.Fatalf("source: %+v %v", src, err)
	}
	if src.Sync.Added != 7 || !slices.Equal(src.Sync.Unmatched, []string{"КІУКІ-25 2026 09 22 ОБД ЛК №1", "Організаційні питання"}) {
		t.Fatalf("first sync: %+v", src.Sync)
	}
	recs := f.recordings(t)
	want := map[string]struct {
		subject, title, date string
		lt                   store.LessonType
	}{
		"https://youtu.be/aaaaaaaaaa1": {"Філ", "Лекція №1", "2026-09-16", store.LessonLecture},
		"https://youtu.be/aaaaaaaaaa2": {"Філ", "Лекція №2", "2026-09-23", store.LessonLecture},
		"https://youtu.be/bbbbbbbbbb4": {"МЗЦОІ", "Лекція №1", "2026-09-04", store.LessonLecture},
		"https://youtu.be/bbbbbbbbbb3": {"МЗЦОІ", "Лекція №2", "2026-09-04", store.LessonLecture},
		"https://youtu.be/bbbbbbbbbb1": {"МЗЦОІ", "Лекція №3", "2026-09-18", store.LessonLecture},
		"https://youtu.be/bbbbbbbbbb2": {"МЗЦОІ", "Лекція №4", "2026-09-25", store.LessonLecture},
		"https://youtu.be/cccccccccc1": {"ФВ", "Практика №1", "2026-09-07", store.LessonPractice},
	}
	if len(recs) != len(want) {
		t.Fatalf("recordings: %d, want %d", len(recs), len(want))
	}
	for url, w := range want {
		r := recs[url]
		if r == nil || r.SubjectID != f.subs[w.subject] || r.Title != w.title || r.Date != w.date || r.LessonType != w.lt || r.Kind != store.ResourceRecording {
			t.Errorf("%s: %+v, want %+v", url, r, w)
		}
	}

	// A missed lecture uploaded later goes in its place by date; a title set
	// by hand stays; a deleted recording does not come back; the subject the
	// sync could not find is created and the video added.
	philo2 := recs["https://youtu.be/aaaaaaaaaa2"]
	if _, err := f.svc.UpdateResourceLink(f.lead, f.group.ID, philo2.ID, service.ResourceLinkInput{
		SubjectID: philo2.SubjectID, Kind: philo2.Kind, Title: "Лекція 2: Кант", URL: philo2.URL, Date: philo2.Date, LessonType: philo2.LessonType,
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.DeleteResourceLink(f.lead, f.group.ID, recs["https://youtu.be/cccccccccc1"].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.CreateSubject(f.lead, f.group.ID, service.SubjectInput{Name: "Бази даних", ShortName: "ОБД"}); err != nil {
		t.Fatal(err)
	}
	f.yt.add("aaaaaaaaaa0", "КІУКІ-25 2026 09 09 Філ ЛК №1", h)
	rec := f.sync(t)
	if rec.Added != 2 || !slices.Equal(rec.Unmatched, []string{"Організаційні питання"}) {
		t.Fatalf("second sync: %+v", rec)
	}
	recs = f.recordings(t)
	for url, title := range map[string]string{
		"https://youtu.be/aaaaaaaaaa0": "Лекція №1",
		"https://youtu.be/aaaaaaaaaa1": "Лекція №2",
		"https://youtu.be/aaaaaaaaaa2": "Лекція 2: Кант",
		"https://youtu.be/eeeeeeeeee1": "Лекція №1",
	} {
		if recs[url] == nil || recs[url].Title != title {
			t.Errorf("%s: %+v, want %q", url, recs[url], title)
		}
	}
	if recs["https://youtu.be/cccccccccc1"] != nil {
		t.Error("a deleted recording came back")
	}

	// Nothing new: nothing changes.
	if rec := f.sync(t); rec.Added != 0 || len(f.recordings(t)) != len(recs) {
		t.Fatalf("third sync: %+v", rec)
	}

	// A failed fetch is recorded; the recordings stay.
	f.yt.fail("", errors.New("quota exceeded"))
	_, err = f.svc.AdminSyncRecordings(ctx, f.group.Code)
	var serr *service.SyncError
	if !errors.As(err, &serr) || serr.Source != "YouTube" {
		t.Fatalf("failed sync: %v", err)
	}
	if src, _ := f.svc.RecordingsSourceFor(f.lead, f.group.ID); src.Sync.LastError == "" || len(f.recordings(t)) != len(recs) {
		t.Fatalf("after a failure: %+v", src.Sync)
	}
}

func TestSyncRecordingsTakesOverRecordingsAddedByHand(t *testing.T) {
	f := setupRecordings(t)
	// Added with the upload script before the playlist was set, with dates
	// in the titles.
	for _, r := range []struct{ url, title, date string }{
		{"https://youtu.be/aaaaaaaaaa1", "Лекція №1 · 16.09", "2026-09-16"},
		{"https://www.youtube.com/watch?v=aaaaaaaaaa2", "Лекція №2", "2026-09-23"},
	} {
		if _, err := f.svc.CreateResourceLink(f.lead, f.group.ID, service.ResourceLinkInput{
			SubjectID: f.subs["Філ"], Kind: store.ResourceRecording, Title: r.title, URL: r.url, Date: r.date, LessonType: store.LessonLecture,
		}); err != nil {
			t.Fatal(err)
		}
	}
	f.yt.add("aaaaaaaaaa1", "КІУКІ-25 2026 09 16 Філ ЛК №1", time.Hour)
	f.yt.add("aaaaaaaaaa2", "КІУКІ-25 2026 09 23 Філ ЛК №2", time.Hour)
	f.yt.add("aaaaaaaaaa3", "КІУКІ-25 2026 09 30 Філ ЛК №3", time.Hour)
	if err := f.svc.AdminAddPlaylist(context.Background(), f.group.Code, testPlaylist); err != nil {
		t.Fatal(err)
	}
	recs := f.recordings(t)
	if len(recs) != 3 || recs["https://youtu.be/aaaaaaaaaa3"].Title != "Лекція №3" ||
		recs["https://youtu.be/aaaaaaaaaa1"].Title != "Лекція №1 · 16.09" { // set by hand: kept
		t.Fatalf("recordings: %+v", recs)
	}
}

func TestRecordingSyncPermissions(t *testing.T) {
	f := setupRecordings(t)
	stud := f.as(t, f.register(t, "stud"))
	edSess := f.register(t, "editor")
	ed := f.as(t, edSess)
	if err := f.svc.GrantEditor(f.lead, f.group.ID, service.ViewerFrom(ed).UserID); err != nil {
		t.Fatal(err)
	}
	ed = f.as(t, edSess)

	if err := f.svc.AddPlaylist(stud, f.group.ID, testPlaylist); !errors.Is(err, service.ErrForbidden) {
		t.Errorf("student adds a playlist: %v", err)
	}
	if _, err := f.svc.RecordingsSourceFor(stud, f.group.ID); !errors.Is(err, service.ErrForbidden) {
		t.Errorf("student reads the source: %v", err)
	}
	if _, err := f.svc.SyncRecordingsNow(ed, f.group.ID); !errors.Is(err, service.ErrNoPlaylist) {
		t.Errorf("sync without a playlist: %v", err)
	}
	wantInputError(t, f.svc.AddPlaylist(ed, f.group.ID, "https://example.org/x"), "playlist")
	if err := f.svc.AddPlaylist(ed, f.group.ID, testPlaylist); err != nil {
		t.Fatalf("editor adds a playlist: %v", err)
	}
	if err := f.svc.RemovePlaylist(stud, f.group.ID, testPlaylist); !errors.Is(err, service.ErrForbidden) {
		t.Errorf("student removes a playlist: %v", err)
	}

	// Syncing by hand waits for the cooldown.
	if _, err := f.svc.SyncRecordingsNow(ed, f.group.ID); !errors.Is(err, service.ErrRecordingSyncTooSoon) {
		t.Errorf("sync right after: %v", err)
	}
	*f.now = f.now.Add(6 * time.Minute)
	if _, err := f.svc.SyncRecordingsNow(ed, f.group.ID); err != nil {
		t.Errorf("sync after the cooldown: %v", err)
	}

	// Removing the last playlist stops syncing.
	if err := f.svc.RemovePlaylist(f.lead, f.group.ID, testPlaylist); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.RemovePlaylist(f.lead, f.group.ID, testPlaylist); !errors.Is(err, service.ErrNotFound) {
		t.Errorf("removing it again: %v", err)
	}
	*f.now = f.now.Add(6 * time.Minute)
	if _, err := f.svc.SyncRecordingsNow(f.lead, f.group.ID); !errors.Is(err, service.ErrNoPlaylist) {
		t.Errorf("sync after clearing: %v", err)
	}

	// Without an API key nothing syncs, but the playlist can be set.
	off := setup(t)
	lead := off.leader(t, "lead")
	if err := off.svc.AddPlaylist(lead, off.group.ID, testPlaylist); err != nil {
		t.Fatal(err)
	}
	if src, err := off.svc.RecordingsSourceFor(lead, off.group.ID); err != nil || src.Enabled || len(src.Playlists) != 1 {
		t.Errorf("without a key: %+v %v", src, err)
	}
	if _, err := off.svc.SyncRecordingsNow(lead, off.group.ID); !errors.Is(err, service.ErrRecordingSyncDisabled) {
		t.Errorf("sync without a key: %v", err)
	}
}

func TestSyncSeveralPlaylists(t *testing.T) {
	f := setupRecordings(t)
	const h = time.Hour
	f.yt.addTo(testPlaylist, "aaaaaaaaaa1", "КІУКІ-25 2026 09 16 Філ ЛК №1", h)
	f.yt.addTo(otherPlaylist, "aaaaaaaaaa2", "КІУКІ-25 2026 09 23 Філ ЛК №2", h)
	// In both playlists: placed once.
	f.yt.addTo(otherPlaylist, "aaaaaaaaaa1", "КІУКІ-25 2026 09 16 Філ ЛК №1", h)

	for _, pl := range []string{testPlaylist, otherPlaylist} {
		if err := f.svc.AddPlaylist(f.lead, f.group.ID, pl); err != nil {
			t.Fatal(err)
		}
	}
	wantInputError(t, f.svc.AddPlaylist(f.lead, f.group.ID, "https://www.youtube.com/playlist?list="+otherPlaylist), "playlist")
	recs := f.recordings(t)
	if len(recs) != 2 || recs["https://youtu.be/aaaaaaaaaa1"].Title != "Лекція №1" || recs["https://youtu.be/aaaaaaaaaa2"].Title != "Лекція №2" {
		t.Fatalf("recordings from two playlists: %+v", recs)
	}

	// One playlist failing does not stop the other; its error is kept.
	f.yt.fail(testPlaylist, youtube.ErrPlaylistNotFound)
	f.yt.addTo(otherPlaylist, "aaaaaaaaaa3", "КІУКІ-25 2026 09 30 Філ ЛК №3", h)
	rec, err := f.svc.AdminSyncRecordings(context.Background(), f.group.Code)
	var serr *service.SyncError
	if !errors.As(err, &serr) || rec == nil || rec.Added != 1 || rec.LastSuccessAt == nil || rec.LastError == "" {
		t.Fatalf("partial failure: %+v %v", rec, err)
	}
	src, err := f.svc.RecordingsSourceFor(f.lead, f.group.ID)
	if err != nil || len(src.Playlists) != 2 || src.Playlists[0].LastError == "" || src.Playlists[1].LastError != "" {
		t.Fatalf("playlist errors: %+v %v", src.Playlists, err)
	}

	// Removing a playlist keeps its recordings.
	if err := f.svc.RemovePlaylist(f.lead, f.group.ID, testPlaylist); err != nil {
		t.Fatal(err)
	}
	if rec := f.sync(t); rec.LastError != "" || len(f.recordings(t)) != 3 {
		t.Fatalf("after removing: %+v", rec)
	}

	// At most MaxPlaylists.
	for i := 1; i < service.MaxPlaylists; i++ {
		if err := f.svc.AdminAddPlaylist(context.Background(), f.group.Code, fmt.Sprintf("PLfill%028d", i)); err != nil {
			var serr *service.SyncError
			if !errors.As(err, &serr) { // the fake does not know these playlists
				t.Fatal(err)
			}
		}
	}
	if src, _ := f.svc.RecordingsSourceFor(f.lead, f.group.ID); src.CanAdd || len(src.Playlists) != service.MaxPlaylists {
		t.Fatalf("full: %d playlists, CanAdd %v", len(src.Playlists), src.CanAdd)
	}
	wantInputError(t, f.svc.AddPlaylist(f.lead, f.group.ID, testPlaylist), "playlist")
}
