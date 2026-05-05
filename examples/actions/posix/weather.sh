#!/usr/bin/env bash
# Action: weather
# Grammar:  @@<otp>#weather location=<place>
# Args:     location  (required) -- city name, ZIP, ICAO airport, or
#                                   "lat,lon"  (Denver, 80202, KDEN,
#                                   "39.7,-105.0")
# Reply:    two-line current conditions in plain English. Set the
#           Action's MaxReplyLines >= 2 or only the first line ships.
#           Line 1: "<location>: <condition> <temp>"
#           Line 2: "wind <wind> hum <hum> <pressure>"
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

# %C condition, %t temp, %w wind, %h humidity, %P pressure; &u forces
# USCS units. The literal "|" splits the response into the two on-air
# lines; wttr.in passes it through verbatim and "|" never appears in
# any of these fields.
url="https://wttr.in/${encoded}?format=%C+%t|wind+%w+hum+%h+%P&u"
resp=$(curl -fsSL --max-time 8 -- "$url") || { echo "fetch failed" >&2; exit 1; }

resp=$(printf '%s' "$resp" | tr -d '\r\n')
if [[ -z "$resp" ]]; then
  echo "$location: no data"
  exit 0
fi

line1="${resp%%|*}"
line2="${resp#*|}"
echo "${location}: ${line1}"
# Drop line 2 if the delimiter was missing (no "|" in resp leaves
# line2 == resp, which would duplicate line 1).
if [[ "$line2" != "$resp" ]]; then
  echo "$line2"
fi
