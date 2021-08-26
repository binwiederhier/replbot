#!/bin/sh
# REPLbot script for terminal sharing (server-side)

case "$1" in
  run)
    echo "Hi there. Please run this command to create a local tmux session"
    echo "and connect it to REPLbot. Your tmux session will be shared here."
    echo
    echo "bash -c "'"'"\$(ssh -T -p ${REPLBOT_SSH_PORT} ${REPLBOT_SESSION_ID}@${REPLBOT_SSH_HOST} \$USER)"'"'
    echo
    echo "Waiting for client to connect ..."
    cleanup() {
      rm -f "${REPLBOT_SSH_KEY_FILE}"
      clear
      echo "Terminal sharing session has ended."
      sleep 1 # Let the output loop grab this script
    }
    trap cleanup EXIT
    while true; do
      if [ -f "${REPLBOT_SSH_USER_FILE}" ]; then
        ssh_user="$(cat "${REPLBOT_SSH_USER_FILE}")"
        break
      fi
      sleep 1
    done
    while true; do
      if ssh -t -p "${REPLBOT_RELAY_PORT}" -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o "IdentityFile=${REPLBOT_SSH_KEY_FILE}" "${ssh_user}@127.0.0.1" 2>/dev/null; then
        exit
      fi
      sleep 1
    done
    ;;
  kill)
    ;;
  *) echo "Syntax: $0 (run|kill) ID"; exit 1 ;;
esac


