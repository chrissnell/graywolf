#!/usr/bin/env bash
# Action: iss
# Grammar:  @@<otp>#iss
# Args:     none
# Reply:    "ISS <lat>,<lon> alt <km>km vel <kph>kph"
# Source:   wheretheiss.at v1 (free, no key)
# Deps:     curl, jq
set -eu

resp=$(curl -fsSL --max-time 6 https://api.wheretheiss.at/v1/satellites/25544) \
  || { echo "fetch failed" >&2; exit 1; }

read -r lat lon alt vel < <(printf '%s' "$resp" | jq -r '
  "\(.latitude  | tostring | .[0:6]) " +
  "\(.longitude | tostring | .[0:6]) " +
  "\(.altitude  | floor) " +
  "\(.velocity  | floor)
"')

echo "ISS ${lat},${lon} alt ${alt}km vel ${vel}kph"
