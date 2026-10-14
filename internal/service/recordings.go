package service

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/sqprrr/ExtendedTimetable/internal/store"
	"github.com/sqprrr/ExtendedTimetable/internal/youtube"
)

// Class recordings can come from a YouTube playlist: the group's leader or an
// editor sets it, and a sync adds each new video to the recordings. The
// subject, lesson type and date come from the video's title, which the
// uploaders write as
//
//	КІУКІ-25 2026 10 07 Філ ЛК №4
//
// (anything, the date, the subject's short name, the lesson type, the
// number). Recordings are titled "Лекція №4" and numbered by date within
// their subject and lesson type, so mistakes in the uploaders' numbers do
// not matter.

const (
	// MinRecordingLength is how long a video must be to be taken: shorter
	// ones are failed recordings.
	MinRecordingLength = 5 * time.Minute
	// recordingSyncCooldown is how often the group may sync by hand.
	recordingSyncCooldown = 5 * time.Minute
	// RecordingSyncCooldownMinutes is how long the group waits between
	// syncs by hand.
	RecordingSyncCooldownMinutes = int(recordingSyncCooldown / time.Minute)
	// maxUnmatched bounds the unplaced titles a sync keeps for the page.
	maxUnmatched = 20
)

var (
	// ErrNoPlaylist is returned when syncing a group without a playlist.
	ErrNoPlaylist = errors.New("this group has no YouTube playlist yet")
	// ErrRecordingSyncDisabled is returned when the server has no YouTube API key.
	ErrRecordingSyncDisabled = errors.New("syncing recordings from YouTube is turned off on this server")
	// ErrRecordingSyncTooSoon is returned when the group syncs again within the cooldown.
	ErrRecordingSyncTooSoon = fmt.Errorf("the playlist was synced less than %d minutes ago, try again later", RecordingSyncCooldownMinutes)
	// ErrRecordingSyncRunning is returned while another sync of the same group runs.
	ErrRecordingSyncRunning = errors.New("the playlist is being synced right now, try again in a minute")
)

// recordingLessonTypes maps the lesson types written in video titles.
var recordingLessonTypes = map[string]store.LessonType{
	"ЛК": store.LessonLecture,
	"ПЗ": store.LessonPractice,
	"ПР": store.LessonPractice,
	"ЛБ": store.LessonLab,
	"ЛР": store.LessonLab,
}

// recordingTitles are the titles synced recordings get, before " №N".
var recordingTitles = map[store.LessonType]string{
	store.LessonLecture:  "Лекція",
	store.LessonPractice: "Практика",
	store.LessonLab:      "Лабораторна",
}

// autoTitleRe matches the titles the sync gives; others were set by hand
// and are kept.
var autoTitleRe = regexp.MustCompile(`^(Лекція|Практика|Лабораторна) №\d+$`)

var (
	titleDateRe   = regexp.MustCompile(`(\d{4})[ ._-](\d{1,2})[ ._-](\d{1,2})(?:\s|$)`)
	titleNumberRe = regexp.MustCompile(`№\s*(\d+)`)
)

// recordingTitle is what a video's title says.
type recordingTitle struct {
	Date       string // "YYYY-MM-DD"
	Subject    string // the word after the date
	LessonType store.LessonType
	Number     int // 0 if none
}

// parseRecordingTitle reads a video's title. ok is false without a date
// followed by a subject. A title without a lesson type is a practice class
// (physical education is written "ФВ №1").
func parseRecordingTitle(title string) (p recordingTitle, ok bool) {
	m := titleDateRe.FindStringSubmatchIndex(title)
	if m == nil {
		return p, false
	}
	y, _ := strconv.Atoi(title[m[2]:m[3]])
	mo, _ := strconv.Atoi(title[m[4]:m[5]])
	d, _ := strconv.Atoi(title[m[6]:m[7]])
	date := time.Date(y, time.Month(mo), d, 0, 0, 0, 0, time.UTC)
	if date.Year() != y || int(date.Month()) != mo || date.Day() != d {
		return p, false
	}
	p.Date = date.Format(time.DateOnly)
	rest := strings.Fields(title[m[1]:])
	if len(rest) == 0 || strings.HasPrefix(rest[0], "№") {
		return p, false
	}
	p.Subject = rest[0]
	p.LessonType = store.LessonPractice
	if len(rest) > 1 {
		if lt, found := recordingLessonTypes[strings.ToUpper(rest[1])]; found {
			p.LessonType = lt
		}
	}
	if n := titleNumberRe.FindStringSubmatch(strings.Join(rest, " ")); n != nil {
		p.Number, _ = strconv.Atoi(n[1])
	}
	return p, true
}

