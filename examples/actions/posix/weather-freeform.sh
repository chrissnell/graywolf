#!/usr/bin/env bash
# weather-freeform.sh -- Fetch latest METAR for an ICAO station.
# Freeform Action variant.
#
# Wire it as an Action with arg_mode=freeform. Senders write:
#
#     @@<otp>#weather KDEN
#
# The runner invokes this script as:
#
#     weather-freeform.sh weather KE0XYZ true "KDEN"
#                         ^       ^      ^    ^
#                         |       |      |    GW_ARG (positional $4): the
#                         |       |      |    entire freeform payload --
#                         |       |      |    here, just the ICAO code
#                         |       |      OTP_VERIFIED: "true" or "false"
#                         |       GW_SENDER_CALL: APRS callsign
#                         GW_ACTION_NAME: always "weather" here
#
# Reply: raw METAR observation, snipped to 50 chars on-air.
# Source: aviationweather.gov (free, no key, worldwide METAR coverage).
# Deps:   curl, jq

set -euo pipefail

# shellcheck disable=SC2034
ACTION="$1"
# shellcheck disable=SC2034
SENDER="$2"
# shellcheck disable=SC2034
OTP_VERIFIED="$3"
PAYLOAD="$4"

# Trim leading/trailing whitespace, collapse internal runs to one space.
# Freeform payload is sanitized by the runtime, but be defensive: a
# stray trailing newline or doubled space would otherwise break the
# regex below.
station=$(printf '%s' "$PAYLOAD" | awk '{$1=$1; print}')

# ICAO codes are 4 letters; some military/regional sites use 3-4
# alphanumerics. Be strict: letters only, 3 or 4 chars, nothing else.
# A single anchored match is the validation AND the split.
if [[ ! "$station" =~ ^[A-Za-z]{3,4}$ ]]; then
    echo "expected ICAO code (3-4 letters)" >&2
    exit 64
fi

station=$(printf '%s' "$station" | tr '[:lower:]' '[:upper:]')

url="https://aviationweather.gov/api/data/metar?ids=${station}&format=json&taf=false&hours=2"
resp=$(curl -fsSL --max-time 8 -- "$url") || { echo "fetch failed" >&2; exit 1; }

raw=$(printf '%s' "$resp" | jq -r 'if length==0 then "" else .[0].rawOb // "" end')
if [[ -z "$raw" ]]; then
    echo "$station: no recent METAR"
    exit 0
fi

# Strip leading "METAR " / "SPECI " so the on-air 50-char snippet
# starts with the ICAO + observation time.
if [[ "$raw" =~ ^(METAR|SPECI)\ (.+)$ ]]; then
    echo "${BASH_REMATCH[2]}"
else
    echo "$raw"
fi
