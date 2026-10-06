# ExtendedTimetable

A small hub for KHNURE student groups: class schedule, class links, homework,
notes, and a private homework tracker per student. See
[docs/design.md](docs/design.md) for the full design and milestones.

**Status:** M1 (skeleton & auth) and M2 (leader tools) done. Open registration
(anyone can join any group), login/logout, the admin CLI and a home page.
Leaders (старости) are appointed only by the superadmin through the CLI. Leaders
manage subjects, class links, homework (with links), notes and recording or
solution links from the group page at `/groups/<code>`.

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
`internal/server` (middleware wiring).
