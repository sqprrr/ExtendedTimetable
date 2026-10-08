#!/bin/sh
# Installs or upgrades Prometheus, the node exporter and Grafana next to
# ExtendedTimetable, all on loopback. Run as root from a directory holding the
# files of deploy/monitoring/ (monitoring.sh prepares it):
#
#   sudo ./install-monitoring.sh [GRAFANA_URL]
#
# GRAFANA_URL is Grafana's public address when it is served through a
# Cloudflare Tunnel or another proxy, e.g. https://grafana.example.org; it
# is kept for later runs without one. Leave it out for SSH-tunnel access.
# extt must already be installed (install.sh). The configuration files and
# the dashboard are replaced on every run; Prometheus' data and Grafana's
# database (users, saved dashboards) are kept.
set -eu
cd "$(dirname "$0")"

[ "$(id -u)" = 0 ] || { echo "install-monitoring.sh: run as root" >&2; exit 1; }
[ -e /etc/extt/extt.env ] || { echo "install-monitoring.sh: install extt first (install.sh)" >&2; exit 1; }

log() { printf '==> %s\n' "$*"; }
METRICS_ADDR=127.0.0.1:9101
GRAFANA_URL=${1:-}
case $GRAFANA_URL in
  "" | http://* | https://*) ;;
  *) echo "install-monitoring.sh: GRAFANA_URL must start with https:// (got $GRAFANA_URL)" >&2; exit 1 ;;
esac

log "pointing extt at $METRICS_ADDR"
if grep -q '^EXTT_METRICS_ADDR=' /etc/extt/extt.env; then
  sed -i "s|^EXTT_METRICS_ADDR=.*|EXTT_METRICS_ADDR=$METRICS_ADDR|" /etc/extt/extt.env
else
  printf '\n# Prometheus scrapes extt here (deploy/monitoring). Keep it on loopback.\nEXTT_METRICS_ADDR=%s\n' "$METRICS_ADDR" >> /etc/extt/extt.env
fi
systemctl restart extt.service

log "installing Prometheus and the node exporter"
apt-get update -q
DEBIAN_FRONTEND=noninteractive apt-get install -y -q prometheus prometheus-node-exporter apt-transport-https gnupg wget

# The Debian packages listen on every interface; keep them on loopback.
cat > /etc/default/prometheus <<'CONF'
# Written by ExtendedTimetable's install-monitoring.sh.
ARGS="--web.listen-address=127.0.0.1:9090 --storage.tsdb.retention.time=30d"
CONF
cat > /etc/default/prometheus-node-exporter <<'CONF'
# Written by ExtendedTimetable's install-monitoring.sh.
ARGS="--web.listen-address=127.0.0.1:9100"
CONF
install -m 0644 ./prometheus.yml /etc/prometheus/prometheus.yml
install -m 0644 ./extt-alerts.yml /etc/prometheus/extt-alerts.yml
if command -v promtool >/dev/null 2>&1; then
  promtool check config /etc/prometheus/prometheus.yml >/dev/null
fi
systemctl enable --quiet prometheus prometheus-node-exporter
systemctl restart prometheus-node-exporter prometheus

if ! dpkg -s grafana >/dev/null 2>&1; then
  log "installing Grafana from apt.grafana.com"
  install -d -m 0755 /etc/apt/keyrings
  wget -q -O - https://apt.grafana.com/gpg.key | gpg --dearmor --yes -o /etc/apt/keyrings/grafana.gpg
  echo "deb [signed-by=/etc/apt/keyrings/grafana.gpg] https://apt.grafana.com stable main" \
    > /etc/apt/sources.list.d/grafana.list
  apt-get update -q
  DEBIAN_FRONTEND=noninteractive apt-get install -y -q grafana
fi

first_password=
if [ ! -e /etc/grafana/extt-admin.env ] && [ ! -e /var/lib/grafana/grafana.db ]; then
  # Grafana reads this only when it creates its database, i.e. now.
  first_password=$(head -c 18 /dev/urandom | base64 | tr -d '/+=')
  umask 077
  printf 'GF_SECURITY_ADMIN_PASSWORD=%s\n' "$first_password" > /etc/grafana/extt-admin.env
  umask 022
fi

log "configuring Grafana"
install -d -m 0755 /etc/systemd/system/grafana-server.service.d /etc/grafana/dashboards
install -m 0644 ./grafana.conf /etc/systemd/system/grafana-server.service.d/extt.conf
install -m 0644 ./grafana-datasource.yml /etc/grafana/provisioning/datasources/extt.yml
install -m 0644 ./grafana-dashboards.yml /etc/grafana/provisioning/dashboards/extt.yml
install -m 0644 ./extt.json /etc/grafana/dashboards/extt.json
if [ -n "$GRAFANA_URL" ]; then
  # Behind a proxy Grafana must know its address: it builds links and
  # redirects from it and checks the Origin of logins against it.
  printf 'GF_SERVER_ROOT_URL=%s\n' "${GRAFANA_URL%/}/" > /etc/grafana/extt-url.env
fi
systemctl daemon-reload
systemctl enable --quiet grafana-server
systemctl restart grafana-server

up() {
  wget -q -O /dev/null "$1"
}
log "waiting for Prometheus, extt's metrics and Grafana"
for _ in $(seq 1 30); do
  if up http://127.0.0.1:9090/-/ready && up http://$METRICS_ADDR/metrics && up http://127.0.0.1:3000/api/health; then
    log "monitoring is running"
    if [ -n "$first_password" ]; then
      log "Grafana login: admin / $first_password (also in /etc/grafana/extt-admin.env)"
    fi
    if [ -e /etc/grafana/extt-url.env ]; then
      log "open $(sed -n 's/^GF_SERVER_ROOT_URL=//p' /etc/grafana/extt-url.env) (route it to http://localhost:3000 in the tunnel)"
    else
      log "open it through an SSH tunnel: ssh -L 3000:127.0.0.1:3000 <server>, then http://localhost:3000"
    fi
    exit 0
  fi
  sleep 1
done
echo "install-monitoring.sh: something did not come up; see systemctl status prometheus grafana-server extt" >&2
exit 1
