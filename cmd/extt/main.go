// Command extt runs the ExtendedTimetable server and its admin CLI.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/sqprrr/ExtendedTimetable/internal/service"
	"github.com/sqprrr/ExtendedTimetable/internal/store"
	"github.com/sqprrr/ExtendedTimetable/migrations"
)

const usage = `Usage: extt <command> [flags]

Commands:
  serve                                   Run the web server
  migrate                                 Apply database migrations and exit
  admin create-superadmin <username>      Create a superadmin account
  admin create-group <code> [--cist-id N] [--name NAME]
                                          Create a group; anyone can register into it
  admin promote <username> --group CODE   Make a user a leader (староста) of a group;
                                          this is the only way to appoint one
  admin demote <username> --group CODE    Make a leader a regular student again
  admin reset-password <username>         Set a new password and sign the user out

Every command accepts --db PATH (default $EXTT_DB or ./extt.db).
Commands that take a password prompt for it, or read one line from stdin
when stdin is not a terminal.

Environment:
  EXTT_DB             SQLite database path            (default extt.db)
  EXTT_ADDR           listen address for serve        (default 127.0.0.1:8080)
  EXTT_SECURE_COOKIES set to "false" for local HTTP   (default true)
  EXTT_TRUST_PROXY    "true" to use X-Real-IP         (default false)
  EXTT_TZ             time zone for dates             (default Europe/Kyiv)
`

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdin, os.Stdout); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(2)
		}
		fmt.Fprintln(os.Stderr, "extt:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return flag.ErrHelp
	}
	switch args[0] {
	case "serve":
		return cmdServe(ctx, args[1:])
	case "migrate":
		fs := newFlagSet("migrate")
		dbPath := dbFlag(fs)
		if _, err := parseArgs(fs, args[1:], 0); err != nil {
			return err
		}
		st, err := openStore(ctx, *dbPath)
		if err != nil {
			return err
		}
		defer st.Close()
		fmt.Fprintln(stdout, "migrations applied")
		return nil
	case "admin":
		return cmdAdmin(ctx, args[1:], stdin, stdout)
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return nil
	default:
		fmt.Fprint(os.Stderr, usage)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

// openStore opens the database and brings the schema up to date.
func openStore(ctx context.Context, path string) (*store.Store, error) {
	st, err := store.Open(path)
	if err != nil {
		return nil, err
	}
	if err := st.Migrate(ctx, migrations.FS); err != nil {
		st.Close()
		return nil, err
	}
	return st, nil
}

func newService(st *store.Store) *service.Service {
	return service.New(st, service.Config{})
}

func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprint(fs.Output(), usage) }
	return fs
}

func dbFlag(fs *flag.FlagSet) *string {
	return fs.String("db", envOr("EXTT_DB", "extt.db"), "SQLite database path")
}

// parseArgs parses flags that may appear before or after positional
// arguments (e.g. `promote alice --group X`) and checks the positional count.
func parseArgs(fs *flag.FlagSet, args []string, positional int) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			break
		}
		pos = append(pos, args[0])
		args = args[1:]
	}
	if len(pos) != positional {
		return nil, fmt.Errorf("%s: expected %d argument(s), got %d", fs.Name(), positional, len(pos))
	}
	return pos, nil
}

func envOr(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func envBool(key string, def bool) bool {
	v, err := strconv.ParseBool(os.Getenv(key))
	if err != nil {
		return def
	}
	return v
}
