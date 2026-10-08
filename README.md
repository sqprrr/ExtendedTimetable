# ExtendedTimetable

A small hub for KHNURE student groups: class schedule, class links, homework,
notes, and a private homework tracker per student. See
[docs/design.md](docs/design.md) for the full design and milestones.

**Status:** M5 (i18n & deploy) done — every milestone of the design is in.

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
- **M5:** the site is in Ukrainian by default and in English on request
  (the switch in the top bar). The choice is kept in a cookie and, once
  signed in, in the account, so it follows the user to other browsers.
  `deploy/` holds a systemd unit, nginx site, nightly backups and scripts
  that install or upgrade the site on a VPS: see
  [docs/deploy.md](docs/deploy.md).
- **After M5:** the homework list can be filtered by subject and by your own
  status (`/g/<code>/homework?subject_id=…&status=…`; the JSON list takes the
  same parameters). Signed-in users send bug reports, suggestions and reviews
  (with an optional 1–5 star rating) from "Feedback" in the top bar; they see
  what they sent and whether it was resolved. Superadmins read, resolve and
  delete feedback at `/admin/feedback`, or print it on the server with
  `extt admin feedback`.
- **Invite links:** groups are joined only through their invite link,
  `/join/<token>`, where a visitor creates an account or logs in. A user is
  in one group at most. A link works for 10 days or until it is replaced; it
  is created with the group and shown to the leader and superadmins on the
  group's Members page (`/g/<code>/members`), where they replace it, and
  where they remove members (which replaces the link too). Students may
  leave a group. While a group has no leader, any member can take the role;
  the leader can give it up. Superadmins hand the role to another member or
  take it away there, create groups and see every group at `/admin/groups`,
  and read each group's log (joins, departures, removals, leader changes,
  new links) on its Members page or with `extt admin group-log`.
- **Subject page:** each subject on the Subjects tab opens
  `/g/<code>/subjects/<id>`: its lecturer and practice/lab teacher and its
  DL page (the leader fills them in on the subject form), its class links,
  and tabs for its homework and for its recordings and solutions, which can
  be filtered by lesson type (recordings and solutions now take an optional
  one).

## Quick start (local)

Requires Go 1.26+.

```sh
go build -o extt ./cmd/extt

# Bootstrap: superadmin and group
./extt admin create-superadmin root          # prompts for a password
./extt admin find-cist-group КІУКІ-25-3        # prints the CIST id: 11881842
./extt admin create-group KIUKI-25-3 --cist-id 11881842 --name "<display name>" \
    --base-url http://127.0.0.1:8080           # prints the invite link
./extt admin sync-schedule KIUKI-25-3          # load the schedule now

# Run over plain HTTP locally (Secure cookies need HTTPS)
./extt serve --secure-cookies=false          # http://127.0.0.1:8080
```

Open the invite link to register: everyone joins as a student, and the
first to press "Become the leader" gets the role. The superadmin can also
create groups and change leaders in the panel at `/admin/groups`, or from the
server:

```sh
./extt admin invite-link KIUKI-25-3 [--regenerate]   # print (or replace) the link
./extt admin promote <username> --group KIUKI-25-3   # replaces the current leader
```

Run `./extt help` for all commands. Other admin commands: `demote`,
`group-log <code>`, `reset-password`, `set-cist-id <code> <id|none>` (link an
existing group to CIST), `feedback [--all]` (print the feedback users sent). `./extt backup <file>` writes a consistent copy of the database, also
while the server runs.

## Deployment

```sh
deploy/deploy.sh user@server example.org
```

builds the linux binary and installs it with systemd, nginx and nightly
backups. [docs/deploy.md](docs/deploy.md) covers HTTPS, upgrades and
restoring a backup.

## Configuration

| Env var | Flag | Default |
|---|---|---|
| `EXTT_DB` | `--db` | `extt.db` |
| `EXTT_ADDR` | `--addr` | `127.0.0.1:8080` |
| `EXTT_SECURE_COOKIES` | `--secure-cookies` | `true` |
| `EXTT_TRUST_PROXY` | `--trust-proxy` | `false` (set `true` behind nginx so `X-Real-IP` is used for rate limiting) |
| `EXTT_BASE_URL` | `--base-url` | none (the site's address for full invite links, e.g. `https://example.org`; `serve` falls back to the request's host, the CLI prints only the path) |
| `EXTT_TZ` | `--tz` | `Europe/Kyiv` (time zone for showing and entering dates) |
| `EXTT_CIST_INTERVAL` | `--cist-interval` | `6h` (how often `serve` syncs schedules from CIST; `0` turns it off) |
| `EXTT_LOG_LEVEL` | `--log-level` | `info` (`debug`, `info`, `warn`, `error`; `debug` also logs the source line) |
| `EXTT_LOG_FORMAT` | `--log-format` | `text` (`json` for log collectors) |
| `EXTT_METRICS_ADDR` | `--metrics-addr` | none: off (address for Prometheus metrics at `/metrics`, e.g. `127.0.0.1:9101`; keep it on loopback) |

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
rejections, schedule syncs, joining and leaving groups, leader changes and,
at `debug`, CIST requests. Passwords, tokens (invite tokens are cut out of
`/join/…` paths), cookies, query strings, request bodies and homework
statuses or grades are never logged.

