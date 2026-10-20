package web

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	assets "github.com/sqprrr/ExtendedTimetable/web"
)

func TestIcons(t *testing.T) {
	icons, err := loadIcons(assets.Icons)
	if err != nil {
		t.Fatal(err)
	}
	got, err := icons.html("house")
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	for _, want := range []string{`<svg class="icon" aria-hidden="true"`, `stroke="currentColor"`, `<path d="M15 21v-8`, `</svg>`} {
		if !strings.Contains(s, want) {
			t.Errorf("house icon: missing %q in %s", want, s)
		}
	}
	if strings.Contains(s, "lucide") || strings.Contains(s, "\n") || strings.Count(s, "<svg") != 1 {
		t.Errorf("house icon: want one bare <svg> on one line, got %s", s)
	}
	if _, err := icons.html("no-such-icon"); err == nil {
		t.Error("unknown icon: want an error")
	}
}

// Every icon a template names must exist, or the page fails to render.
func TestTemplateIconsExist(t *testing.T) {
	icons, err := loadIcons(assets.Icons)
	if err != nil {
		t.Fatal(err)
	}
	ref := regexp.MustCompile(`(?:template "icon"|\bicon) "([^"]+)"`)
	files, _ := fs.Glob(assets.Templates, "templates/*.html")
	for _, f := range files {
		b, _ := fs.ReadFile(assets.Templates, f)
		for _, m := range ref.FindAllStringSubmatch(string(b), -1) {
			if _, ok := icons[m[1]]; !ok {
				t.Errorf("%s: unknown icon %q", f, m[1])
			}
		}
	}
}

func TestIconSpriteErrors(t *testing.T) {
	for name, svg := range map[string]string{
		"no symbols":    `<svg xmlns="http://www.w3.org/2000/svg"></svg>`,
		"no id":         `<svg><symbol viewBox="0 0 24 24"><path d="M1 1"/></symbol></svg>`,
		"other grid":    `<svg><symbol id="a" viewBox="0 0 16 16"><path d="M1 1"/></symbol></svg>`,
		"defined twice": `<svg><symbol id="a" viewBox="0 0 24 24"><path d="M1 1"/></symbol><symbol id="a" viewBox="0 0 24 24"><path d="M2 2"/></symbol></svg>`,
		"not xml":       `<svg><symbol id="a" viewBox="0 0 24 24">`,
	} {
		if _, err := loadIcons(fstest.MapFS{"icons/sprite.svg": {Data: []byte(svg)}}); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

// stylesheets are the stylesheets every page links, in order.
var stylesheets = []string{"tokens.css", "bundle.css", "site.css", "halloween.css"}

// The CSP allows only 'self': the stylesheets must not load anything from
// elsewhere, and every file they name must ship. Inline data: images (the
// Halloween masks) are allowed by img-src.
func TestStylesheetIsSelfContained(t *testing.T) {
	remote := regexp.MustCompile(`url\(\s*["']?(https?:)?//`)
	for _, name := range stylesheets {
		f := "static/" + name
		b, err := fs.ReadFile(assets.Static, f)
		if err != nil {
			t.Fatal(err)
		}
		css := string(b)
		if strings.Contains(css, "@import") || remote.MatchString(css) {
			t.Errorf("%s loads something from another origin", f)
		}
		for _, m := range regexp.MustCompile(`url\("([^"]+)"\)`).FindAllStringSubmatch(css, -1) {
			if strings.HasPrefix(m[1], "data:") {
				continue
			}
			if _, err := fs.Stat(assets.Static, "static/"+m[1]); err != nil {
				t.Errorf("%s: %s does not exist", f, m[1])
			}
		}
	}
}
