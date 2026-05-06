# Action: weather
# Grammar:  @@<otp>#weather location=<place>
# Args:     location  (required) -- city name, ZIP, ICAO airport, or
#                                   "lat,lon" (Denver, 80202, KDEN,
#                                   "39,-105")
# Reply:    two-line current conditions in plain English. Set the
#           Action's MaxReplyLines >= 2 or only the first line ships.
#           Line 1: "<label>: <condition> <temp>"  (label = "<input>
#                   (<City, ST>)" when wttr.in resolves a different
#                   name and it fits, else just <input> or <City, ST>;
#                   ST is the 2-letter US state abbrev when known)
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
# request. Allow letters, digits, comma, hyphen -- enough for ZIP, ICAO,
# single-word city, and integer "lat,lon".
if ($location -notmatch '^[A-Za-z0-9,-]+$') {
  [Console]::Error.WriteLine('invalid location')
  exit 64
}

$encoded = [System.Uri]::EscapeDataString($location)
# j1 returns full JSON: current_condition + nearest_area (resolved city
# + region/state). &u forces USCS units. wttr.in returns HTTP 500 with
# body "location not found: location not found" for unknown ICAO/ZIP/etc.
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
$city   = if ($a -and $a.areaName) { $a.areaName[0].value.Trim() } else { '' }
$region = if ($a -and $a.region)   { $a.region[0].value.Trim()   } else { '' }
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

# Map a wttr.in region (full state name) to a 2-letter US postal code.
# Empty when not a US state -- caller falls back to just the city.
$stateMap = @{
  'Alabama'='AL'; 'Alaska'='AK'; 'Arizona'='AZ'; 'Arkansas'='AR';
  'California'='CA'; 'Colorado'='CO'; 'Connecticut'='CT';
  'Delaware'='DE'; 'District of Columbia'='DC'; 'Florida'='FL';
  'Georgia'='GA'; 'Hawaii'='HI'; 'Idaho'='ID'; 'Illinois'='IL';
  'Indiana'='IN'; 'Iowa'='IA'; 'Kansas'='KS'; 'Kentucky'='KY';
  'Louisiana'='LA'; 'Maine'='ME'; 'Maryland'='MD';
  'Massachusetts'='MA'; 'Michigan'='MI'; 'Minnesota'='MN';
  'Mississippi'='MS'; 'Missouri'='MO'; 'Montana'='MT';
  'Nebraska'='NE'; 'Nevada'='NV'; 'New Hampshire'='NH';
  'New Jersey'='NJ'; 'New Mexico'='NM'; 'New York'='NY';
  'North Carolina'='NC'; 'North Dakota'='ND'; 'Ohio'='OH';
  'Oklahoma'='OK'; 'Oregon'='OR'; 'Pennsylvania'='PA';
  'Rhode Island'='RI'; 'South Carolina'='SC'; 'South Dakota'='SD';
  'Tennessee'='TN'; 'Texas'='TX'; 'Utah'='UT'; 'Vermont'='VT';
  'Virginia'='VA'; 'Washington'='WA'; 'West Virginia'='WV';
  'Wisconsin'='WI'; 'Wyoming'='WY'; 'Puerto Rico'='PR'; 'Guam'='GU';
  'U.S. Virgin Islands'='VI'; 'United States Virgin Islands'='VI';
  'American Samoa'='AS'; 'Northern Mariana Islands'='MP'
}
$st = ''
if ($region -and $stateMap.ContainsKey($region)) { $st = $stateMap[$region] }
$cityDisp = if ($st) { "$city, $st" } else { $city }

# Prefer "<input> (<City, ST>)" when wttr.in resolved a different city
# and the combined label fits in 28 chars (leaves room for conditions on
# a 67-char APRS line). Otherwise fall back to just <input> or cityDisp.
$label = $location
if ($city -and ($location.ToLower() -ne $city.ToLower())) {
  $combined = "$location ($cityDisp)"
  if ($combined.Length -le 28) { $label = $combined } else { $label = $cityDisp }
} elseif ($cityDisp) {
  $label = $cityDisp
}

"${label}: $desc ${tF}°F"
"wind ${wd}${ws}mph hum ${hum}% ${pr}hPa"
