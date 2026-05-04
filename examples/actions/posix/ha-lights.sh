#!/usr/bin/env bash
# Action: ha-lights
# Grammar:  @@<otp>#ha-lights entity=light.<id> state=<on|off> [brightness=<0-255>]
# Args:     entity     (required) -- HA light entity_id
#           state      (required) -- on or off
#           brightness (optional) -- 0..255, only honored with state=on
# Reply:    "<entity> <state>"
# Config:   HA_URL    -- e.g. http://homeassistant.local:8123
#           HA_TOKEN  -- long-lived access token
# Deps:     curl
set -euo pipefail

entity="${GW_ARG_ENTITY:-}"
state="${GW_ARG_STATE:-}"
brightness="${GW_ARG_BRIGHTNESS:-}"

if [[ -z "$entity" || -z "$state" ]]; then
  echo "entity and state required" >&2
  exit 1
fi
: "${HA_URL:?HA_URL not set}"
: "${HA_TOKEN:?HA_TOKEN not set}"

case "$state" in
  on)  svc="turn_on"  ;;
  off) svc="turn_off" ;;
  *)   echo "state must be on or off" >&2; exit 1 ;;
esac

if [[ "$state" == "on" && -n "$brightness" ]]; then
  payload=$(printf '{"entity_id":"%s","brightness":%s}' "$entity" "$brightness")
else
  payload=$(printf '{"entity_id":"%s"}' "$entity")
fi

curl -fsS --max-time 6 \
  -H "Authorization: Bearer $HA_TOKEN" \
  -H "Content-Type: application/json" \
  -d "$payload" \
  "$HA_URL/api/services/light/$svc" -o /dev/null \
  || { echo "ha call failed" >&2; exit 1; }

echo "$entity $state"
