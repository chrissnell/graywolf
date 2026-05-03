# Action: weather
# Grammar:  @@<otp>#weather station=<ICAO>
# Args:     station  (required) -- 4-letter ICAO airport code
# Reply:    raw METAR observation, snipped to 50 chars on-air
# Source:   aviationweather.gov (free, no key, worldwide)

$ErrorActionPreference = 'Stop'

$station = $env:GW_ARG_STATION
if (-not $station) {
  [Console]::Error.WriteLine('station required')
  exit 1
}
$station = $station.ToUpper()

try {
  $resp = Invoke-RestMethod -TimeoutSec 8 `
    "https://aviationweather.gov/api/data/metar?ids=$station&format=json&taf=false&hours=2"
} catch {
  [Console]::Error.WriteLine('fetch failed')
  exit 1
}

if (-not $resp -or $resp.Count -eq 0 -or -not $resp[0].rawOb) {
  "$station no recent METAR"
  exit 0
}

# Strip leading "METAR " / "SPECI " so the on-air 50-char snippet
# starts with the ICAO + observation time.
$raw = $resp[0].rawOb -replace '^(METAR|SPECI)\s+', ''
$raw
