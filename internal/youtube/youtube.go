// Package youtube lists the videos of a YouTube playlist through the YouTube
// Data API v3, for syncing class recordings. Public and unlisted playlists
// work; private ones answer as not found. Callers depend on the Source
// interface, so another implementation can replace it.
package youtube

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Video is a video of a playlist.
type Video struct {
	ID          string
	Title       string
	Description string
	// PublishedAt is when the video was published.
	PublishedAt time.Time
	Duration    time.Duration
	// Live is true for a stream that is on or not started yet: its length
	// is not known.
	Live bool
}

// Playlist is a playlist with its videos.
type Playlist struct {
	Title  string
	Videos []Video
}

// Source returns a playlist. It is what the recording sync depends on.
type Source interface {
	Playlist(ctx context.Context, playlistID string) (*Playlist, error)
}

// ErrPlaylistNotFound is returned for a playlist that does not exist or is
// private.
var ErrPlaylistNotFound = errors.New("the playlist does not exist or is private")

// DefaultBaseURL is the YouTube Data API.
const DefaultBaseURL = "https://www.googleapis.com/youtube/v3"

// maxPages bounds how many pages of 50 videos one listing reads.
const maxPages = 40

// Client talks to the YouTube Data API. Use New.
type Client struct {
	key  string
	base string
	http *http.Client
}

// New returns a client that authenticates with an API key. base is the API's
// address (DefaultBaseURL if empty); timeout bounds each request.
func New(key, base string, timeout time.Duration) *Client {
	if base == "" {
		base = DefaultBaseURL
	}
	return &Client{key: key, base: strings.TrimRight(base, "/"), http: &http.Client{Timeout: timeout, CheckRedirect: sameHost}}
}

// sameHost follows redirects only to the scheme and host the request went
// to. The key travels in a header, which the HTTP client would otherwise
// send on to any host a redirect names: it drops only Authorization and
// Cookie.
func sameHost(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("stopped after 10 redirects")
	}
	if req.URL.Scheme != via[0].URL.Scheme || req.URL.Host != via[0].URL.Host {
		return fmt.Errorf("redirect to another host (%s) refused", req.URL.Host)
	}
	return nil
}

// Playlist returns the playlist's title and videos, in playlist order.
// Deleted and private videos are left out.
func (c *Client) Playlist(ctx context.Context, playlistID string) (*Playlist, error) {
	var meta struct {
		Items []struct {
			Snippet struct {
				Title string `json:"title"`
			} `json:"snippet"`
		} `json:"items"`
	}
	if err := c.get(ctx, "playlists", url.Values{"part": {"snippet"}, "id": {playlistID}}, &meta); err != nil {
		return nil, err
	}
	if len(meta.Items) == 0 {
		return nil, ErrPlaylistNotFound
	}
	videos, err := c.playlistVideos(ctx, playlistID)
	if err != nil {
		return nil, err
	}
	return &Playlist{Title: meta.Items[0].Snippet.Title, Videos: videos}, nil
}

func (c *Client) playlistVideos(ctx context.Context, playlistID string) ([]Video, error) {
	var ids []string
	page := ""
	for range maxPages {
		var res struct {
			NextPageToken string `json:"nextPageToken"`
			Items         []struct {
				ContentDetails struct {
					VideoID string `json:"videoId"`
				} `json:"contentDetails"`
			} `json:"items"`
		}
		q := url.Values{"part": {"contentDetails"}, "maxResults": {"50"}, "playlistId": {playlistID}}
		if page != "" {
			q.Set("pageToken", page)
		}
		if err := c.get(ctx, "playlistItems", q, &res); err != nil {
			return nil, err
		}
		for _, it := range res.Items {
			ids = append(ids, it.ContentDetails.VideoID)
		}
		if page = res.NextPageToken; page == "" {
			break
		}
	}

	videos := make([]Video, 0, len(ids))
	for start := 0; start < len(ids); start += 50 {
		batch := ids[start:min(start+50, len(ids))]
		var res struct {
			Items []struct {
				ID      string `json:"id"`
				Snippet struct {
					Title                string    `json:"title"`
					Description          string    `json:"description"`
					PublishedAt          time.Time `json:"publishedAt"`
					LiveBroadcastContent string    `json:"liveBroadcastContent"`
				} `json:"snippet"`
				ContentDetails struct {
					Duration string `json:"duration"`
				} `json:"contentDetails"`
			} `json:"items"`
		}
		q := url.Values{"part": {"snippet,contentDetails"}, "id": {strings.Join(batch, ",")}}
		if err := c.get(ctx, "videos", q, &res); err != nil {
			return nil, err
		}
		// The answer leaves out deleted and private videos; keep the
		// playlist's order.
		byID := map[string]Video{}
		for _, it := range res.Items {
			d, _ := ParseDuration(it.ContentDetails.Duration)
			byID[it.ID] = Video{
				ID: it.ID, Title: it.Snippet.Title, Description: it.Snippet.Description, PublishedAt: it.Snippet.PublishedAt.UTC(), Duration: d,
				Live: it.Snippet.LiveBroadcastContent == "live" || it.Snippet.LiveBroadcastContent == "upcoming",
			}
		}
		for _, id := range batch {
			if v, ok := byID[id]; ok {
				videos = append(videos, v)
			}
		}
	}
	return videos, nil
}

