#!/bin/sh
set -e
if ! getent passwd guardtower >/dev/null 2>&1; then
  useradd --system --home-dir /var/lib/guardtower --no-create-home --shell /usr/sbin/nologin guardtower
fi
