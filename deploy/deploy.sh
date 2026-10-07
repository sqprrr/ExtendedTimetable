#!/bin/sh
# Builds extt for the server, copies it with the files of deploy/ and runs
# install.sh there:
#
#   deploy/deploy.sh user@host [SERVER_NAME]
#
# SERVER_NAME (domain or IP for nginx) defaults to the host part. sudo on the
# server may ask for the password. Needs key-based SSH (ssh-copy-id).
set -eu
cd "$(dirname "$0")/.."

TARGET=${1:?usage: deploy/deploy.sh user@host [SERVER_NAME]}
SERVER_NAME=${2:-${TARGET#*@}}

case $(ssh "$TARGET" uname -m) in
  x86_64) arch=amd64 ;;
  aarch64 | arm64) arch=arm64 ;;
  *) echo "deploy.sh: unsupported server architecture" >&2; exit 1 ;;
esac

bundle=$(mktemp -d)
trap 'rm -rf "$bundle"' EXIT
echo "==> building linux/$arch"
CGO_ENABLED=0 GOOS=linux GOARCH=$arch go build -trimpath -ldflags='-s -w' -o "$bundle/extt" ./cmd/extt
cp deploy/install.sh deploy/backup.sh deploy/exttctl deploy/extt.env deploy/nginx.conf \
  deploy/extt.service deploy/extt-backup.service deploy/extt-backup.timer "$bundle/"

echo "==> copying to $TARGET"
ssh "$TARGET" 'rm -rf ~/extt-deploy && mkdir -m 700 ~/extt-deploy'
scp -q "$bundle"/* "$TARGET:extt-deploy/"
ssh -t "$TARGET" "sudo ~/extt-deploy/install.sh '$SERVER_NAME'"