### Metrics

With `--metrics-addr` set, `serve` exposes Prometheus metrics at `/metrics`
on that address, a listener of its own that nginx does not proxy.
[docs/deploy.md](docs/deploy.md#monitoring) sets up Prometheus and a
Grafana dashboard on the server.

| Metric | What it counts |
|---|---|
| `extt_http_requests_total{method,route,code}` | Requests; `route` is the URL pattern (`/g/{code}/homework`), `unmatched` for 404s |
| `extt_http_request_duration_seconds{method,route}` | Response times (histogram) |
| `extt_http_requests_in_flight`, `extt_http_panics_total` | Requests being served; panics answered with 500 |
| `extt_logins_total{result}` | `success`, `failure`, `rate_limited` |
| `extt_registrations_total{result}` | `success`, `rate_limited` |
| `extt_csrf_rejections_total{reason}` | `cross_origin`, `no_cookie`, `no_token`, `mismatch` |
| `extt_schedule_syncs_total{result}`, `extt_schedule_sync_duration_seconds` | CIST syncs and how long they took |
| `extt_users`, `extt_groups`, `extt_signed_in_users`, `extt_sessions`, `extt_homework`, `extt_notes`, `extt_feedback_open`, `extt_db_size_bytes` | Read from the database on each scrape |
| `extt_schedule_last_success_timestamp_seconds{group}`, `extt_schedule_events{group}` | Each CIST-linked group's last good sync |

plus the Go runtime (`go_*`) and the process (`process_*`). Labels never
hold usernames, paths or anything else a client chooses.

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
`internal/i18n` with `locales/{uk,en}.toml` (translations, go-i18n),
`internal/cist` (CIST client: timetable CSV export, group lookup),
`internal/logging` (slog setup, request ID and user in every log line),
`internal/metrics` (Prometheus metrics),
`internal/server` (middleware wiring).

### Translations

Every UI string is a message ID in `locales/uk.toml` and `locales/en.toml`
(nested TOML keys: `[nav] homework = "…"` is `nav.homework`). Templates use
`{{t "nav.homework"}}`, or `{{th …}}` for messages holding markup; service
validation errors carry an `i18n.Message` so each page shows them in its
language. Tests check that both files have the same keys and that every ID
the code uses exists.

### JSON API (group content)

All under `/api/v1/groups/{code}`; unsafe methods need the `X-CSRF-Token`
header (get the token from `GET /api/v1/me`). `PUT /api/v1/me` with
`{"locale": "uk"|"en"}` changes the user's language; `/me` reports the one
in effect.

| Resource | Endpoints |
|---|---|
| `subjects` | `GET`, `POST`, `PUT /{id}`, `DELETE /{id}` (with `lecturer`, `instructor`, `dl_url`) |
| `class-links` | `GET`, `POST`, `PUT /{id}`, `DELETE /{id}` |
| `homework` | `GET`, `GET /{id}` (with links and rendered HTML), `POST`, `PUT /{id}`, `DELETE /{id}` |
| `notes` | `GET`, `POST`, `PUT /{id}`, `DELETE /{id}` |
| `resources` | `GET`, `POST`, `PUT /{id}`, `DELETE /{id}` (optional `lesson_type`) |
| `homework/{id}/progress` | `PUT` — the viewer's own `{"status", "grade"}` |
| `grades` | `GET` — the viewer's totals per subject and overall |
| `schedule` | `GET ?from=YYYY-MM-DD&to=YYYY-MM-DD` (default: the 7 days from today), `POST /sync` (leaders) |

Homework items carry the viewer's own `"progress": {"status", "grade"}`
(absent for a superadmin who is not a member of the group).

`PUT` changes only the fields present in the body; send `null` to clear
`due_at` or `max_points`, and `"links": []` to remove all homework links.
Create and update return the same object as `GET`. Validation errors return
`422` with `{"error", "code", "field"}`: the message in English and its
message ID, for a client that translates on its own; deleting a subject that is still in use
returns `409`.
