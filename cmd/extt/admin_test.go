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

func TestAdminRequests(t *testing.T) {
	ctx := context.Background()
	db := filepath.Join(t.TempDir(), "extt.db")
	st, err := openStore(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	cistID := int64(11881842)
	for _, gr := range []*store.GroupRequest{
		{University: "ХНУРЕ", GroupName: "КІУКІ-25-3", Telegram: "leader_25", Comment: "28 людей\x1b[2J\nдякую",
			CISTID: &cistID, CISTName: "КІУКІ-25-3", CreatedAt: time.Now()},
		{University: "КПІ", GroupName: "ІП-21", Telegram: "other_leader", CreatedAt: time.Now()},
	} {
		if err := st.CreateGroupRequest(ctx, gr); err != nil {
			t.Fatal(err)
		}
	}
	st.Close()

	var out bytes.Buffer
	if err := run(ctx, []string{"admin", "requests", "--db", db}, strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if strings.ContainsRune(s, '\x1b') {
		t.Fatalf("raw escape character printed:\n%q", s)
	}
	for _, want := range []string{"КІУКІ-25-3  ХНУРЕ  https://t.me/leader_25  CIST 11881842 (КІУКІ-25-3)",
		`  28 людей\x1b[2J` + "\n  дякую", "ІП-21  КПІ  https://t.me/other_leader\n", "2 open"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in:\n%s", want, s)
		}
	}
}

// The demo's deadlines are 23:59 in the site's time zone, from the CLI too.
func TestAdminDemoResetUsesSiteTimeZone(t *testing.T) {
	t.Setenv("EXTT_TZ", "Europe/Kyiv")
	ctx := context.Background()
	db := filepath.Join(t.TempDir(), "extt.db")
	var out bytes.Buffer
	for _, args := range [][]string{{"admin", "create-group", "DEMO", "--db", db}, {"admin", "demo-reset", "DEMO", "--db", db}} {
		if err := run(ctx, args, strings.NewReader(""), &out); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out.String())
		}
	}
	if !strings.Contains(out.String(), "demo password: ") || !strings.Contains(out.String(), "demo-student password: ") {
		t.Fatalf("the passwords should be printed:\n%s", out.String())
	}
	st, err := openStore(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	g, err := st.GroupByCode(ctx, "DEMO")
	if err != nil {
		t.Fatal(err)
	}
	list, err := st.ListHomework(ctx, g.ID)
	if err != nil {
		t.Fatal(err)
	}
	kyiv, _ := time.LoadLocation("Europe/Kyiv")
	for _, hw := range list {
		if hw.DueAt != nil {
			if at := hw.DueAt.In(kyiv); at.Hour() != 23 || at.Minute() != 59 {
				t.Errorf("%s is due at %s Kyiv time, want 23:59", hw.Title, at.Format("15:04"))
			}
		}
	}
}
