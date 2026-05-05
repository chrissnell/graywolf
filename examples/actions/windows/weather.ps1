# Action: weather
# Grammar:  @@<otp>#weather location=<place>
# Args:     location  (required) -- city name, ZIP, ICAO airport, or
#                                   "lat,lon" (Denver, 80202, KDEN,
#                                   "39.7,-105.0")
# Reply:    one-line current conditions in plain English
# Source:   wttr.in (free, no key, worldwide)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$location = $env:GW_ARG_LOCATION
if (-not $location) {
  [Console]::Error.WriteLine('location required')
  exit 1
}

# Whitelist input so URL/shell metacharacters can't be smuggled into the
# request. Allow letters, digits, space, comma, dot, underscore, hyphen.
if ($location -notmatch '^[A-Za-z0-9.,_ -]+$') {
  [Console]::Error.WriteLine('invalid location')
  exit 64
}

$encoded = [System.Uri]::EscapeDataString($location)
# %C condition, %t temperature, %w wind, %h humidity; &u forces USCS units.
$url = "https://wttr.in/${encoded}?format=%C+%t+%w+%h&u"

try {
  $resp = (Invoke-WebRequest -UseBasicParsing -TimeoutSec 8 -Uri $url).Content.Trim()
} catch {
  [Console]::Error.WriteLine('fetch failed')
  exit 1
}

if (-not $resp) {
  "${location}: no data"
  exit 0
}

"${location}: $resp"
