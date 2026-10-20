package web

import (
	"io/fs"
	"maps"
	"slices"
	"strings"
	"testing"

	assets "github.com/sqprrr/ExtendedTimetable/web"
)

// cssBlock returns the custom properties set by the first rule whose
// selector is sel, from the position from on.
func cssBlock(t *testing.T, css, sel string, from int) map[string]string {
	t.Helper()
	i := strings.Index(css[from:], sel+" {")
	if i < 0 {
		t.Fatalf("no %s block", sel)
	}
	body := css[from+i+len(sel)+2:]
	body = body[:strings.Index(body, "}")]
	props := map[string]string{}
	for decl := range strings.SplitSeq(body, ";") {
		name, value, ok := strings.Cut(strings.TrimSpace(decl), ":")
		if ok && strings.HasPrefix(name, "--") {
			props[name] = strings.TrimSpace(value)
		}
	}
	return props
}

// A "system" page is drawn light and darkened by a media query in
// tokens.css: its values must be the dark themes', and every theme must
// set the same tokens.
func TestSystemThemeMatchesDark(t *testing.T) {
	b, err := fs.ReadFile(assets.Static, "static/tokens.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(b)
	media := strings.Index(css, "@media (prefers-color-scheme: dark)")
	if media < 0 {
		t.Fatal("tokens.css: no prefers-color-scheme block")
	}
	light := cssBlock(t, css, `:root, [data-theme="light"]`, 0)
	for dark, system := range map[string]string{
		`[data-theme="dark"]`:           `[data-theme="light"][data-theme-choice="system"]`,
		`[data-theme="halloween-dark"]`: `[data-theme="halloween-light"][data-theme-choice="system"]`,
	} {
		want, got := cssBlock(t, css, dark, 0), cssBlock(t, css, system, media)
		if !maps.Equal(want, got) {
			t.Errorf("%s differs from %s:\nwant %v\n got %v", system, dark, want, got)
		}
		if !slices.Equal(slices.Sorted(maps.Keys(want)), slices.Sorted(maps.Keys(light))) {
			t.Errorf("%s does not set the same tokens as the light theme", dark)
		}
	}
	if halloween := cssBlock(t, css, `[data-theme="halloween-light"]`, 0); !slices.Equal(slices.Sorted(maps.Keys(halloween)), slices.Sorted(maps.Keys(light))) {
		t.Error(`[data-theme="halloween-light"] does not set the same tokens as the light theme`)
	}
}

func TestDataTheme(t *testing.T) {
	for _, c := range []struct {
		choice    string
		halloween bool
		want      string
	}{
		{"light", false, "light"},
		{"dark", false, "dark"},
		{"system", false, "light"},
		{"light", true, "halloween-light"},
		{"dark", true, "halloween-dark"},
		{"system", true, "halloween-light"},
	} {
		if got := dataTheme(c.choice, c.halloween); got != c.want {
			t.Errorf("dataTheme(%q, %v) = %q, want %q", c.choice, c.halloween, got, c.want)
		}
	}
}
