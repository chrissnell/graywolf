# Action: weather
# Grammar:  @@<otp>#weather location=<place>
# Args:     location  (required) -- city name, ZIP, ICAO airport, or
#                                   "lat,lon" (Denver, 80202, KDEN,
#                                   "39.7,-105.0")
# Reply:    two-line current conditions in plain English. Set the
#           Action's MaxReplyLines >= 2 or only the first line ships.
#           Line 1: "<label>: <condition> <temp>"  (label = "<input>
#                   (<city>)" when wttr.in resolves a different city
#                   name and it fits, else just <input> or <city>)
#           Line 2: "wind <dir><speed>mph hum <hum>% <pressure>hPa"
#           Unknown location -> single-line helpful message.
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
# j1 returns full JSON: current_condition + nearest_area (resolved city).
# &u forces USCS units. wttr.in returns HTTP 500 with body "location not
# found: location not found" for unknown ICAO/ZIP/etc.
$url = "https://wttr.in/${encoded}?format=j1&u"

$content = $null
try {
  $content = (Invoke-WebRequest -UseBasicParsing -TimeoutSec 8 -Uri $url).Content
} catch {
  $errResp = $null
  try { $errResp = $_.Exception.Response } catch {}
  if ($errResp -and ($errResp.StatusCode.value__ -eq 500)) {
    $errBody = ''
    try {
      $reader = [System.IO.StreamReader]::new($errResp.GetResponseStream())
      $errBody = $reader.ReadToEnd()
      $reader.Dispose()
    } catch {}
    if ($errBody -match 'location not found') {
      "unknown location '$location'. Try city, ZIP, ICAO, or lat,lon"
      exit 0
    }
  }
  [Console]::Error.WriteLine('fetch failed')
  exit 1
}

if (-not $content) {
  "${location}: no data"
  exit 0
}

try {
  $j = $content | ConvertFrom-Json
} catch {
  [Console]::Error.WriteLine('parse failed')
  exit 1
}

$c = $j.current_condition[0]
$a = $j.nearest_area[0]
$city = if ($a -and $a.areaName) { $a.areaName[0].value.Trim() } else { '' }
$desc = if ($c.weatherDesc) { $c.weatherDesc[0].value.Trim() } else { '' }
$tF   = $c.temp_F
$wd   = $c.winddir16Point
$ws   = $c.windspeedMiles
$hum  = $c.humidity
$pr   = $c.pressure

if (-not $desc -or -not $tF) {
  "${location}: no data"
  exit 0
}

# Prefer "<input> (<city>)" when wttr.in resolved a different city name
# and the combined label fits in 28 chars (leaves room for conditions on
# a 67-char APRS line). Otherwise fall back to just <input> or <city>.
$label = $location
if ($city -and ($location.ToLower() -ne $city.ToLower())) {
  $combined = "$location ($city)"
  if ($combined.Length -le 28) { $label = $combined } else { $label = $city }
}

"${label}: $desc ${tF}°F"
"wind ${wd}${ws}mph hum ${hum}% ${pr}hPa"
