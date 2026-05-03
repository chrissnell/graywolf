#!/usr/bin/env bash
# Action: weather
# Grammar:  @@<otp>#weather station=<ICAO>
# Args:     station  (required) -- 4-letter ICAO airport code
#                                   (KDEN, EGLL, RJTT, YSSY ...)
# Reply:    raw METAR observation, snipped to 50 chars on-air
# Source:   aviationweather.gov (free, no key, worldwide METAR coverage)
# Deps:     curl, jq
set -eu

station="${GW_ARG_STATION:-}"
if [[ -z "$station" ]]; then
  echo "station required" >&2
  exit 1
fi
station=$(printf '%s' "$station" | tr '[:lower:]' '[:upper:]')

url="https://aviationweather.gov/api/data/metar?ids=${station}&format=json&taf=false&hours=2"
resp=$(curl -fsSL --max-time 8 "$url") || { echo "fetch failed" >&2; exit 1; }

raw=$(printf '%s' "$resp" | jq -r 'if length==0 then "" else .[0].rawOb // "" end')
if [[ -z "$raw" ]]; then
  echo "$station: no recent METAR"
  exit 0
fi

# Strip leading "METAR " / "SPECI " so the on-air 50-char snippet
# starts with the ICAO + observation time.
echo "${raw#METAR }" | sed 's/^SPECI //'