// RecordingsSource is where the group's recordings are synced from, for
// those who edit it.
type RecordingsSource struct {
	// Playlist is the playlist's id, or "".
	Playlist string
	// Enabled is false when the server cannot sync (no API key).
	Enabled bool
	// Sync is nil until the first sync attempt.
	Sync *store.RecordingSync
}

// PlaylistURL is the playlist's link on YouTube.
func (r *RecordingsSource) PlaylistURL() string {
	if r.Playlist == "" {
		return ""
	}
	return "https://www.youtube.com/playlist?list=" + r.Playlist
}

// RecordingsSourceFor returns the group's playlist and how its last sync
// went (leader, editors and superadmins).
func (s *Service) RecordingsSourceFor(ctx context.Context, groupID int64) (*RecordingsSource, error) {
	if _, err := canEdit(ctx, groupID); err != nil {
		return nil, err
	}
	g, err := s.store.GroupByID(ctx, groupID)
	if err != nil {
		return nil, notFound(err)
	}
	src := &RecordingsSource{Playlist: g.YouTubePlaylist, Enabled: s.cfg.YouTube != nil}
	if src.Sync, err = s.store.RecordingSyncFor(ctx, groupID); errors.Is(err, store.ErrNotFound) {
		src.Sync = nil
	} else if err != nil {
		return nil, err
	}
	return src, nil
}

// SetPlaylist sets the group's playlist from a link or id, or clears it with
// "", and syncs it right away. Recordings already added stay. A failed fetch
// is returned as *SyncError after the playlist is saved.
func (s *Service) SetPlaylist(ctx context.Context, groupID int64, link string) error {
	if _, err := canEdit(ctx, groupID); err != nil {
		return err
	}
	return s.setPlaylist(ctx, groupID, link)
}

func (s *Service) setPlaylist(ctx context.Context, groupID int64, link string) error {
	id := ""
	if strings.TrimSpace(link) != "" {
		var ok bool
		if id, ok = youtube.PlaylistID(link); !ok {
			return inputError("playlist", "err.playlist")
		}
	}
	if err := s.store.SetGroupYouTubePlaylist(ctx, groupID, id); err != nil {
		return notFound(err)
	}
	g, err := s.store.GroupByID(ctx, groupID)
	if err != nil {
		return notFound(err)
	}
	slog.InfoContext(ctx, "playlist set", "group", g.Code, "playlist", id)
	if id == "" || s.cfg.YouTube == nil {
		return nil
	}
	_, err = s.syncRecordings(ctx, g)
	return err
}

// SyncRecordingsNow syncs the group's playlist at the request of someone who
// edits the group, at most once per cooldown.
func (s *Service) SyncRecordingsNow(ctx context.Context, groupID int64) (*store.RecordingSync, error) {
	if _, err := canEdit(ctx, groupID); err != nil {
		return nil, err
	}
	g, err := s.store.GroupByID(ctx, groupID)
	if err != nil {
		return nil, notFound(err)
	}
	if rec, err := s.store.RecordingSyncFor(ctx, groupID); err == nil && s.now().Sub(rec.LastAttemptAt) < recordingSyncCooldown {
		return nil, ErrRecordingSyncTooSoon
	} else if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	return s.syncRecordings(ctx, g)
}

