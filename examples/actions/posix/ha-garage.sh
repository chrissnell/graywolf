#!/usr/bin/env bash
# Action: ha-garage
# Grammar:  @@<otp>#ha-garage action=<open|close|toggle>
# Args:     action  (required) -- open, close, or toggle
# Reply:    "garage <action>"
# Config:   HA_URL, HA_TOKEN, HA_GARAGE_ENTITY (e.g. cover.garage_door)
# Deps:     curl
#
# DO NOT install without OTPRequired=true and a sender allowlist.
set -eu

action="${GW_ARG_ACTION:-}"
if [[ -z "$action" ]]; then
  echo "action required" >&2
  exit 1
fi
: "${HA_URL:?HA_URL not set}"
: "${HA_TOKEN:?HA_TOKEN not set}"
: "${HA_GARAGE_ENTITY:?HA_GARAGE_ENTITY not set}"

case "$action" in
  open)   svc="open_cover"  ;;
  close)  svc="close_cover" ;;
  toggle) svc="toggle"      ;;
  *)      echo "action must be open|close|toggle" >&2; exit 1 ;;
esac

payload=$(printf '{"entity_id":"%s"}' "$HA_GARAGE_ENTITY")

curl -fsS --max-time 6 \
  -H "Authorization: Bearer $HA_TOKEN" \
  -H "Content-Type: application/json" \
  -d "$payload" \
  "$HA_URL/api/services/cover/$svc" -o /dev/null \
  || { echo "ha call failed" >&2; exit 1; }

echo "garage $action"
