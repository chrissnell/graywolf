# Action: weather
# Grammar:  @@<otp>#weather location=<place>
# Args:     location  (required) -- city name, ZIP, ICAO airport, or
#                                   "lat,lon" (Denver, 80202, KDEN,
#                                   "39.7,-105.0")
# Reply:    two-line current conditions in plain English. Set the
#           Action's MaxReplyLines >= 2 or only the first line ships.
#           Line 1: "<location>: <condition> <temp>"
#           Line 2: "wind <wind> hum <hum> <pressure>"
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
# %C condition, %t temp, %w wind, %h humidity, %P pressure; &u forces
# USCS units. The literal "|" splits the response into the two on-air
# lines; wttr.in passes it through verbatim and "|" never appears in
# any of these fields.
$url = "https://wttr.in/${encoded}?format=%C+%t|wind+%w+hum+%h+%P&u"

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

$parts = $resp.Split('|', 2)
"${location}: $($parts[0])"
# Drop line 2 if the delimiter was missing.
if ($parts.Length -eq 2) {
  $parts[1]
}
