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
# Reply: two-line current conditions in plain English. Set the Action's
#        MaxReplyLines >= 2 or only the first line ships.
#        Line 1: "<label>: <condition> <temp>"  (label = "<input>
#                (<city>)" when wttr.in resolves a different city name
#                and it fits, else just <input> or <city>)
#        Line 2: "wind <dir><speed>mph hum <hum>% <pressure>hPa"
#        Unknown location -> single-line helpful message.
# Source: wttr.in (free, no key, worldwide).
# Deps:   curl, python3

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

# j1 returns full JSON: current_condition + nearest_area (resolved city).
# &u forces USCS units. wttr.in returns HTTP 500 with body "location not
# found: location not found" for unknown ICAO/ZIP/etc, so capture status.
url="https://wttr.in/${encoded}?format=j1&u"
raw=$(curl -sSL --max-time 8 -w $'\n__HTTP__%{http_code}' "$url") \
    || { echo "fetch failed" >&2; exit 1; }
status="${raw##*__HTTP__}"
body="${raw%$'\n'__HTTP__*}"

if [[ "$status" != "200" ]]; then
    if printf '%s' "$body" | grep -qi 'location not found'; then
        echo "unknown location '$location'. Try city, ZIP, ICAO, or lat,lon"
        exit 0
    fi
    echo "fetch failed" >&2
    exit 1
fi

parsed=$(printf '%s' "$body" | python3 -c '
import json, sys
d = json.load(sys.stdin)
def first(xs):
    return (xs or [{}])[0].get("value", "").strip() if isinstance(xs, list) else ""
c = (d.get("current_condition") or [{}])[0]
a = (d.get("nearest_area") or [{}])[0]
print(first(a.get("areaName")))
print(first(c.get("weatherDesc")))
print(c.get("temp_F", ""))
print(c.get("winddir16Point", ""))
print(c.get("windspeedMiles", ""))
print(c.get("humidity", ""))
print(c.get("pressure", ""))
') || { echo "parse failed" >&2; exit 1; }

mapfile -t F <<<"$parsed"
city="${F[0]:-}"; desc="${F[1]:-}"; tF="${F[2]:-}"
wd="${F[3]:-}"; ws="${F[4]:-}"; hum="${F[5]:-}"; pr="${F[6]:-}"

if [[ -z "$desc" || -z "$tF" ]]; then
    echo "$location: no data"
    exit 0
fi

# Prefer "<input> (<city>)" when wttr.in resolved a different city name
# and the combined label fits in 28 chars (leaves room for conditions on
# a 67-char APRS line). Otherwise fall back to just <input> or <city>.
loc_lc=$(printf '%s' "$location" | tr '[:upper:]' '[:lower:]')
city_lc=$(printf '%s' "$city" | tr '[:upper:]' '[:lower:]')
if [[ -z "$city" || "$loc_lc" == "$city_lc" ]]; then
    label="$location"
else
    combined="${location} (${city})"
    if (( ${#combined} <= 28 )); then
        label="$combined"
    else
        label="$city"
    fi
fi

echo "${label}: ${desc} ${tF}°F"
echo "wind ${wd}${ws}mph hum ${hum}% ${pr}hPa"
