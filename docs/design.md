# ExtendedTimetable — Design Document (draft)

> Status: **draft / open for discussion**. Every decision below can be revisited.

## 1. Overview & goals

ExtendedTimetable is a small blog-like hub for KHNURE student groups. Each group gets a space with:

- its class schedule (pulled automatically from CIST),
- links to classes (meetings),
- homework assignments and notes from the group leader,
- links to class recordings and ready-made solutions,
- a private homework tracker for each student (status + grades).

**MVP scope:** one group — **KIUKI-25-3**. The data model is multi-group from day one (every group-owned entity has a `group_id`). Adding more groups later needs no redesign.

## 2. Roles & permissions

| Role | Who | How they get it |
|---|---|---|
| **Superadmin** | Site owner | Created through CLI on the server |
| **Group leader** | Class representative (староста) | Superadmin promotes a registered user with the server CLI (`extt admin promote`). There is no web UI for this |
| **Student** | Group member | Self-registers and picks their group. Every group is open to everyone |

A user belongs to a group through a membership. The membership carries the role (`student` or `leader`). Superadmin is a flag on the user.

### Permission matrix

| Action | Student | Leader (own group) | Superadmin |
|---|:-:|:-:|:-:|
| View schedule, homework, notes, links | ✅ | ✅ | ✅ |
| Set **own** homework status / grade | ✅ | ✅ | — |
| See other users' status / grades | ❌ | ❌ | ❌ |
| CRUD class links, homework, notes, recording & solution links | ❌ | ✅ | ✅ |
| Create groups, promote/demote leaders | ❌ | ❌ | ✅ (CLI only) |
| Reset a user's password | ❌ | ✅ (own group) | ✅ |

Homework status and grades are **private to the student**. Nobody else can see them, including the leader and the superadmin through the UI.

### Bootstrapping

```
extt admin create-superadmin <username>
extt admin create-group KIUKI-25-3 --cist-id <id>
extt admin promote <username> --group KIUKI-25-3
```

## 3. Onboarding & authentication

- **Registration:** username + password + **group**. Groups are open: anyone can register into any group and joins it as `student`. The form lists every group (for now only KIUKI-25-3, preselected when it is the only one); `/register?group=<code>` preselects a group for sharing in the group chat.
- **Leaders:** only the superadmin can appoint a leader, from the server CLI (`extt admin promote <username> --group <code>`). Nobody can become a leader through the website.
- **Login:** username + password. Passwords are hashed with **bcrypt**.
- **Sessions:** random session ID in an HttpOnly cookie, with sessions stored in the DB. That way they can be revoked, and the setup also works for a same-origin SPA later.
- **Password reset:** no email in MVP. The leader (for their group) or the superadmin sets a temporary password.

## 4. Features

### 4.1 Schedule (CIST auto-sync)
- A background job fetches the group's schedule from CIST periodically (e.g. every 6 h, plus manual "sync now" for the leader).
- Events are cached in `schedule_events`. If CIST is unavailable, the site shows the cached data with a "last synced at …" timestamp.
- Views: **today**, **week**, and the next class highlighted.
- Each event can show the matching **class link** (by subject + lesson type).
- ⚠️ To verify before implementing: the exact CIST endpoints and format (group lookup, e.g. `P_API_GROUP_JSON`, the events JSON or CSV export, `cp1251` encoding and known malformed-JSON quirks). Keep the CIST client isolated behind an interface so it can be swapped.

### 4.2 Class links
- The leader manages meeting links per subject and lesson type (lecture / practice / lab).

### 4.3 Homework
- The leader creates assignments with: subject, title, description (Markdown), due date, related links, and optional **max points**.
- Students see the list sorted by due date, with overdue items highlighted.
- Each student can set their **own**:
  - status: `Not started` (default) → `In progress` → `Done` (htmx toggle, no reload),
  - **grade** received (number, optional; capped at max points if set).
- "My grades" view: total points per subject (sum of grades / sum of max points) and overall.

### 4.4 Notes / announcements
- The leader posts notes (Markdown), optionally pinned to the top. This is the "blog" part.

### 4.5 Recordings & solutions
- **Links only, no file uploads.** Recordings (YouTube / Drive / Teams) and solutions (Drive / GitHub, etc.) are stored as links attached to a subject and an optional date.
- The earlier idea of ≤5 MB ZIP uploads is dropped for now. That keeps the server free of file storage and moderation.

## 5. Data model (SQLite)

