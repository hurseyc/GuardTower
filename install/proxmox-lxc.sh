#!/usr/bin/env bash
# Create a GuardTower LXC container on a Proxmox VE host.
#
# Run on the Proxmox host as root:
#
#   bash -c "$(curl -fsSL https://raw.githubusercontent.com/hurseyc/GuardTower/main/install/proxmox-lxc.sh)"
#
# It creates an unprivileged Debian 12 container (1 vCPU, 512 MB RAM, 4 GB
# disk, DHCP on vmbr0 by default), installs GuardTower in it with install.sh
# and prints the address to open. Override defaults with environment
# variables, e.g.:
#
#   CTID=150 CT_HOSTNAME=guardtower STORAGE=local-lvm BRIDGE=vmbr1 IP=192.168.1.40/24 GW=192.168.1.1 \
#     bash -c "$(curl -fsSL https://raw.githubusercontent.com/hurseyc/GuardTower/main/install/proxmox-lxc.sh)"
#
# Variables: CTID (next free), CT_HOSTNAME (guardtower), STORAGE (local-lvm),
# TEMPLATE_STORAGE (local), BRIDGE (vmbr0), IP (dhcp), GW, VLAN, CORES (1),
# MEMORY (512), DISK (4, in GB), VERSION (latest), PASSWORD (random root password).
set -euo pipefail

say() { printf '\033[1;32m==>\033[0m %s\n' "$*"; }
die() { printf '\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }

[ "$(id -u)" -eq 0 ] || die "run as root on the Proxmox host"
command -v pct >/dev/null 2>&1 || die "pct not found: run this on a Proxmox VE host"

CTID="${CTID:-$(pvesh get /cluster/nextid)}"
# pct exec passes our locale into the container, which only has C.UTF-8;
# en_US.UTF-8 from an SSH session would make apt and perl print warnings.
export LANG=C.UTF-8 LC_ALL=C.UTF-8

CT_HOSTNAME="${CT_HOSTNAME:-guardtower}"
STORAGE="${STORAGE:-local-lvm}"
TEMPLATE_STORAGE="${TEMPLATE_STORAGE:-local}"
BRIDGE="${BRIDGE:-vmbr0}"
IP="${IP:-dhcp}"
GW="${GW:-}"
VLAN="${VLAN:-}"
CORES="${CORES:-1}"
MEMORY="${MEMORY:-512}"
DISK="${DISK:-4}"
VERSION="${VERSION:-latest}"
# Read a fixed amount first so no pipe is closed early (pipefail-safe).
PASSWORD="${PASSWORD:-$(head -c 96 /dev/urandom | tr -dc 'A-Za-z0-9' | cut -c1-20)}"
INSTALL_URL="${INSTALL_URL:-https://raw.githubusercontent.com/hurseyc/GuardTower/main/install/install.sh}"

if pct status "$CTID" >/dev/null 2>&1; then
  die "container $CTID already exists; set CTID to a free ID"
fi

say "Finding the Debian 12 template"
pveam update >/dev/null
TEMPLATE="$(pveam available --section system | awk '/debian-12-standard/ {print $2}' | sort -V | tail -n1)"
[ -n "$TEMPLATE" ] || die "no Debian 12 template is available from pveam"
if ! pveam list "$TEMPLATE_STORAGE" | grep -q "$TEMPLATE"; then
  say "Downloading $TEMPLATE"
  pveam download "$TEMPLATE_STORAGE" "$TEMPLATE" >/dev/null
fi

NET="name=eth0,bridge=$BRIDGE,ip=$IP"
[ -n "$GW" ] && NET="$NET,gw=$GW"
[ -n "$VLAN" ] && NET="$NET,tag=$VLAN"

say "Creating container $CTID ($CT_HOSTNAME)"
pct create "$CTID" "$TEMPLATE_STORAGE:vztmpl/$TEMPLATE" \
  --hostname "$CT_HOSTNAME" \
  --cores "$CORES" --memory "$MEMORY" --swap 256 \
  --rootfs "$STORAGE:$DISK" \
  --net0 "$NET" \
  --unprivileged 1 --features nesting=1 \
  --onboot 1 \
  --password "$PASSWORD" \
  --description "GuardTower: fleet manager for AdGuard Home (https://github.com/hurseyc/GuardTower)" >/dev/null

pct start "$CTID"
say "Waiting for the network"
for _ in $(seq 1 60); do
  if pct exec "$CTID" -- getent hosts github.com >/dev/null 2>&1; then break; fi
  sleep 1
done
pct exec "$CTID" -- getent hosts github.com >/dev/null 2>&1 || die "container $CTID has no network or DNS; check the bridge and IP settings"

say "Installing GuardTower in the container"
pct exec "$CTID" -- bash -c "apt-get update -qq && DEBIAN_FRONTEND=noninteractive apt-get install -y -qq curl ca-certificates >/dev/null"
pct exec "$CTID" -- bash -c "curl -fsSL '$INSTALL_URL' | bash -s -- --version '$VERSION'"

ADDR="$(pct exec "$CTID" -- hostname -I | awk '{print $1}')"
say "Done. Container $CTID is running GuardTower."
echo
echo "  Open:            http://${ADDR:-<container-ip>}:8080 and create the admin account"
echo "  Container login: root / $PASSWORD   (change it with: pct exec $CTID -- passwd)"
echo "  Upgrade later:   pct exec $CTID -- bash -c \"curl -fsSL '$INSTALL_URL' | bash -s -- --upgrade\""
