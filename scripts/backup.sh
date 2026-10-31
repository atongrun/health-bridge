#!/usr/bin/env bash
# Consistent database backup + age encryption + retention. The age public key
# lives in /etc/healthbridge/backup-recipient (root); the private key stays on
# the owner's Apple device. Without a recipient, backups stay plaintext 0600.
set -euo pipefail

CONFIG="/etc/healthbridge/config.json"
BACKUP_DIR="/var/lib/healthbridge/backups"
RECIPIENT_FILE="/etc/healthbridge/backup-recipient"

/opt/healthbridge/current/healthbridge backup -config "$CONFIG"

latest_db="$(ls -1t "$BACKUP_DIR"/*.db 2>/dev/null | head -1 || true)"
[ -n "$latest_db" ] || { echo "no backup produced" >&2; exit 1; }

if command -v age >/dev/null 2>&1 && [ -f "$RECIPIENT_FILE" ]; then
  age -R "$RECIPIENT_FILE" -o "$latest_db.age" "$latest_db"
  chmod 0600 "$latest_db.age"
  rm -f "$latest_db" "$latest_db.manifest.json"
  ls -1t "$BACKUP_DIR"/*.age 2>/dev/null | tail -n +8 | xargs -r rm -f
  echo "encrypted $latest_db -> $latest_db.age"
else
  echo "age/recipient not configured; backup left plaintext 0600: $latest_db"
fi
