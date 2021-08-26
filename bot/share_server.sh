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
      clear
      echo "Terminal sharing session has ended."
      sleep 1 # Let the output loop grab this script
    }
    trap cleanup EXIT
    REPLBOT_SSH_USER=pheckel
    REPLBOT_SSH_IDENTITY_FILE="/tmp/replbot_${REPLBOT_SESSION_ID}.ssh_client_key"
    cat > "$REPLBOT_SSH_IDENTITY_FILE" <<EOF
-----BEGIN OPENSSH PRIVATE KEY-----
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
-----END OPENSSH PRIVATE KEY-----
EOF
    chmod 600 "$REPLBOT_SSH_IDENTITY_FILE"
    while true; do
      if ssh -t -p $REPLBOT_RELAY_PORT -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o "IdentityFile=$REPLBOT_SSH_IDENTITY_FILE" $REPLBOT_SSH_USER@127.0.0.1 2>/dev/null; then
        clear
        exit
      fi
      sleep 1
    done
    ;;
  kill)
    ;;
  *) echo "Syntax: $0 (run|kill) ID"; exit 1 ;;
esac


