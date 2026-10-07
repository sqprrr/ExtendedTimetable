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

	// open parses flags and opens the database; call it after defining
	// subcommand-specific flags.
	open := func(positional int) ([]string, *store.Store, error) {
		pos, err := parseArgs(fs, args, positional)
		if err != nil {
			return nil, nil, err
		}
		st, err := openStore(ctx, *dbPath)
		return pos, st, err
	}

	switch sub {
	case "create-superadmin":
		pos, st, err := open(1)
		if err != nil {
			return err
		}
		defer st.Close()
		password, err := readNewPassword(stdin, stdout)
		if err != nil {
			return err
		}
		if err := newService(st, nil).AdminCreateSuperadmin(ctx, pos[0], password); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "superadmin %s created\n", strings.ToLower(pos[0]))

	case "create-group":
		cistID := fs.Int64("cist-id", 0, "CIST group id")
		name := fs.String("name", "", "display name (defaults to the code)")
		pos, st, err := open(1)
		if err != nil {
			return err
		}
		defer st.Close()
		var cistGroup *int64
		if *cistID != 0 {
			cistGroup = cistID
		}
		g, err := newService(st, nil).AdminCreateGroup(ctx, pos[0], *name, cistGroup)
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "group %s created; anyone can now register into it\n", g.Code)

	case "promote", "demote":
		group := fs.String("group", "", "group code")
		pos, st, err := open(1)
		if err != nil {
			return err
		}
		defer st.Close()
		if *group == "" {
			return fmt.Errorf("admin %s: --group is required", sub)
		}
		role := store.RoleLeader
		if sub == "demote" {
			role = store.RoleStudent
		}
		if err := newService(st, nil).AdminSetRole(ctx, pos[0], *group, role); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "%s is now a %s of %s\n", pos[0], role, strings.ToUpper(*group))

	case "reset-password":
		pos, st, err := open(1)
		if err != nil {
			return err
		}
		defer st.Close()
		password, err := readNewPassword(stdin, stdout)
		if err != nil {
			return err
		}
		if err := newService(st, nil).AdminResetPassword(ctx, pos[0], password); err != nil {
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
		pos, st, err := open(2)
		if err != nil {
			return err
		}
		defer st.Close()
		var id *int64
		if pos[1] != "none" {
			n, err := strconv.ParseInt(pos[1], 10, 64)
			if err != nil || n <= 0 {
				return fmt.Errorf("admin set-cist-id: %q is not a CIST id (or \"none\")", pos[1])
			}
			id = &n
		}
		if err := newService(st, nil).AdminSetCISTID(ctx, pos[0], id); err != nil {
			return err
		}
		if id == nil {
			fmt.Fprintf(stdout, "%s is no longer linked to CIST\n", strings.ToUpper(pos[0]))
		} else {
			fmt.Fprintf(stdout, "%s is linked to CIST timetable %d; run `extt admin sync-schedule %s` to load it now\n",
				strings.ToUpper(pos[0]), *id, strings.ToUpper(pos[0]))
		}

	case "sync-schedule":
		pos, st, err := open(1)
		if err != nil {
			return err
		}
		defer st.Close()
		rec, err := newService(st, nil).AdminSyncSchedule(ctx, pos[0])
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "%d classes loaded from CIST for %s\n", rec.EventCount, strings.ToUpper(pos[0]))

	default:
		fmt.Fprint(os.Stderr, usage)
		return fmt.Errorf("admin: unknown subcommand %q", sub)
	}
	return nil
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
