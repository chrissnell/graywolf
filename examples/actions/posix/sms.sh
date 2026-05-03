#!/usr/bin/env bash
# Action: sms
# Grammar:  @@<otp>#sms to=<E164> msg=<text>
# Args:     to   (required) -- destination phone in E.164 form (+15551234567)
#           msg  (required) -- message body
# Reply:    "sms sent to <to>"
# Behavior: sends one SMS via Twilio. If APRSFI_API_KEY is set, also looks up
#           the sender's last APRS position and sends a second SMS with a
#           Google Maps link.
# Config:   TWILIO_ACCOUNT_SID, TWILIO_AUTH_TOKEN, TWILIO_FROM (E.164),
#           APRSFI_API_KEY (optional)
# Deps:     curl, jq
set -eu

to="${GW_ARG_TO:-}"
msg="${GW_ARG_MSG:-}"
sender="${GW_SENDER_CALL:-?}"

if [[ -z "$to" || -z "$msg" ]]; then
  echo "to and msg required" >&2
  exit 1
fi
: "${TWILIO_ACCOUNT_SID:?TWILIO_ACCOUNT_SID not set}"
: "${TWILIO_AUTH_TOKEN:?TWILIO_AUTH_TOKEN not set}"
: "${TWILIO_FROM:?TWILIO_FROM not set}"

send_sms() {
  curl -fsS --max-time 8 \
    -u "$TWILIO_ACCOUNT_SID:$TWILIO_AUTH_TOKEN" \
    -X POST "https://api.twilio.com/2010-04-01/Accounts/$TWILIO_ACCOUNT_SID/Messages.json" \
    --data-urlencode "From=$TWILIO_FROM" \
    --data-urlencode "To=$to" \
    --data-urlencode "Body=$1" \
    -o /dev/null
}

send_sms "From $sender (APRS): $msg" || { echo "twilio failed" >&2; exit 1; }

# Best-effort location follow-up.
if [[ -n "${APRSFI_API_KEY:-}" ]]; then
  loc=$(curl -fsSL --max-time 6 \
    "https://api.aprs.fi/api/get?name=${sender}&what=loc&apikey=${APRSFI_API_KEY}&format=json" \
    2>/dev/null) || loc=""
  if [[ -n "$loc" ]]; then
    lat=$(printf '%s' "$loc" | jq -r '.entries[0].lat // empty' 2>/dev/null || true)
    lng=$(printf '%s' "$loc" | jq -r '.entries[0].lng // empty' 2>/dev/null || true)
    if [[ -n "$lat" && -n "$lng" ]]; then
      send_sms "Location of $sender: https://maps.google.com/?q=${lat},${lng}" || true
    fi
  fi
fi

echo "sms sent to $to"
