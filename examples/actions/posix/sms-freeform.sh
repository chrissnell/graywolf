#!/bin/bash
# sms-freeform.sh -- Send an SMS via Twilio. Freeform Action variant.
#
# Wire it as an Action with arg_mode=freeform. Senders write:
#
#     @@<otp>#sms +15555551212 hello there
#
# The runner invokes this script as:
#
#     sms-freeform.sh sms KE0XYZ true "+15555551212 hello there"
#                     ^   ^      ^   ^
#                     |   |      |   GW_ARG (positional $4): the entire
#                     |   |      |   freeform payload, no "text=" prefix
#                     |   |      OTP_VERIFIED: "true" or "false"
#                     |   GW_SENDER_CALL: APRS callsign that triggered us
#                     GW_ACTION_NAME: always "sms" here
#
# Required environment (set in the systemd unit, NOT in this script):
#   TWILIO_ACCOUNT_SID
#   TWILIO_AUTH_TOKEN
#   TWILIO_FROM            (your Twilio number, e.g. +14155551111)
#
# Defense layers (left-to-right, fail-fast):
#   1. set -euo pipefail              -- abort on any error or unset var
#   2. quote every expansion          -- no word-splitting / globbing
#   3. revalidate inputs              -- Action regex is one layer; this is two
#   4. no eval, no sh -c              -- argv-style exec only
#   5. -- terminator before user data -- no flag injection into curl

set -euo pipefail

# Positional args captured by name for clarity; only PAYLOAD is used here.
# shellcheck disable=SC2034
ACTION="$1"
# shellcheck disable=SC2034
SENDER="$2"
# shellcheck disable=SC2034
OTP_VERIFIED="$3"
PAYLOAD="$4"

# --- 1. Split number from message body --------------------------------
#
# We expect:  +<E.164 digits><single space><message>
#
# Bash parameter expansion ${VAR%% PAT} removes the LONGEST suffix
# matching PAT, so "${PAYLOAD%% *}" yields everything up to the FIRST
# space (the number). "${PAYLOAD#* }" removes the SHORTEST prefix
# matching "anything followed by a space", yielding everything after
# the first space (the message).
#
# Example:
#   PAYLOAD="+15555551212 hello there"
#   "${PAYLOAD%% *}" -> "+15555551212"
#   "${PAYLOAD#* }"  -> "hello there"
#
# This is purely string surgery in the shell -- no eval, no subshell,
# no command substitution. An attacker cannot inject shell commands
# through this expansion.

NUMBER="${PAYLOAD%% *}"
MESSAGE="${PAYLOAD#* }"

# Edge case: no space in payload means we never split. Reject explicitly.
if [[ "$NUMBER" == "$PAYLOAD" ]]; then
    echo "expected '+<E164> <message>'" >&2
    exit 64
fi

# --- 2. Revalidate the number -----------------------------------------
#
# Bash regex (POSIX ERE flavor under [[ =~ ]]). The pattern requires:
#   ^\+        a literal '+'
#   [1-9]      a single digit 1..9 (E.164 doesn't allow leading 0)
#   [0-9]{6,14} 6 to 14 more digits
#   $          end of string
# Total length: 8..16 characters.
#
# We do this even though the Action's arg_schema regex should already
# reject malformed numbers -- defense in depth. The script's contract
# does not depend on the operator picking a strict regex.

if [[ ! "$NUMBER" =~ ^\+[1-9][0-9]{6,14}$ ]]; then
    echo "invalid E.164: $NUMBER" >&2
    exit 65
fi

# --- 3. Revalidate the message ----------------------------------------
#
# [[:cntrl:]] is the POSIX character class for ASCII control characters
# (0x00..0x1F plus 0x7F). The graywolf sanitizer already strips these,
# but checking again here means the script remains safe even if the
# operator widens the Action regex.

if [[ "$MESSAGE" =~ [[:cntrl:]] ]]; then
    echo "message contains control characters" >&2
    exit 65
fi
if (( ${#MESSAGE} < 1 || ${#MESSAGE} > 160 )); then
    echo "message length out of range (1..160)" >&2
    exit 65
fi

# --- 4. Verify required env (helpful failure mode) --------------------
: "${TWILIO_ACCOUNT_SID:?TWILIO_ACCOUNT_SID not set}"
: "${TWILIO_AUTH_TOKEN:?TWILIO_AUTH_TOKEN not set}"
: "${TWILIO_FROM:?TWILIO_FROM not set}"

# --- 5. Send -----------------------------------------------------------
#
# curl is invoked argv-style, every variable quoted, and we use
# --data-urlencode so curl performs URL-encoding of values (no shell
# concatenation of escaped strings). The "--" separator before the URL
# is conventional for curl; here the URL doesn't start with -, but we
# still keep the habit.
#
# We send the response to stdout (truncated to one line by graywolf),
# so a successful send echoes the Twilio message SID into the on-air
# reply ("ok: SM<sid>...").

response=$(curl -sS --max-time 8 -X POST \
    --data-urlencode "From=${TWILIO_FROM}" \
    --data-urlencode "To=${NUMBER}" \
    --data-urlencode "Body=${MESSAGE}" \
    -u "${TWILIO_ACCOUNT_SID}:${TWILIO_AUTH_TOKEN}" \
    -- "https://api.twilio.com/2010-04-01/Accounts/${TWILIO_ACCOUNT_SID}/Messages.json")

# Cheap success heuristic: Twilio returns a JSON document with a "sid"
# field on success. We don't pull in jq because not every operator has
# it; grep is enough for a one-line reply.
if printf '%s' "$response" | grep -q '"sid"'; then
    sid=$(printf '%s' "$response" | grep -o '"sid":"SM[A-Za-z0-9]*"' | head -n1 | sed 's/.*"\(SM[^"]*\)"/\1/')
    echo "sent ${sid:-?}"
    exit 0
fi

echo "twilio rejected: $(printf '%s' "$response" | head -c 80)" >&2
exit 1
