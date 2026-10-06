# ExtendedTimetable

A small hub for KHNURE student groups: class schedule, class links, homework,
notes, and a private homework tracker per student. See
[docs/design.md](docs/design.md) for the full design and milestones.

**Status:** M2 (leader tools) done.

- **M1:** open registration (anyone can join any group), login/logout, the
  admin CLI and a home page. Leaders (старости) are appointed only by the
  superadmin through the CLI.
- **M2:** each group has a page at `/g/<code>` with homework, notes, class
  links, recordings & solutions and subjects. Members read everything; the
  group's leaders and superadmins add, edit and delete it. Homework
  descriptions and notes are Markdown (sanitized). The same content is
  available as JSON under `/api/v1/groups/<code>/…`.

## Quick start (local)

Requires Go 1.26+.

```sh
go build -o extt ./cmd/extt

# Bootstrap: superadmin, group, leader
./extt admin create-superadmin root          # prompts for a password
./extt admin create-group KIUKI-25-3 --cist-id <id> --name "<display name>"

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
`reset-password`.

## Configuration

| Env var | Flag | Default |
|---|---|---|
| `EXTT_DB` | `--db` | `extt.db` |
| `EXTT_ADDR` | `--addr` | `127.0.0.1:8080` |
| `EXTT_SECURE_COOKIES` | `--secure-cookies` | `true` |
| `EXTT_TRUST_PROXY` | `--trust-proxy` | `false` (set `true` behind nginx so `X-Real-IP` is used for rate limiting) |
| `EXTT_TZ` | `--tz` | `Europe/Kyiv` (time zone for showing and entering due dates) |

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

Validation errors return `422` with `{"error", "field"}`; deleting a subject
that is still in use returns `409`.
