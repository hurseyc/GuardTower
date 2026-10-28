#!/bin/sh
set -e
if command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]; then
  systemctl stop guardtower || true
  systemctl disable guardtower >/dev/null 2>&1 || true
fi
