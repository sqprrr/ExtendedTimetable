# Deploying ExtendedTimetable

The site runs as one static binary behind nginx on a Debian or Ubuntu VPS
with systemd. Everything needed is in [`deploy/`](../deploy):

| File | Installed as | Purpose |
|---|---|---|
| `deploy.sh` | (runs on your machine) | Builds the linux binary, copies the bundle, runs `install.sh` |
| `install.sh` | (runs on the server) | Installs or upgrades everything below; safe to run again |
| `extt.service` | `/etc/systemd/system/` | The web server, sandboxed, as the `extt` user |
| `extt.env` | `/etc/extt/extt.env` | Settings (`EXTT_*`, backup options); created once, never overwritten |
| `nginx.conf` | `/etc/nginx/sites-available/extt` | Reverse proxy; created once so certbot's TLS lines survive upgrades |
| `extt-backup.service`, `.timer` | `/etc/systemd/system/` | Nightly backup at 03:30 |
| `backup.sh` | `/usr/local/lib/extt/` | Gzipped snapshot of the database, keeps 14 days |
| `exttctl` | `/usr/local/sbin/` | Runs the `extt` CLI as the service user against the live database |
| `monitoring.sh`, `install-monitoring.sh`, `monitoring/` | see [Monitoring](#monitoring) | Optional: Prometheus, node exporter and Grafana with the dashboard |

The database lives in `/var/lib/extt/extt.db`, backups in `/var/backups/extt`.

## First deployment

1. Allow key-based SSH once (it asks for the password), and make sure the
   user can `sudo`:

   ```sh
   ssh-copy-id user@server
   ```

2. Deploy from the repository root. The second argument is the domain (or IP
   address) nginx answers to; it defaults to the host:

   ```sh
   deploy/deploy.sh user@server example.org
   ```

   `install.sh` creates the `extt` system user, installs nginx if it is
   missing, starts the server and the backup timer, and waits for
   `/healthz`. sudo on the server may ask for the password.

3. HTTPS, with a domain pointing at the server:

   ```sh
   sudo apt install certbot python3-certbot-nginx
   sudo certbot --nginx -d example.org --redirect --hsts
   ```

   or with a [Cloudflare Tunnel](#cloudflare-tunnel) instead (no certificate
   on the server, no open ports).

   For a bare IP address `install.sh` sets `EXTT_SECURE_COOKIES=false`,
   because browsers drop Secure cookies over plain HTTP. Once HTTPS works,
   set it back to `true` in `/etc/extt/extt.env` and
   `sudo systemctl restart extt`.

4. Bootstrap (see the README for what each command does):

   ```sh
   sudo exttctl admin create-superadmin root
   sudo exttctl admin find-cist-group КІУКІ-25-3
   sudo exttctl admin create-group KIUKI-25-3 --cist-id 11881842 --name "КІУКІ-25-3"
   sudo exttctl admin sync-schedule KIUKI-25-3
   ```

## Cloudflare Tunnel

With the domain on Cloudflare, a tunnel serves the site over HTTPS without a
certificate on the server or ports 80/443 open to the internet. Cloudflare
terminates TLS; `cloudflared` on the server carries the traffic through an
encrypted tunnel and hands it to nginx over plain HTTP on localhost.

1. Deploy with the domain (`deploy/deploy.sh user@server example.org`). Skip
   certbot: with its `--redirect`, nginx would send the tunnel's HTTP
   requests back to HTTPS in a loop.

2. In Cloudflare Zero Trust, create a tunnel (Networks → Tunnels) and run
   the install command it shows on the server:

   ```sh
   sudo cloudflared service install <token>
   sudo systemctl status cloudflared
   ```

3. In the tunnel's **Public Hostname** tab, add `example.org` with service
   type `HTTP` and URL `localhost:80`. Cloudflare creates the DNS record;
   delete an existing `A`/`AAAA` record for the name first. Under the
   domain's SSL/TLS → Edge Certificates, turn on **Always Use HTTPS**.

4. Keep `EXTT_SECURE_COOKIES=true` in `/etc/extt/extt.env` (set it back if
   the server was first installed for an IP address) and
   `sudo systemctl restart extt`.

5. Check: `curl -I https://example.org/healthz` answers `200`, and
   `curl -I http://example.org` redirects to HTTPS.

cloudflared connects to nginx from localhost, so nginx takes the visitor's
address from Cloudflare's `CF-Connecting-IP` header, trusting it only from
`127.0.0.1` and `::1`; otherwise every user would share one login rate limit.
A server installed before this change needs the lines added by hand, since
`install.sh` never overwrites the nginx site:

```nginx
# in the server block of /etc/nginx/sites-available/extt:
set_real_ip_from 127.0.0.1;
set_real_ip_from ::1;
real_ip_header CF-Connecting-IP;
```

then `sudo nginx -t && sudo systemctl reload nginx`.

Once the tunnel works, the web ports can be closed (allow SSH first):
`sudo ufw allow OpenSSH && sudo ufw enable`.

## Upgrades

Run `deploy/deploy.sh user@server` again. Before the new version starts (and
migrates the database), `install.sh` writes a `…-pre-upgrade.db.gz` backup.
Settings and the nginx site are left as they are.

Settings added in a new version are therefore missing from an existing
`/etc/extt/extt.env`; compare it with `deploy/extt.env`. In particular, set
`EXTT_BASE_URL=https://example.org` (the site's public address) so that
`exttctl admin invite-link` prints full links; the site itself falls back to
the address each request came to.

### Static files and caches (Cloudflare)

No cache purge is needed after a deploy, in Cloudflare or anywhere else.
Pages link to every stylesheet, script and font by a hash of its content
(`/static/style.css?v=3f9a1c0b2e4d`; fonts are versioned inside
`style.css`), so a deploy that changes a file also changes its URL. The app
sends `Cache-Control: public, max-age=31536000, immutable` for a URL with the
current hash, and `no-cache` (with an `ETag`, so the check is a cheap 304)
for an unversioned URL or one with an old hash. A page cached before the
deploy therefore never pins a new file for long.

This relies on the query string being part of the cache key, which is
Cloudflare's default ("Standard" caching level). Keep it that way, and don't
add a Cache Rule that ignores the query string or overrides the origin's
`Cache-Control` for `/static/`.

## Monitoring

Prometheus and Grafana run on the same server, listening on loopback only:

```
extt :9101/metrics ─┐
node exporter :9100 ┼─▶ Prometheus :9090 (scrapes every 15 s, keeps 30 days) ─▶ Grafana :3000
```

extt serves its metrics on a separate address (`EXTT_METRICS_ADDR`) that
nginx does not proxy, so they never reach the internet. Install with:

```sh
deploy/deploy.sh user@server        # first, so extt knows --metrics-addr
deploy/monitoring.sh user@server
```

`install-monitoring.sh` sets `EXTT_METRICS_ADDR=127.0.0.1:9101` in
`/etc/extt/extt.env` and restarts extt, installs `prometheus` and
`prometheus-node-exporter` from the distribution and `grafana` from
apt.grafana.com, and provisions the Prometheus data source and the
**ExtendedTimetable** dashboard (also Grafana's home page). On the first run
it prints the `admin` password it generated (kept in
`/etc/grafana/extt-admin.env`); change it after logging in. Together they
need about 300 MB of RAM.

Open Grafana through an SSH tunnel:

```sh
ssh -L 3000:127.0.0.1:3000 user@server     # then http://localhost:3000
```

or on its own subdomain through the [Cloudflare Tunnel](#cloudflare-tunnel)
the site already uses:

1. Protect it first: in Cloudflare Zero Trust → Access → Applications, add a
   **Self-hosted** application for `grafana.example.org` with a policy
   *Allow* → *Emails* → your address. Visitors then have to enter a one-time
   code Cloudflare e-mails them before they even see Grafana's login page.
2. In the tunnel's **Public Hostname** tab, add `grafana.example.org` with
   service type `HTTP` and URL `localhost:3000`. It goes straight to Grafana,
   not through nginx; Cloudflare creates the DNS record.
3. Tell Grafana its address, so that its links, redirects and login checks
   use it:

   ```sh
   deploy/monitoring.sh user@server https://grafana.example.org
   ```

The metrics themselves stay off the internet either way:
`https://example.org/metrics` reaches nginx and extt's site address, where
there is no such page (404).

The dashboard shows the site's status, requests per minute, 5xx share and
response times, the busiest and slowest pages, logins, new accounts and
rejected (CSRF) requests, when each group's schedule last synced from CIST,
extt's memory and CPU, and the server's CPU, memory, disk and network.
Restarts (deploys) are marked on every graph.

Alert rules in `monitoring/extt-alerts.yml` (extt down, over 5% 5xx, slow
responses, a schedule not synced for 13 hours, a burst of failed logins,
disk almost full) show under Alerting → Alert rules in Grafana. To be
notified, add a contact point there (Telegram, e-mail, …) and an alert rule
that uses it.

`monitoring.sh` replaces the configuration and the dashboard on every run;
Prometheus' data and Grafana's users are kept. Change the dashboard in
`deploy/monitoring/extt.json` (or save a copy in Grafana: the provisioned
one cannot be saved over).

## Day to day

```sh
sudo systemctl status extt
sudo journalctl -u extt -f                 # server log
sudo exttctl admin invite-link KIUKI-25-3   # the group's invite link
sudo exttctl admin promote alice --group KIUKI-25-3
sudo systemctl start extt-backup           # back up now
systemctl list-timers extt-backup.timer    # next nightly backup
```

## Logs

The server logs to journald: one line per request, plus logins, failed
logins, CSRF rejections, schedule syncs and errors. The format and rules are
in [design.md §9](design.md#9-logging).

```sh
sudo journalctl -u extt -f                              # follow
sudo journalctl -u extt --since "1 hour ago" | grep -E 'level=(WARN|ERROR)'
sudo journalctl -u extt | grep 29846df684ca8618         # every line of one request
```

Every response carries an `X-Request-ID` header; when a user reports an
error, that ID finds its lines. (`journalctl -p` cannot filter by these
levels: journald sees every line from the app at the same priority.)

To debug, set `EXTT_LOG_LEVEL=debug` in `/etc/extt/extt.env`, run
`sudo systemctl restart extt`, and set it back afterwards.

nginx sets `X-Request-ID` itself so that clients cannot choose the ID. A
server installed before this change needs the line added by hand, since
`install.sh` never overwrites the nginx site:

```sh
# in the location / block of /etc/nginx/sites-available/extt:
proxy_set_header X-Request-ID $request_id;
```

then `sudo nginx -t && sudo systemctl reload nginx`.

journald keeps logs by its own system-wide limits. To keep about four weeks,
create `/etc/systemd/journald.conf.d/retention.conf`:

```ini
[Journal]
MaxRetentionSec=4week
SystemMaxUse=500M
```

and run `sudo systemctl restart systemd-journald`.

## Backups and restore

`backup.sh` uses `extt backup`, which takes a consistent copy with SQLite's
`VACUUM INTO` while the server runs; no `sqlite3` tool is needed. The copies
stay on the same disk, so also pull them elsewhere now and then, e.g. from
your machine:

```sh
rsync -a user@server:/var/backups/extt/ ./extt-backups/
```

(`/var/backups/extt` is readable only by `extt` and root; run rsync as a user
that may read it, or copy with sudo first.) For continuous off-site
replication, [Litestream](https://litestream.io) can stream the same
database to S3-compatible storage.

To restore:

```sh
sudo systemctl stop extt
gunzip -c /var/backups/extt/extt-2026-10-07T033012.db.gz | sudo -u extt tee /var/lib/extt/extt.db.restore >/dev/null
sudo -u extt mv /var/lib/extt/extt.db /var/lib/extt/extt.db.broken
sudo rm -f /var/lib/extt/extt.db-wal /var/lib/extt/extt.db-shm
sudo -u extt mv /var/lib/extt/extt.db.restore /var/lib/extt/extt.db
sudo systemctl start extt
```

## Checked

`install.sh` was tested in a Debian 12 container with systemd: a fresh
install, bootstrap through `exttctl`, a CIST sync, the backup unit, a second
run as an upgrade (with the pre-upgrade backup), and the site through nginx.
`systemd-analyze security extt.service` rates the sandbox 1.7 ("OK").
