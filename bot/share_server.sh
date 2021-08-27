#!/bin/sh
# REPLbot script for terminal sharing (server-side)

case "$1" in
  run)
    cleanup() {
      rm -f "${REPLBOT_SSH_KEY_FILE}" "${REPLBOT_SSH_USER_FILE}"
      clear
      echo "Terminal sharing session has ended."
      sleep 1 # Let the output loop grab this script
    }
    trap cleanup EXIT
    echo "Hi there, you started a terminal sharing session."
    echo
    echo "To connect your terminal to REPLbot, run the command I sent to you in a DM."
    echo "Your terminal session will be shared here."
    while true; do
      echo
      echo "Waiting for client to connect ..."
      retry=1
      while [ -n "$retry" ]; do
        sleep 1
        if [ ! -f "${REPLBOT_SSH_USER_FILE}" ]; then
          continue
        fi
        ssh_user="$(cat "${REPLBOT_SSH_USER_FILE}")"
        if ! ssh -t -p "${REPLBOT_SSH_RELAY_PORT}" -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o "IdentityFile=${REPLBOT_SSH_KEY_FILE}" "${ssh_user}@127.0.0.1" 2>/dev/null; then
          continue
        fi
        clear
        echo "Terminal session ended. You may reconnect to it using the same"
        echo 'command, or type !exit to quit the session.'
        retry=
        rm -f "${REPLBOT_SSH_USER_FILE}"
      done
    done
    ;;
  kill)
    ;;
  *) echo "Syntax: $0 (run|kill) ID"; exit 1 ;;
esac


