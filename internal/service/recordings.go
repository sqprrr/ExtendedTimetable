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

// Class recordings can come from YouTube playlists: the group's leader or an
// editor adds them, and a sync adds each new video to the recordings. The
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
	// MaxPlaylists bounds a group's playlists.
	MaxPlaylists = 10
	// maxUnmatched bounds the unplaced titles a sync keeps for the page.
	maxUnmatched = 20
)

var (
	// ErrNoPlaylist is returned when syncing a group without playlists.
	ErrNoPlaylist = errors.New("this group has no YouTube playlists yet")
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
	// Playlists are the group's playlists, in the order they were added.
	Playlists []*store.YouTubePlaylist
	// Enabled is false when the server cannot sync (no API key).
	Enabled bool
	// Sync is nil until the first sync attempt.
	Sync *store.RecordingSync
	// CanAdd is false once the group has MaxPlaylists.
	CanAdd bool
}

// PlaylistURL is a playlist's link on YouTube.
func PlaylistURL(id string) string { return "https://www.youtube.com/playlist?list=" + id }

// RecordingsSourceFor returns the group's playlists and how their last sync
// went (leader, editors and superadmins).
func (s *Service) RecordingsSourceFor(ctx context.Context, groupID int64) (*RecordingsSource, error) {
	if _, err := canEdit(ctx, groupID); err != nil {
		return nil, err
	}
	pls, err := s.store.ListYouTubePlaylists(ctx, groupID)
	if err != nil {
		return nil, err
	}
	src := &RecordingsSource{Playlists: pls, Enabled: s.cfg.YouTube != nil, CanAdd: len(pls) < MaxPlaylists}
	if src.Sync, err = s.store.RecordingSyncFor(ctx, groupID); errors.Is(err, store.ErrNotFound) {
		src.Sync = nil
	} else if err != nil {
		return nil, err
	}
	return src, nil
}

// AddPlaylist adds a playlist to the group from a link or id, and syncs the
// group's playlists right away. A failed fetch is returned as *SyncError
// after the playlist is saved.
func (s *Service) AddPlaylist(ctx context.Context, groupID int64, link string) error {
	if _, err := canEdit(ctx, groupID); err != nil {
		return err
	}
	return s.addPlaylist(ctx, groupID, link)
}

func (s *Service) addPlaylist(ctx context.Context, groupID int64, link string) error {
	id, ok := youtube.PlaylistID(link)
	if !ok {
		return inputError("playlist", "err.playlist")
	}
	err := s.store.InTx(ctx, func(q *store.Queries) error {
		pls, err := q.ListYouTubePlaylists(ctx, groupID)
		if err != nil {
			return err
		}
		if len(pls) >= MaxPlaylists {
			return inputError("playlist", "err.too_many_playlists", "Count", MaxPlaylists)
		}
		err = q.AddYouTubePlaylist(ctx, &store.YouTubePlaylist{GroupID: groupID, PlaylistID: id, AddedAt: s.now()})
		if errors.Is(err, store.ErrConflict) {
			return inputError("playlist", "err.playlist_added")
		}
		return err
	})
	if err != nil {
		return err
	}
	g, err := s.store.GroupByID(ctx, groupID)
	if err != nil {
		return notFound(err)
	}
	slog.InfoContext(ctx, "playlist added", "group", g.Code, "playlist", id)
	if s.cfg.YouTube == nil {
		return nil
	}
	_, err = s.syncRecordings(ctx, g)
	return err
}

// RemovePlaylist stops syncing a playlist. The recordings it added stay.
func (s *Service) RemovePlaylist(ctx context.Context, groupID int64, playlistID string) error {
	if _, err := canEdit(ctx, groupID); err != nil {
		return err
	}
	return s.removePlaylist(ctx, groupID, playlistID)
}

func (s *Service) removePlaylist(ctx context.Context, groupID int64, playlistID string) error {
	if err := notFound(s.store.DeleteYouTubePlaylist(ctx, groupID, playlistID)); err != nil {
		return err
	}
	slog.InfoContext(ctx, "playlist removed", "group_id", groupID, "playlist", playlistID)
	return nil
}

// SyncRecordingsNow syncs the group's playlists at the request of someone
// who edits the group, at most once per cooldown.
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

// AdminAddPlaylist adds a playlist to a group from the admin CLI, and syncs.
func (s *Service) AdminAddPlaylist(ctx context.Context, groupCode, link string) error {
	g, err := s.adminGroup(ctx, groupCode)
	if err != nil {
		return err
	}
	return s.addPlaylist(ctx, g.ID, link)
}

