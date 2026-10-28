#!/usr/bin/env bash
# GuardTower installer for Linux (Debian, Ubuntu, Proxmox LXC, Raspberry Pi OS,
# Fedora, Alpine…). Installs the binary, a "guardtower" system user, a data
# directory and a systemd service.
#
#   curl -fsSL https://raw.githubusercontent.com/hurseyc/GuardTower/main/install/install.sh | sudo bash
#
# Options (pass after "bash -s --" when piping):
#   --version vX.Y.Z   install a specific release (default: latest)
#   --listen ADDR      listen address written to /etc/default/guardtower (default :8080)
#   --upgrade          replace the binary of an existing install, keep data and config
#   --uninstall        remove the service and binary (data is kept)
#   --purge            with --uninstall, also delete /var/lib/guardtower and the user
#   --no-start         install but don't enable/start the service
#   -h, --help         show this help
#
# The download is verified against the release's checksums.txt, and that file's
# cosign signature is verified when cosign is installed.
set -euo pipefail

REPO="hurseyc/GuardTower"
BASE_URL="${GT_INSTALL_BASE_URL:-https://github.com/${REPO}/releases}"
BIN=/usr/local/bin/guardtower
UNIT=/etc/systemd/system/guardtower.service
UPDATER_DIR=/usr/local/lib/guardtower
UPDATER_UNITS="/etc/systemd/system/guardtower-update.path /etc/systemd/system/guardtower-update.service"
DEFAULTS=/etc/default/guardtower
DATA=/var/lib/guardtower

VERSION=latest
LISTEN=":8080"
MODE=install
PURGE=0
START=1

say() { printf '\033[1;32m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33mwarning:\033[0m %s\n' "$*" >&2; }
die() { printf '\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }

usage() { sed -n '2,20p' "$0" 2>/dev/null | sed 's/^# \{0,1\}//'; exit 0; }

while [ $# -gt 0 ]; do
  case "$1" in
    --version) VERSION="${2:?--version needs a value}"; shift ;;
    --listen) LISTEN="${2:?--listen needs a value}"; shift ;;
    --upgrade) MODE=upgrade ;;
    --uninstall) MODE=uninstall ;;
    --purge) PURGE=1 ;;
    --no-start) START=0 ;;
    -h|--help) usage ;;
    *) die "unknown option $1 (see --help)" ;;
  esac
  shift
done

[ "$(id -u)" -eq 0 ] || die "run as root (e.g. with sudo)"
[ "$(uname -s)" = "Linux" ] || die "this installer supports Linux; use the Docker image or a release archive elsewhere"

has_systemd() { command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]; }

if [ "$MODE" = uninstall ]; then
  if has_systemd; then
    systemctl disable --now guardtower guardtower-update.path >/dev/null 2>&1 || true
  fi
  # shellcheck disable=SC2086 # UPDATER_UNITS is a list of paths
  rm -f "$UNIT" "$BIN" $UPDATER_UNITS
  rm -rf "$UPDATER_DIR"
  if has_systemd; then systemctl daemon-reload; fi
  if [ "$PURGE" = 1 ]; then
    rm -rf "$DATA" "$DEFAULTS"
    if getent passwd guardtower >/dev/null 2>&1; then userdel guardtower 2>/dev/null || true; fi
    say "GuardTower removed, including its data."
  else
    say "GuardTower removed. Data is kept in $DATA (use --uninstall --purge to delete it)."
  fi
  exit 0
fi

case "$(uname -m)" in
  x86_64|amd64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  armv7l|armv7|armhf) ARCH=armv7 ;;
  *) die "unsupported CPU architecture $(uname -m) (supported: amd64, arm64, armv7)" ;;
esac

if [ "$MODE" = upgrade ] && [ ! -x "$BIN" ]; then
  die "GuardTower isn't installed at $BIN; run without --upgrade"
fi

for tool in tar sha256sum; do command -v "$tool" >/dev/null 2>&1 || die "$tool is required"; done
if command -v curl >/dev/null 2>&1; then
  fetch() { curl -fsSL --retry 3 -o "$2" "$1"; }
elif command -v wget >/dev/null 2>&1; then
  fetch() { wget -q -O "$2" "$1"; }
else
  die "curl or wget is required"
fi

if [ "$VERSION" = latest ]; then
  URL="$BASE_URL/latest/download"
else
  case "$VERSION" in v*) ;; *) VERSION="v$VERSION" ;; esac
  URL="$BASE_URL/download/$VERSION"
fi
ASSET="guardtower_linux_${ARCH}.tar.gz"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
say "Downloading $ASSET ($VERSION)"
fetch "$URL/$ASSET" "$TMP/$ASSET" || die "download failed: $URL/$ASSET"
fetch "$URL/checksums.txt" "$TMP/checksums.txt" || die "download failed: $URL/checksums.txt"