// AdminSetPlaylist sets (or with "" clears) a group's playlist from the
// admin CLI, and syncs it.
func (s *Service) AdminSetPlaylist(ctx context.Context, groupCode, link string) error {
	g, err := s.adminGroup(ctx, groupCode)
	if err != nil {
		return err
	}
	return s.setPlaylist(ctx, g.ID, link)
}

// AdminSyncRecordings syncs a group's playlist from the admin CLI: no
// permission check and no cooldown.
func (s *Service) AdminSyncRecordings(ctx context.Context, groupCode string) (*store.RecordingSync, error) {
	g, err := s.adminGroup(ctx, groupCode)
	if err != nil {
		return nil, err
	}
	return s.syncRecordings(ctx, g)
}

// AdminRecordingSync returns how a group's last playlist sync went, for the
// admin CLI.
func (s *Service) AdminRecordingSync(ctx context.Context, groupCode string) (*store.RecordingSync, error) {
	g, err := s.adminGroup(ctx, groupCode)
	if err != nil {
		return nil, err
	}
	rec, err := s.store.RecordingSyncFor(ctx, g.ID)
	return rec, notFound(err)
}

// RunRecordingSync syncs every group that has a playlist, now and then every
// interval, until ctx is done. Groups synced less than half an interval ago
// are skipped.
func (s *Service) RunRecordingSync(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		s.syncAllRecordings(ctx, every/2)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (s *Service) syncAllRecordings(ctx context.Context, fresh time.Duration) {
	groups, err := s.store.ListGroups(ctx)
	if err != nil {
		if ctx.Err() == nil {
			slog.ErrorContext(ctx, "recording sync: list groups", "err", err)
		}
		return
	}
	for _, g := range groups {
		if g.YouTubePlaylist == "" || ctx.Err() != nil {
			continue
		}
		if rec, err := s.store.RecordingSyncFor(ctx, g.ID); err == nil && s.now().Sub(rec.LastAttemptAt) < fresh {
			continue
		}
		_, err := s.syncRecordings(ctx, g)
		var serr *SyncError
		if err != nil && !errors.As(err, &serr) && !errors.Is(err, ErrRecordingSyncRunning) && ctx.Err() == nil {
			slog.ErrorContext(ctx, "recording sync", "group", g.Code, "err", err)
		}
	}
}

// syncRecordings adds the playlist's new videos to the group's recordings
// and renumbers them. A video is placed once: a recording deleted later is
// not added again. Videos shorter than MinRecordingLength and streams that
// are on are left for later; videos whose title has no date or names no
// subject of the group are listed in the sync status and retried next time.
// A recording added by hand with the video's link is taken over instead of
// added twice.
func (s *Service) syncRecordings(ctx context.Context, g *store.Group) (*store.RecordingSync, error) {
	if s.cfg.YouTube == nil {
		return nil, ErrRecordingSyncDisabled
	}
	if g.YouTubePlaylist == "" {
		return nil, ErrNoPlaylist
	}
	if !s.startSync(syncRecordings, g.ID) {
		return nil, ErrRecordingSyncRunning
	}
	defer s.endSync(syncRecordings, g.ID)
	now := s.now()
	videos, err := s.cfg.YouTube.PlaylistVideos(ctx, g.YouTubePlaylist)

	rec, lerr := s.store.RecordingSyncFor(ctx, g.ID)
	if errors.Is(lerr, store.ErrNotFound) {
		rec = &store.RecordingSync{GroupID: g.ID}
	} else if lerr != nil {
		return nil, lerr
	}
	rec.LastAttemptAt = now
	if err != nil {
		slog.WarnContext(ctx, "recording sync failed", "group", g.Code, "err", err)
		rec.LastError = truncate(err.Error(), 500)
		if serr := s.store.SaveRecordingSync(ctx, rec); serr != nil {
			return nil, serr
		}
		return rec, &SyncError{Source: "YouTube", Err: err}
	}

	var added int
	var unmatched []string
	err = s.store.InTx(ctx, func(q *store.Queries) error {
		subjects, err := q.ListSubjects(ctx, g.ID)
		if err != nil {
			return err
		}
		bySubject := map[string]int64{}
		for _, sub := range subjects {
			bySubject[strings.ToLower(sub.Name)] = sub.ID
		}
		for _, sub := range subjects { // short names win over names
			if sub.ShortName != "" {
				bySubject[strings.ToLower(sub.ShortName)] = sub.ID
			}
		}
		placed, err := q.YouTubeVideoIDs(ctx, g.ID)
		if err != nil {
			return err
		}
		links, err := q.ListResourceLinks(ctx, g.ID)
		if err != nil {
			return err
		}
		byVideo := map[string]int64{}
		for _, l := range links {
			if id, ok := youtube.VideoID(l.URL); ok {
				byVideo[id] = l.ID
			}
		}
		for _, v := range videos {
			if placed[v.ID] || v.Live || v.Duration < MinRecordingLength {
				continue
			}
			p, ok := parseRecordingTitle(v.Title)
			yv := &store.YouTubeVideo{GroupID: g.ID, VideoID: v.ID, TitleNumber: p.Number, PublishedAt: v.PublishedAt}
			if id, found := byVideo[v.ID]; found {
				yv.ResourceID = &id
				if err := q.AddYouTubeVideo(ctx, yv); err != nil {
					return err
				}
				placed[v.ID] = true
				continue
			}
			subjectID, found := bySubject[strings.ToLower(p.Subject)]
			if !ok || !found {
				if len(unmatched) < maxUnmatched {
					unmatched = append(unmatched, truncate(v.Title, maxTitleLen))
				}
				continue
			}
			l := &store.ResourceLink{
				GroupID: g.ID, SubjectID: subjectID, Kind: store.ResourceRecording,
				Title: recordingTitles[p.LessonType] + " №0", URL: "https://youtu.be/" + v.ID,
				Date: p.Date, LessonType: p.LessonType, CreatedAt: now,
			}
			if err := q.CreateResourceLink(ctx, l); err != nil {
				return err
			}
			yv.ResourceID = &l.ID
			if err := q.AddYouTubeVideo(ctx, yv); err != nil {
				return err
			}
			placed[v.ID] = true
			added++
		}
		if err := renumberRecordings(ctx, q, g.ID); err != nil {
			return err
		}
		rec.LastSuccessAt, rec.LastError, rec.Added, rec.Unmatched = &now, "", added, unmatched
		return q.SaveRecordingSync(ctx, rec)
	})
	if err != nil {
		return nil, err
	}
	slog.InfoContext(ctx, "recordings synced", "group", g.Code, "videos", len(videos), "added", added, "unmatched", len(unmatched))
	return rec, nil
}

// renumberRecordings titles the group's synced recordings "Лекція №N",
// numbering each subject's lectures (practice classes, labs) by date. Two of
// the same day go in the order of their titles' numbers, then of
// publishing. Recordings someone renamed by hand keep their title.
func renumberRecordings(ctx context.Context, q *store.Queries, groupID int64) error {
	recs, err := q.ListSyncedRecordings(ctx, groupID)
	if err != nil {
		return err
	}
	slices.SortFunc(recs, func(a, b *store.SyncedRecording) int {
		return cmp.Or(
			cmp.Compare(a.SubjectID, b.SubjectID),
			cmp.Compare(a.LessonType, b.LessonType),
			cmp.Compare(a.Date, b.Date),
			cmp.Compare(a.TitleNumber, b.TitleNumber),
			a.PublishedAt.Compare(b.PublishedAt),
			cmp.Compare(a.ResourceID, b.ResourceID),
		)
	})
	n := 0
	for i, r := range recs {
		if i == 0 || r.SubjectID != recs[i-1].SubjectID || r.LessonType != recs[i-1].LessonType {
			n = 0
		}
		n++
		name, ok := recordingTitles[r.LessonType]
		if !ok || !autoTitleRe.MatchString(r.Title) {
			continue
		}
		if want := name + " №" + strconv.Itoa(n); r.Title != want {
			if err := q.SetResourceLinkTitle(ctx, groupID, r.ResourceID, want); err != nil {
				return err
			}
		}
	}
	return nil
}