// AdminRemovePlaylist removes a group's playlist, given by link or id, from
// the admin CLI.
func (s *Service) AdminRemovePlaylist(ctx context.Context, groupCode, link string) error {
	g, err := s.adminGroup(ctx, groupCode)
	if err != nil {
		return err
	}
	id, ok := youtube.PlaylistID(link)
	if !ok {
		return inputError("playlist", "err.playlist")
	}
	return s.removePlaylist(ctx, g.ID, id)
}

// AdminPlaylists returns a group's playlists, for the admin CLI.
func (s *Service) AdminPlaylists(ctx context.Context, groupCode string) ([]*store.YouTubePlaylist, error) {
	g, err := s.adminGroup(ctx, groupCode)
	if err != nil {
		return nil, err
	}
	return s.store.ListYouTubePlaylists(ctx, g.ID)
}

// AdminSyncRecordings syncs a group's playlists from the admin CLI: no
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

// RunRecordingSync syncs every group that has playlists, now and then every
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
		if ctx.Err() != nil {
			return
		}
		if rec, err := s.store.RecordingSyncFor(ctx, g.ID); err == nil && s.now().Sub(rec.LastAttemptAt) < fresh {
			continue
		}
		_, err := s.syncRecordings(ctx, g)
		var serr *SyncError
		if err != nil && !errors.As(err, &serr) && !errors.Is(err, ErrNoPlaylist) &&
			!errors.Is(err, ErrRecordingSyncRunning) && ctx.Err() == nil {
			slog.ErrorContext(ctx, "recording sync", "group", g.Code, "err", err)
		}
	}
}

// syncRecordings adds the new videos of the group's playlists to its
// recordings and renumbers them. A video in several playlists is placed
// once, and a recording deleted later is not added again. Videos shorter
// than MinRecordingLength and streams that are on are left for later; videos
// whose title has no date or names no subject of the group are listed in
// the sync status and retried next time. A recording added by hand with the
// video's link is taken over instead of added twice.
//
// A playlist that cannot be read keeps its error and does not stop the
// others; the sync returns *SyncError if any failed.
func (s *Service) syncRecordings(ctx context.Context, g *store.Group) (*store.RecordingSync, error) {
	if s.cfg.YouTube == nil {
		return nil, ErrRecordingSyncDisabled
	}
	pls, err := s.store.ListYouTubePlaylists(ctx, g.ID)
	if err != nil {
		return nil, err
	}
	if len(pls) == 0 {
		return nil, ErrNoPlaylist
	}
	if !s.startSync(syncRecordings, g.ID) {
		return nil, ErrRecordingSyncRunning
	}
	defer s.endSync(syncRecordings, g.ID)
	now := s.now()

	var videos []youtube.Video
	var failures []error
	seen := map[string]bool{}
	for _, pl := range pls {
		got, err := s.cfg.YouTube.Playlist(ctx, pl.PlaylistID)
		pl.LastError = ""
		if err != nil {
			slog.WarnContext(ctx, "playlist sync failed", "group", g.Code, "playlist", pl.PlaylistID, "err", err)
			pl.LastError = truncate(err.Error(), 500)
			failures = append(failures, fmt.Errorf("%s: %w", cmp.Or(pl.Title, pl.PlaylistID), err))
			continue
		}
		pl.Title = truncate(got.Title, maxTitleLen)
		for _, v := range got.Videos {
			if !seen[v.ID] {
				seen[v.ID] = true
				videos = append(videos, v)
			}
		}
	}
	failed := errors.Join(failures...)

	rec, err := s.store.RecordingSyncFor(ctx, g.ID)
	if errors.Is(err, store.ErrNotFound) {
		rec = &store.RecordingSync{GroupID: g.ID}
	} else if err != nil {
		return nil, err
	}
	rec.LastAttemptAt = now
	rec.LastError = ""
	if failed != nil {
		rec.LastError = truncate(strings.ReplaceAll(failed.Error(), "\n", "; "), 500)
	}
	if len(failures) == len(pls) {
		err := s.store.InTx(ctx, func(q *store.Queries) error {
			for _, pl := range pls {
				if err := q.SaveYouTubePlaylistStatus(ctx, pl); err != nil {
					return err
				}
			}
			return q.SaveRecordingSync(ctx, rec)
		})
		if err != nil {
			return nil, err
		}
		return rec, &SyncError{Source: "YouTube", Err: failed}
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
		for _, pl := range pls {
			if err := q.SaveYouTubePlaylistStatus(ctx, pl); err != nil {
				return err
			}
		}
		rec.LastSuccessAt, rec.Added, rec.Unmatched = &now, added, unmatched
		return q.SaveRecordingSync(ctx, rec)
	})
	if err != nil {
		return nil, err
	}
	slog.InfoContext(ctx, "recordings synced", "group", g.Code, "playlists", len(pls), "videos", len(videos),
		"added", added, "unmatched", len(unmatched))
	if failed != nil {
		return rec, &SyncError{Source: "YouTube", Err: failed}
	}
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
