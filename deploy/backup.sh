#!/bin/sh
# Writes a consistent, gzipped snapshot of the database to $BACKUP_DIR and
# deletes snapshots older than $KEEP_DAYS days. Safe while the server runs.
# Run by extt-backup.service; `backup.sh pre-upgrade` labels the file.
set -eu

DB=${EXTT_DB:-/var/lib/extt/extt.db}
DIR=${BACKUP_DIR:-/var/backups/extt}
KEEP_DAYS=${KEEP_DAYS:-14}
EXTT=${EXTT_BIN:-/usr/local/bin/extt}

# find returns to the starting directory, which may be one extt cannot enter.
cd "$DIR"

name="extt-$(date +%Y-%m-%dT%H%M%S)${1:+-$1}.db"
tmp="$DIR/.$name"
trap 'rm -f "$tmp" "$tmp.gz"' EXIT

"$EXTT" backup --db "$DB" "$tmp" >/dev/null
gzip -9 "$tmp"
mv "$tmp.gz" "$DIR/$name.gz"
find "$DIR" -maxdepth 1 -name 'extt-*.db.gz' -mtime +"$KEEP_DAYS" -delete
echo "backup: $DIR/$name.gz"
