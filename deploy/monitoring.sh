#!/bin/sh
# Copies deploy/monitoring/ to the server and runs install-monitoring.sh
# there, which sets up Prometheus and Grafana next to extt:
#
#   deploy/monitoring.sh user@host [GRAFANA_URL]
#
# GRAFANA_URL is Grafana's public address behind a Cloudflare Tunnel, e.g.
# https://grafana.example.org (docs/deploy.md#monitoring).
# Run it after deploy.sh has installed extt; run it again after changing the
# dashboard or the Prometheus configuration.
set -eu
cd "$(dirname "$0")"

TARGET=${1:?usage: deploy/monitoring.sh user@host [GRAFANA_URL]}
GRAFANA_URL=${2:-}

echo "==> copying to $TARGET"
ssh "$TARGET" 'rm -rf ~/extt-monitoring && mkdir -m 700 ~/extt-monitoring'
scp -q install-monitoring.sh monitoring/* "$TARGET:extt-monitoring/"
ssh -t "$TARGET" "sudo ~/extt-monitoring/install-monitoring.sh '$GRAFANA_URL'"
