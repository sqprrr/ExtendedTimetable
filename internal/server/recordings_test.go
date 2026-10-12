package server_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/sqprrr/ExtendedTimetable/internal/service"
	"github.com/sqprrr/ExtendedTimetable/internal/store"
	"github.com/sqprrr/ExtendedTimetable/internal/youtube"
)

// fakePlaylist is a YouTube source with one playlist.
type fakePlaylist struct{ videos []youtube.Video }

const playlistID = "PLNlrZymF62fMabcdefghijklmnopqrstu"

func (f *fakePlaylist) PlaylistVideos(_ context.Context, id string) ([]youtube.Video, error) {
	if id != playlistID {
		return nil, youtube.ErrPlaylistNotFound
	}
	return f.videos, nil
}

func TestRecordingsPlaylist(t *testing.T) {
	e := newEnvWith(t, service.Config{YouTube: &fakePlaylist{videos: []youtube.Video{
		{ID: "N0ICmxyIe-I", Title: "КІУКІ-25 2026 10 07 Філ ЛК №4", Duration: time.Hour},
		{ID: "xxxxxxxxxx1", Title: "КІУКІ-25 2026 10 08 ОБД ЛК №5", Duration: time.Hour},
	}}})
	lead := e.signUp(t, "lead", store.RoleLeader)
	stud := e.signUp(t, "stud", store.RoleStudent)
	const g = "/g/KIUKI-25-3"
	lead.submit(g+"/subjects/new", g+"/subjects", url.Values{"name": {"Філософія"}, "short_name": {"Філ"}})

	if _, body, _ := stud.get(g + "/resources"); strings.Contains(body, "YouTube playlist") {
		t.Fatal("students should not see the playlist settings")
	}
	if code, _, _ := stud.submit(g+"/resources", g+"/resources/youtube", url.Values{"playlist": {playlistID}}); code != http.StatusForbidden {
		t.Fatalf("student sets the playlist: %d", code)
	}

	_, body, _ := lead.get(g + "/resources")
	if !strings.Contains(body, "YouTube playlist") || strings.Contains(body, "Sync with YouTube") {
		t.Fatalf("leader before setting a playlist:\n%s", body)
	}
	code, body, _ := lead.submit(g+"/resources", g+"/resources/youtube", url.Values{"playlist": {"https://example.org/list"}})
	if code != http.StatusUnprocessableEntity || !strings.Contains(body, "not a YouTube playlist link") || !strings.Contains(body, `value="https://example.org/list"`) {
		t.Fatalf("bad link: %d", code)
	}

	code, _, h := lead.submit(g+"/resources", g+"/resources/youtube", url.Values{"playlist": {"https://youtube.com/playlist?list=" + playlistID + "&si=x"}})
	if code != http.StatusSeeOther || h.Get("Location") != g+"/resources" {
		t.Fatalf("set playlist: %d %s", code, h.Get("Location"))
	}
	_, body, _ = lead.get(g + "/resources")
	for _, want := range []string{
		"https://www.youtube.com/playlist?list=" + playlistID, "Sync with YouTube", "new recordings: 1",
		`href="https://youtu.be/N0ICmxyIe-I"`, "Лекція №1", "Videos that could not be placed: 1", "КІУКІ-25 2026 10 08 ОБД ЛК №5",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("recordings page lacks %q", want)
		}
	}
	if _, body, _ := stud.get(g + "/resources"); !strings.Contains(body, "Лекція №1") {
		t.Error("students should see the synced recording")
	}

	// Syncing again right away waits for the cooldown.
	code, body, _ = lead.submit(g+"/resources", g+"/resources/youtube/sync", url.Values{})
	if code != http.StatusTooManyRequests || !strings.Contains(body, "The playlist was synced less than 5 minutes ago") {
		t.Fatalf("sync too soon: %d", code)
	}
}

func TestRecordingsPlaylistWithoutAKey(t *testing.T) {
	e := newEnv(t)
	lead := e.signUp(t, "lead", store.RoleLeader)
	if _, body, _ := lead.get("/g/KIUKI-25-3/resources"); !strings.Contains(body, "no YouTube API key") {
		t.Fatal("the leader should be told the server cannot sync")
	}
}
