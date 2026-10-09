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
	open := func(positional int) ([]string, *service.Service, error) {
		pos, err := parseArgs(fs, args, positional)
		if err != nil {
			return nil, nil, err
		}
		if st, err = openStore(ctx, *dbPath); err != nil {
			return nil, nil, err
		}
		return pos, newService(st, nil), nil
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
		printInvite(stdout, *baseURL, inv)

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
		printInvite(stdout, *baseURL, inv)

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
			fmt.Fprintf(stdout, "%s  %-18s  by %s", e.CreatedAt.Local().Format(cliTime), e.Event, actor)
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
			fmt.Fprintf(stdout, "%d\t%s\n", g.ID, g.Name)
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
		pos, svc, err := open(1)
		if err != nil {
			return err
		}
		rec, err := svc.AdminSyncSchedule(ctx, pos[0])
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "%d classes loaded from CIST for %s\n", rec.EventCount, strings.ToUpper(pos[0]))

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
			fmt.Fprintf(stdout, "#%d  %s  %s  %s", f.ID, f.CreatedAt.Local().Format(cliTime), author, f.Kind)
			if f.Rating != nil {
				fmt.Fprintf(stdout, " %d/5", *f.Rating)
			}
			if f.ResolvedAt != nil {
				fmt.Fprint(stdout, "  [resolved]")
			}
			fmt.Fprintf(stdout, "\n  %s\n\n", strings.ReplaceAll(f.Message, "\n", "\n  "))
		}
		fmt.Fprintf(stdout, "%d open; read and resolve them at /admin/feedback\n", inbox.Open)

	default:
		fmt.Fprint(os.Stderr, usage)
		return fmt.Errorf("admin: unknown subcommand %q", sub)
	}
	return nil
}

// cliTime is how the CLI prints a moment, in the server's local time.
const cliTime = "2006-01-02 15:04"

// printInvite prints an invite link, in full when the site's address is known.
func printInvite(w io.Writer, baseURL string, inv *store.Invite) {
	fmt.Fprintf(w, "%s/join/%s\n", strings.TrimRight(baseURL, "/"), inv.Token)
	fmt.Fprintf(w, "valid until %s\n", inv.ExpiresAt.Local().Format(cliTime))
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
