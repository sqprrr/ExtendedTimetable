package web

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"regexp"
	"strings"
	"time"
)

// staticAssets serves /static/ with cache-busting URLs. Every file gets a
// short hash of its content; pages link to /static/<name>?v=<hash> (the asset
// template func), and a request carrying the current hash may be cached for
// a year. Anything else (no v, or a v from an older deploy) must be
// revalidated, so a cached old page never pins an asset.
type staticAssets struct {
	files map[string]staticFile
}

type staticFile struct {
	body []byte
	hash string
}

// assetHashLen is how many hex digits of the SHA-256 go into ?v=.
const assetHashLen = 12

// cssURL matches url("…") in stylesheets, to version the fonts they load.
var cssURL = regexp.MustCompile(`url\("([^"?#:]+)"\)`)

// loadStatic reads and hashes every file under static/ in fsys. Stylesheets
// are hashed after their url("…") references are versioned, so a new font
// also gives the stylesheet a new hash.
func loadStatic(fsys fs.FS) (*staticAssets, error) {
	s := &staticAssets{files: map[string]staticFile{}}
	var css []string
	err := fs.WalkDir(fsys, "static", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		name := strings.TrimPrefix(p, "static/")
		s.files[name] = staticFile{body: b, hash: contentHash(b)}
		if path.Ext(name) == ".css" {
			css = append(css, name)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, name := range css {
		var missing error
		body := cssURL.ReplaceAllFunc(s.files[name].body, func(m []byte) []byte {
			ref := string(cssURL.FindSubmatch(m)[1])
			f, ok := s.files[path.Join(path.Dir(name), ref)]
			if !ok {
				missing = fmt.Errorf("%s: %s not found", name, ref)
				return m
			}
			return fmt.Appendf(nil, `url("%s?v=%s")`, ref, f.hash)
		})
		if missing != nil {
			return nil, missing
		}
		s.files[name] = staticFile{body: body, hash: contentHash(body)}
	}
	return s, nil
}

func contentHash(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])[:assetHashLen]
}

// url is the versioned URL of a static file, for the asset template func. An
// unknown name fails the template, so a typo shows up in tests.
func (s *staticAssets) url(name string) (string, error) {
	f, ok := s.files[name]
	if !ok {
		return "", fmt.Errorf("unknown static asset %q", name)
	}
	return "/static/" + name + "?v=" + f.hash, nil
}

// ServeHTTP serves a static file below /static/.
func (s *staticAssets) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f, ok := s.files[strings.TrimPrefix(r.URL.Path, "/static/")]
	if !ok {
		http.NotFound(w, r)
		return
	}
	if r.URL.Query().Get("v") == f.hash {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	w.Header().Set("ETag", `"`+f.hash+`"`)
	http.ServeContent(w, r, r.URL.Path, time.Time{}, bytes.NewReader(f.body))
}
