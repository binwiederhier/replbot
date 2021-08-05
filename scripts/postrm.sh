#!/bin/sh
set -eu
systemctl stop replbot >/dev/null 2>&1 || true
if [ "$1" = "purge" ]; then
  id replbot >/dev/null 2>&1 && userdel replbot
  rm -rf /etc/replbot
fi
