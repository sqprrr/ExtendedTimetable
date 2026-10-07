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

3. HTTPS (needs a domain pointing at the server):

   ```sh
   sudo apt install certbot python3-certbot-nginx
   sudo certbot --nginx -d example.org --redirect --hsts
   ```

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

## Upgrades

Run `deploy/deploy.sh user@server` again. Before the new version starts (and
migrates the database), `install.sh` writes a `…-pre-upgrade.db.gz` backup.
Settings and the nginx site are left as they are.

## Day to day

```sh
sudo systemctl status extt
sudo journalctl -u extt -f                 # server log
sudo exttctl admin promote alice --group KIUKI-25-3
sudo systemctl start extt-backup           # back up now
systemctl list-timers extt-backup.timer    # next nightly backup
```

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
