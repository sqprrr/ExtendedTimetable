package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	assets "github.com/sqprrr/ExtendedTimetable/web"
)

func testStatic(t *testing.T, files map[string]string) *staticAssets {
	t.Helper()
	fsys := fstest.MapFS{}
	for name, body := range files {
		fsys["static/"+name] = &fstest.MapFile{Data: []byte(body)}
	}
	s, err := loadStatic(fsys)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestAssetHashFollowsContent(t *testing.T) {
	css := `@font-face { src: url("fonts/a.woff2") format("woff2"); }`
	old := testStatic(t, map[string]string{"app.js": "1", "style.css": css, "fonts/a.woff2": "font v1"})
	changed := testStatic(t, map[string]string{"app.js": "2", "style.css": css, "fonts/a.woff2": "font v2"})

	if old.files["app.js"].hash == changed.files["app.js"].hash {
		t.Error("app.js: the hash did not change with the content")
	}
	if old.files["fonts/a.woff2"].hash == changed.files["fonts/a.woff2"].hash {
		t.Error("font: the hash did not change with the content")
	}
	// The stylesheet itself is the same, but it loads the new font.
	if old.files["style.css"].hash == changed.files["style.css"].hash {
		t.Error("style.css: a new font must give the stylesheet a new hash")
	}
	want := `url("fonts/a.woff2?v=` + changed.files["fonts/a.woff2"].hash + `")`
	if body := string(changed.files["style.css"].body); !strings.Contains(body, want) {
		t.Errorf("style.css: want %s in\n%s", want, body)
	}

	u, err := changed.url("app.js")
	if err != nil || u != "/static/app.js?v="+changed.files["app.js"].hash {
		t.Errorf("url(app.js) = %q, %v", u, err)
	}
	if _, err := changed.url("missing.js"); err == nil {
		t.Error("url of an unknown file: want an error")
	}
}

func TestStaticMissingFontFails(t *testing.T) {
	_, err := loadStatic(fstest.MapFS{"static/style.css": &fstest.MapFile{Data: []byte(`a { background: url("nope.png") }`)}})
	if err == nil {
		t.Fatal("a stylesheet pointing at a missing file must fail at startup")
	}
}

func TestStaticCacheHeaders(t *testing.T) {
	s := testStatic(t, map[string]string{"app.js": "console.log(1)", "fonts/a.woff2": "font"})
	hash := s.files["app.js"].hash
	get := func(target string, hdr ...string) *http.Response {
		r := httptest.NewRequest("GET", target, nil)
		for i := 0; i+1 < len(hdr); i += 2 {
			r.Header.Set(hdr[i], hdr[i+1])
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w.Result()
	}

	resp := get("/static/app.js?v=" + hash)
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || string(body) != "console.log(1)" {
		t.Fatalf("versioned: %d %q", resp.StatusCode, body)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "public, max-age=31536000, immutable" {
		t.Errorf("current hash: Cache-Control = %q", cc)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Errorf("Content-Type = %q", ct)
	}
	if ct := get("/static/fonts/a.woff2").Header.Get("Content-Type"); ct != "font/woff2" {
		t.Errorf("font Content-Type = %q", ct)
	}

	for _, target := range []string{"/static/app.js", "/static/app.js?v=0123456789ab"} {
		resp := get(target)
		if resp.StatusCode != 200 || resp.Header.Get("Cache-Control") != "no-cache" {
			t.Errorf("%s: %d, Cache-Control = %q; want 200 and no-cache", target, resp.StatusCode, resp.Header.Get("Cache-Control"))
		}
	}

	if resp := get("/static/app.js", "If-None-Match", `"`+hash+`"`); resp.StatusCode != http.StatusNotModified {
		t.Errorf("revalidation: %d, want 304", resp.StatusCode)
	}
	for _, target := range []string{"/static/nope.js", "/static/fonts/", "/static/"} {
		if resp := get(target); resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s: %d, want 404", target, resp.StatusCode)
		}
	}
}

// The real stylesheet loads only versioned fonts.
func TestShippedStylesheetFontsVersioned(t *testing.T) {
	s, err := loadStatic(assets.Static)
	if err != nil {
		t.Fatal(err)
	}
	css := string(s.files["style.css"].body)
	refs := regexp.MustCompile(`url\("([^"]+)"\)`).FindAllStringSubmatch(css, -1)
	if len(refs) == 0 {
		t.Fatal("style.css loads no fonts")
	}
	for _, m := range refs {
		if !regexp.MustCompile(`^fonts/[\w-]+\.woff2\?v=[0-9a-f]{12}$`).MatchString(m[1]) {
			t.Errorf("unversioned url in style.css: %s", m[1])
		}
	}
}
