#!/usr/bin/env bash
# Action: echo
# Grammar:  @@<otp>#echo msg=<text>
# Args:     msg  (required) -- text to echo
# Reply:    "<sender> said: <msg>"
set -eu

msg="${GW_ARG_MSG:-}"
sender="${GW_SENDER_CALL:-?}"

if [[ -z "$msg" ]]; then
  echo "msg empty" >&2
  exit 1
fi

echo "$sender said: $msg"
