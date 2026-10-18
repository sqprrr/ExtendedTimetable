package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/term"

	"github.com/sqprrr/ExtendedTimetable/internal/cist"
	"github.com/sqprrr/ExtendedTimetable/internal/service"
	"github.com/sqprrr/ExtendedTimetable/internal/store"
)

func cmdAdmin(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return errors.New("admin: missing subcommand")
	}
	sub, args := args[0], args[1:]
	fs := newFlagSet("admin " + sub)
	dbPath := dbFlag(fs)
	baseURL := baseURLFlag(fs)

	// open parses flags and opens the database, which is closed when the
	// command returns; call it after defining subcommand-specific flags.
	var st *store.Store
	defer func() {
		if st != nil {
			st.Close()
		}
	}()
	// Dates the CLI makes (the demo's deadlines) and prints are in the
	// site's time zone, as the server's are.
	loc, locErr := time.LoadLocation(envOr("EXTT_TZ", "Europe/Kyiv"))
	open := func(positional int) ([]string, *service.Service, error) {
		pos, err := parseArgs(fs, args, positional)
		if err != nil {
			return nil, nil, err
		}
		if locErr != nil {
			return nil, nil, fmt.Errorf("EXTT_TZ: %w", locErr)
		}
		if st, err = openStore(ctx, *dbPath); err != nil {
			return nil, nil, err
		}
		return pos, newService(st, loc, ""), nil
	}

	switch sub {
	case "create-superadmin":
		pos, svc, err := open(1)
		if err != nil {
			return err
		}
		password, err := readNewPassword(stdin, stdout)
		if err != nil {
			return err
		}
		if err := svc.AdminCreateSuperadmin(ctx, pos[0], password); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "superadmin %s created\n", strings.ToLower(pos[0]))

	case "create-group":
		cistID := fs.Int64("cist-id", 0, "CIST group id")
		name := fs.String("name", "", "display name (defaults to the code)")
		pos, svc, err := open(1)
		if err != nil {
			return err
		}
		var cistGroup *int64
		if *cistID != 0 {
			cistGroup = cistID
		}
		g, err := svc.AdminCreateGroup(ctx, pos[0], *name, cistGroup)
		if err != nil {
			return err
		}
		inv, err := svc.AdminInvite(ctx, g.Code, false)
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "group %s created; share its invite link:\n", g.Code)
		printInvite(stdout, *baseURL, inv, loc)
		// Load the timetable, and with it the subjects, now rather than at
		// the server's next sync; a failure only delays that.
		if cistGroup != nil {
			if rec, err := svc.AdminSyncSchedule(ctx, g.Code, false); err != nil {
				fmt.Fprintf(stdout, "schedule not loaded yet (%s); the server will retry\n", printable(err.Error()))
			} else {
				fmt.Fprintf(stdout, "%d classes loaded from CIST\n", rec.EventCount)
			}
		}

	case "invite-link":
		regenerate := fs.Bool("regenerate", false, "replace the link first; the old one stops working")
		pos, svc, err := open(1)
		if err != nil {
			return err
		}
		inv, err := svc.AdminInvite(ctx, pos[0], *regenerate)
		if err != nil {
			return err
		}
		printInvite(stdout, *baseURL, inv, loc)

	case "group-log":
		limit := fs.Int("limit", 50, "how many entries to print")
		pos, svc, err := open(1)
		if err != nil {
			return err
		}
		entries, err := svc.AdminGroupLog(ctx, pos[0], *limit)
		if err != nil {
			return err
		}
		for _, e := range entries {
			actor := e.ActorName
			if e.ActorID == nil {
				actor = "(server CLI)"
			}
			fmt.Fprintf(stdout, "%s  %-18s  by %s", e.CreatedAt.In(loc).Format(cliTime), e.Event, actor)
			if e.UserID != nil {
				fmt.Fprintf(stdout, "  user %s", e.UserName)
			}
			fmt.Fprintln(stdout)
		}

	case "promote", "demote":
		group := fs.String("group", "", "group code")
		pos, svc, err := open(1)
		if err != nil {
			return err
		}
		if *group == "" {
			return fmt.Errorf("admin %s: --group is required", sub)
		}
		if sub == "promote" {
			if err := svc.AdminSetLeader(ctx, pos[0], *group); err != nil {
				return err
			}
			fmt.Fprintf(stdout, "%s is now the leader of %s\n", pos[0], strings.ToUpper(*group))
		} else {
			if err := svc.AdminRemoveLeader(ctx, pos[0], *group); err != nil {
				return err
			}
			fmt.Fprintf(stdout, "%s is now a student of %s; the group has no leader\n", pos[0], strings.ToUpper(*group))
		}

	case "reset-password":
		pos, svc, err := open(1)
		if err != nil {
			return err
		}
		password, err := readNewPassword(stdin, stdout)
		if err != nil {
			return err
		}
		if err := svc.AdminResetPassword(ctx, pos[0], password); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "password for %s updated; existing sessions revoked\n", pos[0])

	case "find-cist-group":
		pos, err := parseArgs(fs, args, 1)
		if err != nil {
			return err
		}
		groups, err := cist.New(nil, cistTimeout).FindGroups(ctx, pos[0])
		if err != nil {
			return err
		}
		if len(groups) == 0 {
			return fmt.Errorf("no CIST group matches %q (CIST names are in Ukrainian, e.g. КІУКІ-25-3)", pos[0])
		}
		for _, g := range groups {
			fmt.Fprintf(stdout, "%d\t%s\n", g.ID, printable(g.Name))
		}

	case "set-cist-id":
		pos, svc, err := open(2)
		if err != nil {
			return err
		}
		var id *int64
		if pos[1] != "none" {
			n, err := strconv.ParseInt(pos[1], 10, 64)
			if err != nil || n <= 0 {
				return fmt.Errorf("admin set-cist-id: %q is not a CIST id (or \"none\")", pos[1])
			}
			id = &n
		}
		if err := svc.AdminSetCISTID(ctx, pos[0], id); err != nil {
			return err
		}
		if id == nil {
			fmt.Fprintf(stdout, "%s is no longer linked to CIST\n", strings.ToUpper(pos[0]))
		} else {
			fmt.Fprintf(stdout, "%s is linked to CIST timetable %d; run `extt admin sync-schedule %s` to load it now\n",
				strings.ToUpper(pos[0]), *id, strings.ToUpper(pos[0]))
		}

	case "sync-schedule":
		allowEmpty := fs.Bool("allow-empty", false, "accept an empty timetable from CIST, clearing the cached classes")
		pos, svc, err := open(1)
		if err != nil {
			return err
		}
		rec, err := svc.AdminSyncSchedule(ctx, pos[0], *allowEmpty)
		if errors.Is(err, service.ErrEmptyExport) {
			return fmt.Errorf("%w\nIf CIST really has no classes for %s, run again with --allow-empty", err, strings.ToUpper(pos[0]))
		}
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "%d classes loaded from CIST for %s\n", rec.EventCount, strings.ToUpper(pos[0]))

	case "add-playlist":
		pos, svc, err := open(2)
		if err != nil {
			return err
		}
		err = svc.AdminAddPlaylist(ctx, pos[0], pos[1])
		var serr *service.SyncError
		if err != nil && !errors.As(err, &serr) {
			return err
		}
		code := strings.ToUpper(pos[0])
		switch {
		case os.Getenv("EXTT_YOUTUBE_KEY") == "":
			fmt.Fprintf(stdout, "playlist added to %s; it is synced once EXTT_YOUTUBE_KEY is set\n", code)
		case serr != nil:
			fmt.Fprintf(stdout, "playlist added to %s, but the sync failed\n", code)
			return err
		default:
			fmt.Fprintf(stdout, "playlist added to %s\n", code)
			return printRecordingSync(ctx, svc, stdout, code)
		}

	case "remove-playlist":
		pos, svc, err := open(2)
		if err != nil {
			return err
		}
		if err := svc.AdminRemovePlaylist(ctx, pos[0], pos[1]); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "playlist removed from %s; the recordings it added stay\n", strings.ToUpper(pos[0]))

	case "playlists":
		pos, svc, err := open(1)
		if err != nil {
			return err
		}
		pls, err := svc.AdminPlaylists(ctx, pos[0])
		if err != nil {
			return err
		}
		if len(pls) == 0 {
			fmt.Fprintf(stdout, "%s has no playlists\n", strings.ToUpper(pos[0]))
		}
		for _, p := range pls {
			fmt.Fprintf(stdout, "%s  %s\n", service.PlaylistURL(p.PlaylistID), printable(p.Title))
			if p.LastError != "" {
				fmt.Fprintf(stdout, "    last sync failed: %s\n", printable(p.LastError))
			}
		}

	case "sync-recordings":
		pos, svc, err := open(1)
		if err != nil {
			return err
		}
		if _, err := svc.AdminSyncRecordings(ctx, pos[0]); err != nil {
			return err
		}
		return printRecordingSync(ctx, svc, stdout, strings.ToUpper(pos[0]))

	case "feedback":
		all := fs.Bool("all", false, "include resolved feedback")
		_, svc, err := open(0)
		if err != nil {
			return err
		}
		inbox, err := svc.AdminFeedback(ctx, !*all)
		if err != nil {
			return err
		}
		for _, f := range inbox.Items {
			author := f.Username
			if author == "" {
				author = "(deleted account)"
			}
			fmt.Fprintf(stdout, "#%d  %s  %s  %s", f.ID, f.CreatedAt.In(loc).Format(cliTime), author, f.Kind)
			if f.Rating != nil {
				fmt.Fprintf(stdout, " %d/5", *f.Rating)
			}
			if f.ResolvedAt != nil {
				fmt.Fprint(stdout, "  [resolved]")
			}
			fmt.Fprintf(stdout, "\n  %s\n\n", strings.ReplaceAll(printable(f.Message), "\n", "\n  "))
		}
		fmt.Fprintf(stdout, "%d open; read and resolve them at /admin/feedback\n", inbox.Open)

	case "demo-reset":
		pos, svc, err := open(1)
		if err != nil {
			return err
		}
		passwords, err := svc.ResetDemo(ctx, pos[0])
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "%s is the demo group: %s is its leader, %s a student\n",
			strings.ToUpper(pos[0]), service.DemoUsername, service.DemoStudentUsername)
		for _, name := range service.DemoAccounts {
			if pw, ok := passwords[name]; ok {
				fmt.Fprintf(stdout, "%s password: %s\n", name, pw)
			}
		}
		if len(passwords) > 0 {
			fmt.Fprintln(stdout, "give them to the leaders who ask; change one with `admin reset-password <username>`")
		}
		fmt.Fprintln(stdout, "set EXTT_DEMO_GROUP for the server to reset the group every night")

	case "requests":
		all := fs.Bool("all", false, "include resolved requests")
		_, svc, err := open(0)
		if err != nil {
			return err
		}
		inbox, err := svc.AdminGroupRequests(ctx, !*all)
		if err != nil {
			return err
		}
		for _, gr := range inbox.Items {
			fmt.Fprintf(stdout, "#%d  %s  %s  %s  https://t.me/%s", gr.ID, gr.CreatedAt.In(loc).Format(cliTime),
				printable(gr.GroupName), printable(gr.University), gr.Telegram)
			if gr.CISTID != nil {
				fmt.Fprintf(stdout, "  CIST %d (%s)", *gr.CISTID, printable(gr.CISTName))
			}
			if gr.ResolvedAt != nil {
				fmt.Fprint(stdout, "  [resolved]")
			}
			fmt.Fprintln(stdout)
			if gr.Comment != "" {
				fmt.Fprintf(stdout, "  %s\n", strings.ReplaceAll(printable(gr.Comment), "\n", "\n  "))
			}
			fmt.Fprintln(stdout)
		}
		fmt.Fprintf(stdout, "%d open; read and resolve them at /admin/requests\n", inbox.Open)

	default:
		fmt.Fprint(os.Stderr, usage)
		return fmt.Errorf("admin: unknown subcommand %q", sub)
	}
	return nil
}