if command -v cosign >/dev/null 2>&1; then
  if fetch "$URL/checksums.txt.sig" "$TMP/checksums.txt.sig" && fetch "$URL/checksums.txt.pem" "$TMP/checksums.txt.pem"; then
    cosign verify-blob --certificate "$TMP/checksums.txt.pem" --signature "$TMP/checksums.txt.sig" \
      --certificate-identity-regexp "^https://github.com/${REPO}/" \
      --certificate-oidc-issuer https://token.actions.githubusercontent.com \
      "$TMP/checksums.txt" >/dev/null 2>&1 || die "the checksums file's signature is not valid"
    say "Signature verified"
  else
    warn "no signature published for this release; relying on checksums only"
  fi
fi

( cd "$TMP" && grep " ${ASSET}\$" checksums.txt | sha256sum -c --status - ) || die "checksum mismatch for $ASSET"
say "Checksum verified"
tar -xzf "$TMP/$ASSET" -C "$TMP"
[ -x "$TMP/guardtower" ] || die "archive does not contain the guardtower binary"

if ! getent passwd guardtower >/dev/null 2>&1; then
  if command -v useradd >/dev/null 2>&1; then
    useradd --system --home-dir "$DATA" --no-create-home --shell /usr/sbin/nologin guardtower
  else
    adduser -S -D -H -h "$DATA" -s /sbin/nologin guardtower # Alpine/BusyBox
  fi
fi
install -d -o guardtower -g guardtower -m 0750 "$DATA"

if has_systemd && [ "$MODE" = upgrade ]; then
  systemctl stop guardtower || true
fi
install -m 0755 "$TMP/guardtower" "$BIN.new"
mv -f "$BIN.new" "$BIN"

if [ ! -f "$DEFAULTS" ]; then
  cat > "$DEFAULTS" <<EOF
# GuardTower settings (see "Configuration reference" in the in-app docs).
GT_LISTEN=$LISTEN
GT_DATA_DIR=$DATA
EOF
fi

# In-app updates: the web UI (running as the guardtower user) can only create
# $DATA/update/request; a root path unit then runs this installer with
# --upgrade, which downloads and verifies the latest release as usual.
install_updater() {
  [ -f "$TMP/install/install.sh" ] && [ -f "$TMP/install/guardtower-update.path" ] || return 0 # older archives
  install -d -m 0755 "$UPDATER_DIR"
  install -m 0755 "$TMP/install/install.sh" "$UPDATER_DIR/install.sh"
  cat > "$UPDATER_DIR/self-update" <<SCRIPT
#!/bin/sh
# Started by guardtower-update.service. Runs a copy of the installer, since
# the upgrade replaces $UPDATER_DIR/install.sh while it runs.
rm -f "$DATA/update/request"
tmp=\$(mktemp)
cp "$UPDATER_DIR/install.sh" "\$tmp"
date -u +%s > "$DATA/update/started"
bash "\$tmp" --upgrade > "$DATA/update/last.log" 2>&1
echo \$? > "$DATA/update/last.exit"
rm -f "\$tmp"
SCRIPT
  chmod 0755 "$UPDATER_DIR/self-update"
  install -d -o guardtower -g guardtower -m 0750 "$DATA/update"
  sed "s#@DATA@#$DATA#g" "$TMP/install/guardtower-update.path" > /etc/systemd/system/guardtower-update.path
  install -m 0644 "$TMP/install/guardtower-update.service" /etc/systemd/system/guardtower-update.service
}

if has_systemd; then
  if [ -f "$TMP/install/guardtower.service" ]; then
    sed "s#/usr/bin/guardtower#$BIN#" "$TMP/install/guardtower.service" > "$UNIT"
  else
    die "the archive is missing install/guardtower.service"
  fi
  install_updater
  systemctl daemon-reload
  if [ -f /etc/systemd/system/guardtower-update.path ]; then
    systemctl enable --now guardtower-update.path >/dev/null 2>&1 || warn "could not enable in-app updates"
  fi
  if [ "$START" = 1 ]; then
    systemctl enable guardtower >/dev/null 2>&1
    systemctl restart guardtower
  fi
else
  warn "systemd not found: start GuardTower yourself, e.g. 'sudo -u guardtower env GT_DATA_DIR=$DATA $BIN serve'"
fi

"$BIN" version || true
PORT="${LISTEN##*:}"
IP="$(hostname -I 2>/dev/null | awk '{print $1}')"
if [ "$MODE" = upgrade ]; then
  say "GuardTower upgraded."
else
  say "GuardTower is installed. Open http://${IP:-<this-host>}:${PORT:-8080} to create the admin account."
fi
