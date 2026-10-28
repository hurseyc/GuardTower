#!/bin/sh
set -e
install -d -o guardtower -g guardtower -m 0750 /var/lib/guardtower
if command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]; then
  systemctl daemon-reload
  systemctl enable guardtower >/dev/null 2>&1 || true
  systemctl restart guardtower || true
  echo "GuardTower is running on port 8080. Open http://$(hostname -I 2>/dev/null | awk '{print $1}'):8080 to finish setup."
fi
