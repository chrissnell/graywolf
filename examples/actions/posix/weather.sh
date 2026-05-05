#!/usr/bin/env bash
# Action: weather
# Grammar:  @@<otp>#weather location=<place>
# Args:     location  (required) -- city name, ZIP, ICAO airport, or
#                                   "lat,lon"  (Denver, 80202, KDEN,
#                                   "39.7,-105.0")
# Reply:    one-line current conditions in plain English
# Source:   wttr.in (free, no key, worldwide)
# Deps:     curl
set -euo pipefail

location="${GW_ARG_LOCATION:-}"
if [[ -z "$location" ]]; then
  echo "location required" >&2
  exit 1
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
