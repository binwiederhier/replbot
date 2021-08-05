#!/bin/sh
set -eu
id replbot >/dev/null 2>&1 || useradd --system --no-create-home replbot
systemctl daemon-reload
if systemctl is-active -q replbot; then
  systemctl restart replbot
fi
exit 0
