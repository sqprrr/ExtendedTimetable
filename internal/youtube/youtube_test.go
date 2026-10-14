package youtube

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPlaylistVideos(t *testing.T) {
	// Two pages of playlist items; v3 is deleted (videos does not return it).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Goog-Api-Key") != "k" || r.URL.Query().Get("key") != "" {
			http.Error(w, `{"error":{"message":"API key not valid"}}`, http.StatusBadRequest)
			return
		}
		switch r.URL.Path {
		case "/playlistItems":
			if r.URL.Query().Get("playlistId") != "PLx" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			if r.URL.Query().Get("pageToken") == "" {
				fmt.Fprint(w, `{"nextPageToken":"p2","items":[{"contentDetails":{"videoId":"v1"}},{"contentDetails":{"videoId":"v2"}}]}`)
			} else {
				fmt.Fprint(w, `{"items":[{"contentDetails":{"videoId":"v3"}},{"contentDetails":{"videoId":"v4"}}]}`)
			}
		case "/videos":
			if got := r.URL.Query().Get("id"); got != "v1,v2,v3,v4" {
				t.Errorf("videos ids = %q", got)
			}
			fmt.Fprint(w, `{"items":[
				{"id":"v4","snippet":{"title":"Live","publishedAt":"2026-10-07T10:00:00Z","liveBroadcastContent":"live"},"contentDetails":{"duration":"P0D"}},
				{"id":"v2","snippet":{"title":"Second","publishedAt":"2026-10-02T09:00:00Z","liveBroadcastContent":"none"},"contentDetails":{"duration":"PT2M11S"}},
				{"id":"v1","snippet":{"title":"First","publishedAt":"2026-10-01T09:00:00Z","liveBroadcastContent":"none"},"contentDetails":{"duration":"PT1H42M20S"}}
			]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	got, err := New("k", srv.URL, time.Second).PlaylistVideos(context.Background(), "PLx")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].ID != "v1" || got[1].ID != "v2" || got[2].ID != "v4" {
		t.Fatalf("videos (playlist order, deleted left out): %+v", got)
	}
	if got[0].Title != "First" || got[0].Duration != time.Hour+42*time.Minute+20*time.Second ||
		!got[0].PublishedAt.Equal(time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)) || got[0].Live {
		t.Errorf("first video: %+v", got[0])
	}
	if !got[2].Live {
		t.Errorf("a live stream should be marked: %+v", got[2])
	}

	if _, err := New("k", srv.URL, time.Second).PlaylistVideos(context.Background(), "PLnope"); !errors.Is(err, ErrPlaylistNotFound) {
		t.Errorf("missing playlist: %v", err)
	}
	_, err = New("bad", srv.URL, time.Second).PlaylistVideos(context.Background(), "PLx")
	if err == nil || !strings.Contains(err.Error(), "API key not valid") || strings.Contains(err.Error(), "bad") {
		t.Errorf("bad key: %v", err)
	}
}

func TestParseDuration(t *testing.T) {
	for in, want := range map[string]time.Duration{
		"PT1H42M20S": time.Hour + 42*time.Minute + 20*time.Second,
		"PT7M44S":    7*time.Minute + 44*time.Second,
		"PT45S":      45 * time.Second,
		"P1DT2H":     26 * time.Hour,
		"P0D":        0,
	} {
		if got, err := ParseDuration(in); err != nil || got != want {
			t.Errorf("%s: %v %v, want %v", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "P", "PT", "1H", "PT1.5S"} {
		if _, err := ParseDuration(bad); err == nil {
			t.Errorf("%q should not parse", bad)
		}
	}
}

func TestPlaylistID(t *testing.T) {
	const id = "PLNlrZymF62fMabcdefghijklmnopqrstu"
	for _, in := range []string{
		id,
		" " + id + " ",
		"https://youtube.com/playlist?list=" + id + "&si=CTtk6Tbpks63wCQ7",
		"https://www.youtube.com/playlist?list=" + id,
		"https://m.youtube.com/watch?v=N0ICmxyIe-I&list=" + id,
		"https://youtu.be/N0ICmxyIe-I?list=" + id,
	} {
		if got, ok := PlaylistID(in); !ok || got != id {
			t.Errorf("%q: %q %v", in, got, ok)
		}
	}
	for _, bad := range []string{"", "short", "https://example.org/playlist?list=" + id, "https://youtube.com/watch?v=N0ICmxyIe-I", "javascript:alert(1)"} {
		if got, ok := PlaylistID(bad); ok {
			t.Errorf("%q should be rejected, got %q", bad, got)
		}
	}
}

func TestVideoID(t *testing.T) {
	for _, in := range []string{
		"https://youtu.be/N0ICmxyIe-I",
		"https://youtu.be/N0ICmxyIe-I?t=30",
		"https://www.youtube.com/watch?v=N0ICmxyIe-I&list=PLx",
		"https://m.youtube.com/watch?v=N0ICmxyIe-I",
		"https://youtube.com/live/N0ICmxyIe-I",
		"https://www.youtube.com/embed/N0ICmxyIe-I",
	} {
		if got, ok := VideoID(in); !ok || got != "N0ICmxyIe-I" {
			t.Errorf("%q: %q %v", in, got, ok)
		}
	}
	for _, bad := range []string{"https://example.org/watch?v=N0ICmxyIe-I", "https://youtu.be/", "https://drive.google.com/x"} {
		if _, ok := VideoID(bad); ok {
			t.Errorf("%q should be rejected", bad)
		}
	}
}