// get calls an API method and decodes its JSON answer into out. The key goes
// in a header, so it never shows up in a URL or an error message.
func (c *Client) get(ctx context.Context, method string, q url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/"+method+"?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Goog-Api-Key", c.key)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("youtube %s: %w", method, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return fmt.Errorf("youtube %s: %w", method, err)
	}
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusNotFound {
			return ErrPlaylistNotFound
		}
		var e struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(body, &e) == nil && e.Error.Message != "" {
			return fmt.Errorf("youtube %s: %s: %s", method, resp.Status, e.Error.Message)
		}
		return fmt.Errorf("youtube %s: %s", method, resp.Status)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("youtube %s: %w", method, err)
	}
	return nil
}

var durationRe = regexp.MustCompile(`^P(?:(\d+)D)?(?:T(?:(\d+)H)?(?:(\d+)M)?(?:(\d+)S)?)?$`)

// ParseDuration parses the ISO 8601 durations the API gives ("PT1H42M20S").
func ParseDuration(s string) (time.Duration, error) {
	m := durationRe.FindStringSubmatch(s)
	if m == nil || s == "P" || s == "PT" {
		return 0, fmt.Errorf("bad duration %q", s)
	}
	var d time.Duration
	for i, unit := range []time.Duration{24 * time.Hour, time.Hour, time.Minute, time.Second} {
		if m[i+1] != "" {
			n, err := strconv.Atoi(m[i+1])
			if err != nil {
				return 0, fmt.Errorf("bad duration %q", s)
			}
			d += time.Duration(n) * unit
		}
	}
	return d, nil
}

var playlistIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{10,64}$`)

// PlaylistID returns the playlist id in a playlist link
// (https://youtube.com/playlist?list=…, or a video link with &list=), or s
// itself when it is an id. ok is false for anything else.
func PlaylistID(s string) (id string, ok bool) {
	s = strings.TrimSpace(s)
	if playlistIDRe.MatchString(s) {
		return s, true
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return "", false
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	host = strings.TrimPrefix(host, "m.")
	if host != "youtube.com" && host != "youtu.be" && host != "music.youtube.com" {
		return "", false
	}
	id = u.Query().Get("list")
	return id, playlistIDRe.MatchString(id)
}

var videoIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

// VideoID returns the video id in a YouTube video link: youtu.be/ID,
// youtube.com/watch?v=ID, /live/ID, /embed/ID or /shorts/ID.
func VideoID(link string) (id string, ok bool) {
	u, err := url.Parse(strings.TrimSpace(link))
	if err != nil {
		return "", false
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	host = strings.TrimPrefix(host, "m.")
	path := strings.Trim(u.Path, "/")
	switch {
	case host == "youtu.be":
		id = path
	case host == "youtube.com" && path == "watch":
		id = u.Query().Get("v")
	case host == "youtube.com":
		for _, prefix := range []string{"live/", "embed/", "shorts/"} {
			if rest, found := strings.CutPrefix(path, prefix); found {
				id = rest
			}
		}
	}
	return id, videoIDRe.MatchString(id)
}
