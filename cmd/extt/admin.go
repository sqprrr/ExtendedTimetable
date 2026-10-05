package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

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
		if err := newService(st).AdminCreateSuperadmin(ctx, pos[0], password); err != nil {
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
		var cist *int64
		if *cistID != 0 {
			cist = cistID
		}
		g, err := newService(st).AdminCreateGroup(ctx, pos[0], *name, cist)
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "group %s created\ninvite code: %s\n", g.Code, g.InviteCode)

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
		if err := newService(st).AdminSetRole(ctx, pos[0], *group, role); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "%s is now a %s of %s\n", pos[0], role, strings.ToUpper(*group))

	case "invite-code":
		regenerate := fs.Bool("regenerate", false, "replace the current code with a new one")
		pos, st, err := open(1)
		if err != nil {
			return err
		}
		defer st.Close()
		code, err := newService(st).AdminInviteCode(ctx, pos[0], *regenerate)
		if err != nil {
			return err
		}
		fmt.Fprintln(stdout, code)

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
		if err := newService(st).AdminResetPassword(ctx, pos[0], password); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "password for %s updated; existing sessions revoked\n", pos[0])

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
