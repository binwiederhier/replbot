#!/bin/sh
# REPLbot script for terminal sharing (server-side)

case "$1" in
  run)
    echo "Hi there. Please run this command to create a local tmux session"
    echo "and connect it to REPLbot. Your tmux session will be shared here."
    echo
    echo "bash -c "'"'"\$(ssh -p ${REPLBOT_SSH_PORT} ${REPLBOT_SESSION_ID}@${REPLBOT_SSH_HOST})"'"'
    echo
    echo "Waiting for client to connect ..."
    cleanup() {
      clear
      echo "Session exited."
    }
    trap cleanup EXIT
    while true; do
      if socat file:$(tty),raw,echo=0 tcp-connect:localhost:${REPLBOT_RELAY_PORT} 2>/dev/null; then
        exit
      fi
      sleep 1
    done
    ;;
  kill)
    ;;
  *) echo "Syntax: $0 (run|kill) ID"; exit 1 ;;
esac