// cliTime is how the CLI prints a moment, in the site's time zone.
const cliTime = "2006-01-02 15:04"

// printable makes text that users, YouTube or CIST wrote safe to print to
// the operator's terminal, where control characters are escape sequences
// that can clear the screen, hide lines, retitle the window or write the
// clipboard. Those other than newline and tab are shown escaped (\x1b), and
// invalid UTF-8 as U+FFFD.
func printable(s string) string {
	unsafe := func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\t' }
	if utf8.ValidString(s) && !strings.ContainsFunc(s, unsafe) {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		if unsafe(r) {
			fmt.Fprintf(&b, `\x%02x`, r)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// printInvite prints an invite link, in full when the site's address is known.
func printInvite(w io.Writer, baseURL string, inv *store.Invite, loc *time.Location) {
	fmt.Fprintf(w, "%s/join/%s\n", strings.TrimRight(baseURL, "/"), inv.Token)
	fmt.Fprintf(w, "valid until %s\n", inv.ExpiresAt.In(loc).Format(cliTime))
	if baseURL == "" {
		fmt.Fprintln(w, "(set EXTT_BASE_URL or --base-url to print the full address)")
	}
}

// readNewPassword prompts twice on a terminal, or reads one line otherwise.
func readNewPassword(stdin io.Reader, stdout io.Writer) (string, error) {
	if f, ok := stdin.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		fmt.Fprint(stdout, "Password: ")
		p1, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(stdout)
		if err != nil {
			return "", err
		}
		fmt.Fprint(stdout, "Repeat password: ")
		p2, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(stdout)
		if err != nil {
			return "", err
		}
		if string(p1) != string(p2) {
			return "", errors.New("passwords do not match")
		}
		return string(p1), nil
	}
	line, err := bufio.NewReader(stdin).ReadString('\n')
	if err != nil && !(errors.Is(err, io.EOF) && line != "") {
		return "", fmt.Errorf("read password from stdin: %w", err)
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// printRecordingSync prints what the group's last playlist sync did.
func printRecordingSync(ctx context.Context, svc *service.Service, w io.Writer, code string) error {
	rec, err := svc.AdminRecordingSync(ctx, code)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "%d recordings added from YouTube for %s\n", rec.Added, code)
	if len(rec.Unmatched) > 0 {
		fmt.Fprintln(w, "videos left out (no date, or a subject the group does not have):")
		for _, t := range rec.Unmatched {
			fmt.Fprintln(w, "  "+printable(t))
		}
	}
	return nil
}
