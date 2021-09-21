#!/bin/sh
set -eu
id replbot >/dev/null 2>&1 || useradd --system --create-home --home-dir /var/lib/replbot replbot
systemctl daemon-reload
if systemctl is-active -q replbot; then
  systemctl restart replbot
fi
if [ ! -f /etc/replbot/hostkey ] && which ssh-keygen > /dev/null; then
  ssh-keygen -q -N "" -f /etc/replbot/hostkey
  chown replbot.replbot /etc/replbot/hostkey*
fi
exit 0
