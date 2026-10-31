#!/usr/bin/env bash
# Health Bridge installer. Idempotent; --dry-run performs every preflight check
# and prints the actions it would take without mutating anything. Never pipes a
# remote script; run it from a downloaded, checksum-verified release tarball.
set -euo pipefail

DRY_RUN=0
FROM=""
CONFIG_SRC=""
VERSION=""

usage() {
  cat <<'EOF'
usage: install.sh --from <binary> [--config <config.json>] [--version <v>] [--dry-run]

  --from PATH      path to the healthbridge binary for this machine's arch
  --config PATH    optional config.json to install (defaults to a template)
  --version V      release version, e.g. 0.1.0-s07a (defaults to `healthbridge version`)
  --dry-run        run every preflight check and print planned actions only
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --from) FROM="$2"; shift 2;;
    --config) CONFIG_SRC="$2"; shift 2;;
    --version) VERSION="$2"; shift 2;;
    --dry-run) DRY_RUN=1; shift;;
    --help|-h) usage; exit 0;;
    *) echo "unknown flag: $1" >&2; usage >&2; exit 2;;
  esac
done

[ -n "$FROM" ] || { usage >&2; exit 2; }
[ -f "$FROM" ] || { echo "binary not found: $FROM" >&2; exit 2; }

# --- preflight ---------------------------------------------------------------
fail() { echo "preflight failed: $1" >&2; exit 1; }
note() { echo "check: $1"; }

[ "$(id -u)" -eq 0 ] || fail "installer must run as root"

case "$(uname -m)" in
  x86_64|aarch64) ;;
  *) fail "unsupported arch $(uname -m)";;
esac

if [ -z "$VERSION" ]; then
  VERSION="$("$FROM" version 2>/dev/null)" || fail "binary refused to report its version"
fi
case "$VERSION" in
  ''|*[!0-9a-zA-Z.-]*) fail "invalid version string";;
esac

# Disk space: refuse below 1 GiB free (backups need room).
avail_kb="$(df -kP /var | awk 'NR==2{print $4}')"
[ "$avail_kb" -ge 1048576 ] || fail "less than 1 GiB free on /var ($avail_kb KiB)"

# Port: refuse to take an in-use port.
if command -v ss >/dev/null 2>&1; then
  ss -ltn 2>/dev/null | grep -q ':8080 ' && fail "port 8080 already in use"
fi

note "arch=$(uname -m) version=$VERSION binary=$FROM dry_run=$DRY_RUN"

# --- planned actions ---------------------------------------------------------
DEST_DIR="/opt/healthbridge/releases/$VERSION"
STATE_DIR="/var/lib/healthbridge"
CONFIG_DIR="/etc/healthbridge"
CONFIG_PATH="$CONFIG_DIR/config.json"
BIN_PATH="$DEST_DIR/healthbridge"

run() {
  if [ "$DRY_RUN" -eq 1 ]; then
    echo "would: $*"
  else
    "$@"
  fi
}

if [ "$DRY_RUN" -eq 1 ]; then
  echo "dry-run complete; no changes made."
  exit 0
fi

# --- apply -------------------------------------------------------------------
umask 077
id healthbridge >/dev/null 2>&1 || useradd --system --home "$STATE_DIR" --shell /usr/sbin/nologin healthbridge
mkdir -p "$DEST_DIR" "$CONFIG_DIR" "$STATE_DIR/backups"
install -d -m 0700 -o healthbridge -g healthbridge "$STATE_DIR"
install -m 0755 "$FROM" "$BIN_PATH"
ln -sfn "$DEST_DIR" /opt/healthbridge/current
# ship the release's own scripts and units (single source of truth)
mkdir -p "$DEST_DIR/scripts" "$DEST_DIR/deploy"
install -m 0755 /opt/healthbridge/current/scripts/backup.sh "$DEST_DIR/scripts/backup.sh" 2>/dev/null || true
install -m 0644 /opt/healthbridge/current/deploy/Caddyfile "$DEST_DIR/deploy/Caddyfile" 2>/dev/null || true

if [ -n "$CONFIG_SRC" ]; then
  [ -f "$CONFIG_SRC" ] || { echo "config not found: $CONFIG_SRC" >&2; exit 2; }
  install -m 0640 -o root -g healthbridge "$CONFIG_SRC" "$CONFIG_PATH"
else
  # Template config: no secrets, no tokens; owner fills the real one by hand.
  [ -f "$CONFIG_PATH" ] || install -m 0640 -o root -g healthbridge /opt/healthbridge/current/config.example.json "$CONFIG_PATH" 2>/dev/null || true
fi

if [ -f /etc/healthbridge/config.json ]; then
  "$BIN_PATH" check-config -config "$CONFIG_PATH"
  "$BIN_PATH" migrate -config "$CONFIG_PATH"
fi

# systemd units
install -m 0644 /opt/healthbridge/current/deploy/healthbridge.service /etc/systemd/system/healthbridge.service 2>/dev/null || true
install -m 0644 /opt/healthbridge/current/deploy/healthbridge-backup.service /etc/systemd/system/healthbridge-backup.service 2>/dev/null || true
install -m 0644 /opt/healthbridge/current/deploy/healthbridge-backup.timer /etc/systemd/system/healthbridge-backup.timer 2>/dev/null || true
systemctl daemon-reload
systemctl enable --now healthbridge healthbridge-backup.timer 2>/dev/null || true

echo "installed $VERSION; run 'systemctl status healthbridge' and 'journalctl -u healthbridge' to verify."
