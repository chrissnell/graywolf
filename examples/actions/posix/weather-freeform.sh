#!/usr/bin/env bash
# weather-freeform.sh -- Fetch current conditions for a place.
# Freeform Action variant.
#
# Wire it as an Action with arg_mode=freeform. Senders write:
#
#     @@<otp>#weather Denver
#     @@<otp>#weather 80202
#     @@<otp>#weather KDEN
#
# The runner invokes this script as:
#
#     weather-freeform.sh weather KE0XYZ true "Denver"
#                         ^       ^      ^    ^
#                         |       |      |    GW_ARG (positional $4): the
#                         |       |      |    entire freeform payload --
#                         |       |      |    here, the location string
#                         |       |      OTP_VERIFIED: "true" or "false"
#                         |       GW_SENDER_CALL: APRS callsign
#                         GW_ACTION_NAME: always "weather" here
#
# Reply: one-line current conditions in plain English.
# Source: wttr.in (free, no key, worldwide).
# Deps:   curl

set -euo pipefail

# shellcheck disable=SC2034
ACTION="$1"
# shellcheck disable=SC2034
SENDER="$2"
# shellcheck disable=SC2034
OTP_VERIFIED="$3"
PAYLOAD="$4"

# Trim leading/trailing whitespace, collapse internal runs to one space.
location=$(printf '%s' "$PAYLOAD" | awk '{$1=$1; print}')

if [[ -z "$location" ]]; then
    echo "location required" >&2
    exit 64
fi

# Whitelist input so URL/shell metacharacters can't be smuggled into the
# request. Allow letters, digits, space, comma, dot, underscore, hyphen.
if [[ ! "$location" =~ ^[A-Za-z0-9.,_\ -]+$ ]]; then
    echo "invalid location" >&2
    exit 64
fi
encoded=$(printf '%s' "$location" | tr ' ' '+')

# %C condition, %t temperature, %w wind, %h humidity; &u forces USCS units.
url="https://wttr.in/${encoded}?format=%C+%t+%w+%h&u"
resp=$(curl -fsSL --max-time 8 -- "$url") || { echo "fetch failed" >&2; exit 1; }

resp=$(printf '%s' "$resp" | tr -d '\r\n')
if [[ -z "$resp" ]]; then
    echo "$location: no data"
    exit 0
fi
echo "${location}: ${resp}"
