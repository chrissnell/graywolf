#!/bin/bash
# notify-freeform.sh -- Send a Pushover notification. Freeform Action.
#
# Wire it as an Action with arg_mode=freeform. Senders write:
#
#     @@<otp>#notify mowing the lawn brb
#
# Required env:
#   PUSHOVER_TOKEN   (application token from pushover.net)
#   PUSHOVER_USER    (user key)
#
# This is a second worked example to demonstrate that freeform isn't
# just for "phone-number space message" patterns -- you can take any
# blob of text and pass it directly to a downstream API.

set -euo pipefail

# Positional args captured by name for clarity.
# shellcheck disable=SC2034
ACTION="$1"
SENDER="$2"
OTP_VERIFIED="$3"
MESSAGE="$4"

# Defense in depth: refuse if the runtime did not verify an OTP.
# Push notifications are sent to a single operator so this is less
# critical than SMS, but the pattern keeps the example honest.
if [[ "$OTP_VERIFIED" != "true" ]]; then
    echo "otp required" >&2
    exit 65
fi

# Revalidate. Even though the Action regex should already catch this,
# this script's safety contract should not depend on operator
# configuration.
if [[ "$MESSAGE" =~ [[:cntrl:]] ]]; then
    echo "message contains control characters" >&2
    exit 65
fi
if (( ${#MESSAGE} < 1 || ${#MESSAGE} > 200 )); then
    echo "message length out of range (1..200)" >&2
    exit 65
fi

: "${PUSHOVER_TOKEN:?PUSHOVER_TOKEN not set}"
: "${PUSHOVER_USER:?PUSHOVER_USER not set}"

# Build the title from the sender's callsign so the operator can see
# who triggered the notification at a glance.
TITLE="aprs from ${SENDER}"

# Note the use of --data-urlencode for every operator-provided value.
# This is the same pattern as sms-freeform.sh -- never concatenate
# user-supplied bytes into a URL or a shell command.
response=$(curl -sS --max-time 8 -X POST \
    --data-urlencode "token=${PUSHOVER_TOKEN}" \
    --data-urlencode "user=${PUSHOVER_USER}" \
    --data-urlencode "title=${TITLE}" \
    --data-urlencode "message=${MESSAGE}" \
    -- "https://api.pushover.net/1/messages.json")

if printf '%s' "$response" | grep -q '"status":1'; then
    echo "pushed"
    exit 0
fi

echo "pushover rejected: $(printf '%s' "$response" | head -c 80)" >&2
exit 1
