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
#                (<City, ST>)" when wttr.in resolves a different name
#                and it fits, else just <input> or <City, ST>; ST is
#                the 2-letter US state abbrev when known)
#        Line 2: "wind <dir><speed>mph hum <hum>% <pressure>hPa"
#        Unknown location -> single-line helpful message.
# Source: wttr.in (free, no key, worldwide).
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
location=$(printf '%s' "$PAYLOAD" | awk '{$1=$1; print}')

if [[ -z "$location" ]]; then
    echo "location required" >&2
    exit 64
fi

# Whitelist input so URL/shell metacharacters can't be smuggled into the
# request. Allow letters, digits, comma, hyphen -- enough for ZIP, ICAO,
# single-word city, and integer "lat,lon".
if [[ ! "$location" =~ ^[A-Za-z0-9,-]+$ ]]; then
    echo "invalid location" >&2
    exit 64
fi
encoded="$location"

# j1 returns full JSON: current_condition + nearest_area (resolved city
# + region/state). &u forces USCS units. wttr.in returns HTTP 500 with
# body "location not found: location not found" for unknown ICAO/ZIP,
# so capture status.
url="https://wttr.in/${encoded}?format=j1&u"
raw=$(curl -sSL --max-time 8 -w $'\n__HTTP__%{http_code}' -- "$url") \
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

# Pull the eight fields we need as a single tab-separated line. Region
# is the wttr.in label for state/province (full name, e.g. "Colorado").
parsed=$(printf '%s' "$body" | jq -r '
    [
      (.nearest_area[0].areaName[0].value     // ""),
      (.nearest_area[0].region[0].value       // ""),
      (.current_condition[0].weatherDesc[0].value // ""),
      (.current_condition[0].temp_F           // ""),
      (.current_condition[0].winddir16Point   // ""),
      (.current_condition[0].windspeedMiles   // ""),
      (.current_condition[0].humidity         // ""),
      (.current_condition[0].pressure         // "")
    ] | @tsv
') || { echo "parse failed" >&2; exit 1; }

IFS=$'\t' read -r city region desc tF wd ws hum pr <<<"$parsed"

if [[ -z "$desc" || -z "$tF" ]]; then
    echo "$location: no data"
    exit 0
fi

# Map a wttr.in region (full state name) to a 2-letter US postal code.
# Returns empty string when the region is not a US state -- callers fall
# back to just the city in that case to avoid noisy non-US labels.
state_abbrev() {
    case "$1" in
        Alabama) echo AL;; Alaska) echo AK;; Arizona) echo AZ;;
        Arkansas) echo AR;; California) echo CA;; Colorado) echo CO;;
        Connecticut) echo CT;; Delaware) echo DE;;
        "District of Columbia") echo DC;; Florida) echo FL;;
        Georgia) echo GA;; Hawaii) echo HI;; Idaho) echo ID;;
        Illinois) echo IL;; Indiana) echo IN;; Iowa) echo IA;;
        Kansas) echo KS;; Kentucky) echo KY;; Louisiana) echo LA;;
        Maine) echo ME;; Maryland) echo MD;; Massachusetts) echo MA;;
        Michigan) echo MI;; Minnesota) echo MN;; Mississippi) echo MS;;
        Missouri) echo MO;; Montana) echo MT;; Nebraska) echo NE;;
        Nevada) echo NV;; "New Hampshire") echo NH;; "New Jersey") echo NJ;;
        "New Mexico") echo NM;; "New York") echo NY;;
        "North Carolina") echo NC;; "North Dakota") echo ND;;
        Ohio) echo OH;; Oklahoma) echo OK;; Oregon) echo OR;;
        Pennsylvania) echo PA;; "Rhode Island") echo RI;;
        "South Carolina") echo SC;; "South Dakota") echo SD;;
        Tennessee) echo TN;; Texas) echo TX;; Utah) echo UT;;
        Vermont) echo VT;; Virginia) echo VA;; Washington) echo WA;;
        "West Virginia") echo WV;; Wisconsin) echo WI;; Wyoming) echo WY;;
        "Puerto Rico") echo PR;; Guam) echo GU;;
        "U.S. Virgin Islands"|"United States Virgin Islands") echo VI;;
        "American Samoa") echo AS;;
        "Northern Mariana Islands") echo MP;;
        *) echo "";;
    esac
}

st=$(state_abbrev "$region")
if [[ -n "$st" ]]; then
    city_disp="${city}, ${st}"
else
    city_disp="$city"
fi

# Prefer "<input> (<City, ST>)" when wttr.in resolved a different city
# and the combined label fits in 28 chars (leaves room for conditions on
# a 67-char APRS line). Otherwise fall back to just <input> or city_disp.
loc_lc=$(printf '%s' "$location" | tr '[:upper:]' '[:lower:]')
city_lc=$(printf '%s' "$city" | tr '[:upper:]' '[:lower:]')
if [[ -z "$city" || "$loc_lc" == "$city_lc" ]]; then
    label="$city_disp"
    [[ -z "$label" ]] && label="$location"
else
    combined="${location} (${city_disp})"
    if (( ${#combined} <= 28 )); then
        label="$combined"
    else
        label="$city_disp"
    fi
fi

echo "${label}: ${desc} ${tF}°F"
echo "wind ${wd}${ws}mph hum ${hum}% ${pr}hPa"
