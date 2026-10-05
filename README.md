# ExtendedTimetable

A small hub for KHNURE student groups: class schedule, class links, homework,
notes, and a private homework tracker per student. See
[docs/design.md](docs/design.md) for the full design and milestones.

**Status:** M1 (skeleton & auth) done — invite-code registration,
login/logout, the admin CLI, and a home page where leaders can see and
regenerate the group invite code.

## Quick start (local)

Requires Go 1.26+.

```sh
go build -o extt ./cmd/extt

# Bootstrap: superadmin, group, leader
./extt admin create-superadmin root          # prompts for a password
./extt admin create-group KIUKI-25-3 --cist-id <id>   # prints the invite code

# Run over plain HTTP locally (Secure cookies need HTTPS)
./extt serve --secure-cookies=false          # http://127.0.0.1:8080
```

Register at `/register` with the invite code (or share
`/register?code=XXXX-XXXX-XXXX`), then make that user the group leader:

```sh
./extt admin promote <username> --group KIUKI-25-3
```

Run `./extt help` for all commands. Other admin commands: `demote`,
`invite-code [--regenerate]`, `reset-password`.

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
