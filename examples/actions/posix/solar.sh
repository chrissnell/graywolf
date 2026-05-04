#!/usr/bin/env bash
# Action: solar
# Grammar:  @@<otp>#solar
# Args:     none
# Reply:    "SFI <n> A <n> K <n> SN <n>"
# Source:   hamqsl.com solar XML feed (free)
# Deps:     curl
set -euo pipefail

xml=$(curl -fsSL --max-time 8 https://www.hamqsl.com/solarxml.php) \
  || { echo "fetch failed" >&2; exit 1; }

# Extract a single XML element value. Trims whitespace.
extract() {
  printf '%s' "$xml" \
    | sed -n "s|.*<$1>[[:space:]]*\([^<]*\)[[:space:]]*</$1>.*|\1|p" \
    | head -1
}

sfi=$(extract solarflux)
a=$(extract aindex)
k=$(extract kindex)
sn=$(extract sunspots)

echo "SFI ${sfi:-?} A ${a:-?} K ${k:-?} SN ${sn:-?}"
