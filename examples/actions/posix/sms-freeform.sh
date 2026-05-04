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

# Positional args captured by name for clarity.
# shellcheck disable=SC2034
ACTION="$1"
# shellcheck disable=SC2034
SENDER="$2"
OTP_VERIFIED="$3"
PAYLOAD="$4"

# --- 0. Refuse without a verified OTP --------------------------------
#
# SMS sends are exactly the surface that should require the per-Action
# OTP toggle to be set. The runtime should not have invoked us if OTP
# was missing -- this is defense in depth so the script's contract
# does not depend on operator configuration.
if [[ "$OTP_VERIFIED" != "true" ]]; then
    echo "otp required" >&2
    exit 65
fi

# --- 1. Validate + split in one regex ---------------------------------
#
# Bash regex (POSIX ERE flavor under [[ =~ ]]) with capture groups.
# Pattern:
#   ^                        anchor at start
#   (\+[1-9][0-9]{6,14})     group 1: E.164 number
#                              \+        literal '+'
#                              [1-9]     leading digit (E.164 forbids 0)
#                              [0-9]{6,14} 6-14 more digits (8-16 total)
#   [[:space:]]+             one or more whitespace separators
#   (.+)                     group 2: message body (non-empty)
#   $                        anchor at end
#
# A single match enforces both the format AND the split atomically.
# BASH_REMATCH[1] is the number, [2] is the message. A malformed
# input simply fails the match -- no partial state, no separate
# "missing separator" branch.
#
# The match is purely an in-process string operation: no eval, no
# subshell, no command substitution. An attacker cannot inject shell
# commands through it.

if [[ ! "$PAYLOAD" =~ ^(\+[1-9][0-9]{6,14})[[:space:]]+(.+)$ ]]; then
    echo "expected '+<E164> <message>'" >&2
    exit 64
fi
NUMBER="${BASH_REMATCH[1]}"
MESSAGE="${BASH_REMATCH[2]}"

# --- 2. Revalidate the message ----------------------------------------
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

# --- 3. Verify required env (helpful failure mode) --------------------
: "${TWILIO_ACCOUNT_SID:?TWILIO_ACCOUNT_SID not set}"
: "${TWILIO_AUTH_TOKEN:?TWILIO_AUTH_TOKEN not set}"
: "${TWILIO_FROM:?TWILIO_FROM not set}"

# --- 4. Send -----------------------------------------------------------
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

# Twilio Message SIDs are documented to start with "SM". Match the
# full prefix instead of just any "sid" key, so error bodies that
# mention subaccount_sid / application_sid don't false-positive as
# success. We don't pull in jq because not every operator has it;
# grep is enough for a one-line reply.
if printf '%s' "$response" | grep -q '"sid":"SM'; then
    sid=$(printf '%s' "$response" | grep -o '"sid":"SM[A-Za-z0-9]*"' | head -n1 | sed 's/.*"\(SM[^"]*\)"/\1/')
    echo "sent ${sid:-?}"
    exit 0
fi

echo "twilio rejected: $(printf '%s' "$response" | head -c 80)" >&2
exit 1
