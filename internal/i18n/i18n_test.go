package i18n_test

import (
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"github.com/sqprrr/ExtendedTimetable/internal/i18n"
	"github.com/sqprrr/ExtendedTimetable/locales"
)

// ids returns the message IDs of a locale file.
func ids(t *testing.T, lang string) map[string]bool {
	t.Helper()
	var raw map[string]any
	if _, err := toml.DecodeFS(locales.FS, lang+".toml", &raw); err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	var walk func(prefix string, m map[string]any)
	walk = func(prefix string, m map[string]any) {
		for k, v := range m {
			sub, ok := v.(map[string]any)
			if !ok {
				out[prefix+k] = true
				continue
			}
			if _, plural := sub["other"]; plural {
				out[prefix+k] = true
				continue
			}
			walk(prefix+k+".", sub)
		}
	}
	walk("", raw)
	return out
}

func TestLocalesHaveTheSameMessages(t *testing.T) {
	en, uk := ids(t, "en"), ids(t, "uk")
	for id := range en {
		if !uk[id] {
			t.Errorf("uk.toml lacks %s", id)
		}
	}
	for id := range uk {
		if !en[id] {
			t.Errorf("en.toml lacks %s", id)
		}
	}
}

// Message IDs are literals in templates (t "id", th "id") and Go code
// (i18n.M("id"), inputError(field, "id"), .T("id")); prefixes such as
// "status." are completed at run time and listed separately.
var (
	usedIDRe = regexp.MustCompile(`(?:\{\{-?\s*th? |i18n\.M\(|inputError\("[a-z_]+", |\.T\()"([a-z_]+\.[a-z0-9_.]*[a-z0-9_])"`)
	dynamic  = []string{
		"status.not_started", "status.in_progress", "status.done",
		"role.student", "role.leader", "lesson.lecture", "lesson.practice", "lesson.lab",
		"kind.recording", "kind.solution", "error.status.403", "error.status.404", "error.status.500",
		"weekday.short.mon", "weekday.short.sun", "weekday.long.mon", "weekday.long.sun",
	}
)

func TestEveryUsedMessageExists(t *testing.T) {
	en := ids(t, "en")
	root := filepath.Join("..", "..")
	used := map[string]bool{}
	for _, dir := range []string{"internal", "web/templates"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || strings.HasSuffix(path, "_test.go") ||
				!(filepath.Ext(path) == ".go" || filepath.Ext(path) == ".html") {
				return err
			}
			src, err := os.ReadFile(path)
			for _, m := range usedIDRe.FindAllSubmatch(src, -1) {
				used[string(m[1])] = true
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(used) < 100 {
		t.Fatalf("found only %d message IDs; is the pattern out of date?", len(used))
	}
	for _, id := range append(slices.Sorted(maps.Keys(used)), dynamic...) {
		if !en[id] {
			t.Errorf("message %s is used but not defined", id)
		}
	}
}

func TestTranslate(t *testing.T) {
	uk, en := i18n.For("uk"), i18n.For("en")
	if got := i18n.For("de").Lang(); got != "uk" {
		t.Fatalf("unknown language: %s", got)
	}
	// A field name inside a message is translated as well.
	m := i18n.M("err.too_long", "Field", i18n.M("field.title"), "Count", 200)
	if got := uk.Msg(m); got != "Назва: не більше 200 символів." {
		t.Errorf("uk: %q", got)
	}
	if got := en.Msg(m); got != "Title must be at most 200 characters." {
		t.Errorf("en: %q", got)
	}
	for n, want := range map[int]string{1: "хвилину", 2: "хвилини", 5: "хвилин", 21: "хвилину", 11: "хвилин"} {
		if got := uk.T("err.sync_too_soon", "Count", n); !regexp.MustCompile(` ` + want + ` `).MatchString(got) {
			t.Errorf("uk plural for %d: %q", n, got)
		}
	}
	if got := en.T("no.such.message"); got != "no.such.message" {
		t.Errorf("missing message: %q", got)
	}
}
