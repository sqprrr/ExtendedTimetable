package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sqprrr/ExtendedTimetable/internal/store"
)

func TestPrintable(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"Лекція №4\tок\nдалі", "Лекція №4\tок\nдалі"},
		{"done\x1b[2J\x1b[H", `done\x1b[2J\x1b[H`},
		{"\x1b]52;c;ZXZpbA==\x07", `\x1b]52;c;ZXZpbA==\x07`},
		{"a\u009bb", `a\x9bb`},
		{"a\rb\x00", `a\x0db\x00`},
		{"bad \x9b byte", "bad � byte"},
	} {
		if got := printable(c.in); got != c.want {
			t.Errorf("printable(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// Feedback saved before the web form refused control characters still
// reaches the operator's terminal as text, not as escape sequences.
func TestAdminFeedbackEscapesControlCharacters(t *testing.T) {
	ctx := context.Background()
	db := filepath.Join(t.TempDir(), "extt.db")
	st, err := openStore(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	u := &store.User{Username: "mallory", PasswordHash: "x", CreatedAt: time.Now()}
	if err := st.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	f := &store.Feedback{UserID: &u.ID, Kind: store.FeedbackBug, Message: "all fixed\x1b[2J\x1b[H\nsecond line", CreatedAt: time.Now()}
	if err := st.CreateFeedback(ctx, f); err != nil {
		t.Fatal(err)
	}
	st.Close()

	var out bytes.Buffer
	if err := run(ctx, []string{"admin", "feedback", "--db", db}, strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	if strings.ContainsRune(out.String(), '\x1b') {
		t.Fatalf("raw escape character printed:\n%q", out.String())
	}
	if !strings.Contains(out.String(), `all fixed\x1b[2J\x1b[H`) || !strings.Contains(out.String(), "\n  second line") {
		t.Fatalf("unexpected output:\n%s", out.String())
	}
}
