#!/bin/sh
# Installs or upgrades ExtendedTimetable on a Debian/Ubuntu host with systemd.
# Run as root from a directory holding the linux `extt` binary and the files
# of deploy/ (deploy.sh prepares it):
#
#   sudo ./install.sh SERVER_NAME
#
# SERVER_NAME is the domain (or IP address) nginx answers to. Configuration
# files (/etc/extt/extt.env, the nginx site) are created only once; the
# binary, scripts and systemd units are replaced on every run. An existing
# database is backed up before the new version starts and migrates it.
set -eu
cd "$(dirname "$0")"

SERVER_NAME=${1:?usage: install.sh SERVER_NAME (domain or IP address)}
[ "$(id -u)" = 0 ] || { echo "install.sh: run as root" >&2; exit 1; }
[ -x ./extt ] || { echo "install.sh: ./extt binary missing" >&2; exit 1; }

log() { printf '==> %s\n' "$*"; }

if ! id extt >/dev/null 2>&1; then
  log "creating system user extt"
  useradd --system --home-dir /var/lib/extt --no-create-home --shell /usr/sbin/nologin extt
fi
install -d -o extt -g extt -m 0750 /var/lib/extt /var/backups/extt
install -d -m 0755 /etc/extt /usr/local/lib/extt

log "installing binary and scripts"
install -m 0755 ./extt /usr/local/bin/extt.new
mv /usr/local/bin/extt.new /usr/local/bin/extt
install -m 0755 ./backup.sh /usr/local/lib/extt/backup.sh
install -m 0755 ./exttctl /usr/local/sbin/exttctl

case $SERVER_NAME in
  *[!0-9.]*) is_ip=false ;;
  *) is_ip=true ;;
esac
if [ ! -e /etc/extt/extt.env ]; then
  log "creating /etc/extt/extt.env"
  install -m 0640 -g extt ./extt.env /etc/extt/extt.env
  scheme=https
  if $is_ip; then
    # No certificate for a bare IP address: serve plain HTTP.
    sed -i 's/^EXTT_SECURE_COOKIES=true/EXTT_SECURE_COOKIES=false/' /etc/extt/extt.env
    scheme=http
  fi
  sed -i "s|^EXTT_BASE_URL=.*|EXTT_BASE_URL=$scheme://$SERVER_NAME|" /etc/extt/extt.env
fi

# The running server has not migrated yet: back up the old schema. The new
# binary's backup command does not migrate.
if [ -f /var/lib/extt/extt.db ]; then
  log "backing up the database before the upgrade"
  # Run from a directory extt can enter: this script's directory is usually
  # the deploying user's private home.
  (cd /var/lib/extt && set -a && . /etc/extt/extt.env && set +a &&
    runuser -u extt -- /usr/local/lib/extt/backup.sh pre-upgrade)
fi

log "installing systemd units"
install -m 0644 ./extt.service ./extt-backup.service ./extt-backup.timer /etc/systemd/system/
systemctl daemon-reload
systemctl enable --quiet extt.service extt-backup.timer
systemctl restart extt.service
systemctl start extt-backup.timer

if ! command -v nginx >/dev/null 2>&1; then
  log "installing nginx"
  apt-get update -q && DEBIAN_FRONTEND=noninteractive apt-get install -y -q nginx
fi
if [ ! -e /etc/nginx/sites-available/extt ]; then
  log "creating the nginx site for $SERVER_NAME"
  sed "s/SERVER_NAME/$SERVER_NAME/" ./nginx.conf > /etc/nginx/sites-available/extt
  ln -sf /etc/nginx/sites-available/extt /etc/nginx/sites-enabled/extt
fi
nginx -t -q
systemctl enable --quiet nginx
systemctl reload nginx || systemctl start nginx

healthy() {
  if command -v curl >/dev/null 2>&1; then
    curl -fsS http://127.0.0.1:8080/healthz >/dev/null 2>&1
  else
    wget -q -O /dev/null http://127.0.0.1:8080/healthz
  fi
}
log "waiting for the server"
for _ in 1 2 3 4 5 6 7 8 9 10; do
  if healthy; then
    log "extt is running"
    if $is_ip; then
      log "open http://$SERVER_NAME/"
    else
      log "open http://$SERVER_NAME/ — for HTTPS run: certbot --nginx -d $SERVER_NAME --redirect --hsts"
      log "or route a Cloudflare Tunnel to http://localhost:80 (docs/deploy.md)"
    fi
    exit 0
  fi
  sleep 1
done
echo "install.sh: extt did not come up; see: journalctl -u extt -n 50" >&2
exit 1