```
groups            (id, code, name, cist_group_id, created_at)
users             (id, username UNIQUE, password_hash, is_superadmin, locale, created_at)
memberships       (user_id, group_id, role[student|leader], joined_at)   PK(user_id, group_id)
sessions          (id, user_id, expires_at, created_at)
subjects          (id, group_id, name, short_name)
class_links       (id, group_id, subject_id, lesson_type, url, note)
homework          (id, group_id, subject_id, title, description_md, due_at, max_points NULL, created_by, created_at, updated_at)
homework_links    (id, homework_id, title, url)
homework_progress (user_id, homework_id, status[not_started|in_progress|done], grade NULL, updated_at)  PK(user_id, homework_id)
notes             (id, group_id, title, body_md, pinned, created_by, created_at, updated_at)
resource_links    (id, group_id, subject_id, kind[recording|solution], title, url, date NULL, created_by, created_at)
schedule_events   (id, group_id, subject_id NULL, cist_event_id, starts_at, ends_at, lesson_type, room, teacher, raw_title, synced_at)
```

## 6. Tech stack

| Concern | Choice |
|---|---|
| Language | **Go** (single static binary) |
| HTTP | stdlib `net/http` (`ServeMux` with method/path patterns) |
| Pages | `html/template` + **htmx** for interactive bits |
| DB | **SQLite** via `modernc.org/sqlite` (pure Go, no CGO) |
| Migrations | Embedded SQL files, applied on startup |
| Assets | `embed` for templates, static files, locales |
| i18n | `go-i18n` with `uk` (default) and `en` message files; the user's choice is stored in `users.locale` with a cookie fallback |
| Markdown | `goldmark` + HTML sanitizer (`bluemonday`) |
| Reverse proxy | nginx + Let's Encrypt |

## 7. Architecture (ready for a future JS frontend)

```
            ┌──────────────┐     ┌──────────────┐
            │  web (HTML)  │     │ api (/api/v1)│   ← transports (thin)
            └──────┬───────┘     └──────┬───────┘
                   └─────────┬──────────┘
                      ┌──────▼──────┐
                      │   service   │   ← business rules + authorization
                      └──────┬──────┘
                      ┌──────▼──────┐     ┌──────────┐
                      │    store    │     │   cist   │ ← external client
                      └─────────────┘     └──────────┘
```

- All authorization and business rules live in **service**. Handlers only parse input and render output.
- The HTML handlers and the JSON API call the same services. Moving to Vue/Svelte/React later means building the frontend against `/api/v1` and retiring the templates. The backend logic stays the same.
- Cookie session auth works unchanged for a same-origin SPA.

### Project layout

```
cmd/extt/            main: serve, admin CLI subcommands
internal/
  auth/              password hashing, sessions, middleware, CSRF
  service/           domain logic + permission checks
  store/             SQLite queries
  web/               HTML handlers
  api/               JSON handlers (/api/v1)
  cist/              CIST client + sync job
  i18n/              locale loading/middleware
migrations/          *.sql
web/templates/       html/template files
web/static/          css, htmx.min.js
locales/             uk.toml, en.toml
docs/
```

## 8. Security

- Cookies: `HttpOnly`, `Secure`, `SameSite=Lax`.
- CSRF tokens on all state-changing forms and htmx requests.
- Login rate limiting (per IP + per username).
- Authorization enforced in the service layer, never only in templates.
- Markdown output sanitized.
- Users can only ever read or write their own `homework_progress` rows.

## 9. Deployment (VPS)

- Build: `CGO_ENABLED=0 go build -o extt ./cmd/extt`.
- Run as a **systemd** service under its own user. Config comes from env vars or a config file (listen addr, DB path, CIST sync interval).
- **nginx** reverse proxy with TLS from Let's Encrypt (certbot).
- **Backups:** nightly `sqlite3 extt.db ".backup ..."` via cron, or Litestream for continuous replication.

## 10. Milestones

1. **M1 — Skeleton & auth:** project layout, migrations, register (open groups) / login / logout, admin CLI.
2. **M2 — Leader tools:** CRUD for subjects, class links, homework, notes, recording/solution links.
3. **M3 — Student tracker:** homework status toggle, grades, "my grades" totals.
4. **M4 — Schedule:** CIST client, sync job, schedule views.
5. **M5 — i18n & deploy:** uk/en translations, systemd + nginx setup, backups.

## 11. Open questions

- What "additional conditions" should the leader be able to set (deadlines policy, grading rules, per-subject requirements)?
- Where will recordings live (YouTube unlisted, Drive, Teams)? Does access need restricting?
- Should students see a list of group members?
- Domain name for the VPS.
- How reliable is the CIST API? Do we need a manual CSV-import fallback?
- Should subjects come from CIST automatically, or be managed by the leader?
