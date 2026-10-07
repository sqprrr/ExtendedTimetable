# ExtendedTimetable

A small hub for KHNURE student groups: class schedule, class links, homework,
notes, and a private homework tracker per student. See
[docs/design.md](docs/design.md) for the full design and milestones.

**Status:** M4 (schedule) done.

- **M1:** open registration (anyone can join any group), login/logout, the
  admin CLI and a home page. Leaders (старости) are appointed only by the
  superadmin through the CLI.
- **M2:** each group has a page at `/g/<code>` with homework, notes, class
  links, recordings & solutions and subjects. Members read everything; the
  group's leaders and superadmins add, edit and delete it. Homework
  descriptions and notes are Markdown (sanitized). The same content is
  available as JSON under `/api/v1/groups/<code>/…`.
- **M3:** every group member keeps a private homework tracker: a status
  (not started → in progress → done, toggled with htmx without a reload)
  and the grade they got, capped at the assignment's max points. "My grades"
  (`/g/<code>/grades`) sums grades per subject and overall. Nobody else,
  leaders and superadmins included, can see someone's status or grades.
- **M4:** the class schedule is synced from CIST every 6 hours (leaders can
  also press "Sync with CIST now"). `/g/<code>/schedule` shows the week with
  the class in progress or next and its meeting link; the overview shows
  today. When CIST is down the last good copy stays, with a note saying so.
  Subjects that appear in the timetable for the first time are created from
  CIST's short names; leaders can rename them, and deleted ones stay deleted.

## Quick start (local)

Requires Go 1.26+.

```sh
go build -o extt ./cmd/extt

# Bootstrap: superadmin, group, leader
./extt admin create-superadmin root          # prompts for a password
./extt admin find-cist-group КІУКІ-25-3        # prints the CIST id: 11881842
./extt admin create-group KIUKI-25-3 --cist-id 11881842 --name "<display name>"
./extt admin sync-schedule KIUKI-25-3          # load the schedule now

# Run over plain HTTP locally (Secure cookies need HTTPS)
./extt serve --secure-cookies=false          # http://127.0.0.1:8080
```

Register at `/register` and pick the group (share
`/register?group=KIUKI-25-3` to preselect it). Everyone joins as a student;
the superadmin makes someone the group leader from the server:

```sh
./extt admin promote <username> --group KIUKI-25-3
```

Run `./extt help` for all commands. Other admin commands: `demote`,
`reset-password`, `set-cist-id <code> <id|none>` (link an existing group to
CIST).

## Configuration

| Env var | Flag | Default |
|---|---|---|
| `EXTT_DB` | `--db` | `extt.db` |
| `EXTT_ADDR` | `--addr` | `127.0.0.1:8080` |
| `EXTT_SECURE_COOKIES` | `--secure-cookies` | `true` |
| `EXTT_TRUST_PROXY` | `--trust-proxy` | `false` (set `true` behind nginx so `X-Real-IP` is used for rate limiting) |
| `EXTT_TZ` | `--tz` | `Europe/Kyiv` (time zone for showing and entering dates) |
| `EXTT_CIST_INTERVAL` | `--cist-interval` | `6h` (how often `serve` syncs schedules from CIST; `0` turns it off) |
| `EXTT_LOG_LEVEL` | `--log-level` | `info` (`debug`, `info`, `warn`, `error`; `debug` also logs the source line) |
| `EXTT_LOG_FORMAT` | `--log-format` | `text` (`json` for log collectors) |

### Logs

Logs go to stderr (under systemd: `journalctl -u extt`). Every request is
logged once, at `debug` for `/static/` and `/healthz`:

```
level=INFO msg=request method=GET path=/g/KIUKI-25-3/schedule status=200 bytes=5588 duration_ms=7.4 request_id=29846df684ca8618 user=root
```

Each request gets an ID, returned as the `X-Request-ID` header (kept from
the proxy when `--trust-proxy` is on) and added to every line logged while
serving it, so an error can be matched to its request. Also logged: logins
and failed logins, registrations, rate limiting, CSRF and cross-origin
rejections, schedule syncs and, at `debug`, CIST requests. Passwords,
tokens, cookies, query strings, request bodies and homework statuses or
grades are never logged.

Migrations in `migrations/` are embedded and applied automatically on every
command; `extt migrate` applies them and exits.

## Development

```sh
go test ./...
CGO_ENABLED=0 go build -o extt ./cmd/extt
```

Layout follows the design doc: `internal/store` (SQLite queries),
`internal/service` (business rules and every permission check),
`internal/web` and `internal/api` (thin HTML and JSON transports),
`internal/auth` (passwords, sessions, CSRF, rate limiting),
`internal/markdown` (Markdown to sanitized HTML),
`internal/cist` (CIST client: timetable CSV export, group lookup),
`internal/logging` (slog setup, request ID and user in every log line),
`internal/server` (middleware wiring).

### JSON API (group content)

All under `/api/v1/groups/{code}`; unsafe methods need the `X-CSRF-Token`
header (get the token from `GET /api/v1/me`).

| Resource | Endpoints |
|---|---|
| `subjects` | `GET`, `POST`, `PUT /{id}`, `DELETE /{id}` |
| `class-links` | `GET`, `POST`, `PUT /{id}`, `DELETE /{id}` |
| `homework` | `GET`, `GET /{id}` (with links and rendered HTML), `POST`, `PUT /{id}`, `DELETE /{id}` |
| `notes` | `GET`, `POST`, `PUT /{id}`, `DELETE /{id}` |
| `resources` | `GET`, `POST`, `PUT /{id}`, `DELETE /{id}` |
| `homework/{id}/progress` | `PUT` — the viewer's own `{"status", "grade"}` |
| `grades` | `GET` — the viewer's totals per subject and overall |
| `schedule` | `GET ?from=YYYY-MM-DD&to=YYYY-MM-DD` (default: the 7 days from today), `POST /sync` (leaders) |

Homework items carry the viewer's own `"progress": {"status", "grade"}`
(absent for a superadmin who is not a member of the group).

`PUT` changes only the fields present in the body; send `null` to clear
`due_at` or `max_points`, and `"links": []` to remove all homework links.
Create and update return the same object as `GET`. Validation errors return
`422` with `{"error", "field"}`; deleting a subject that is still in use
returns `409`.
