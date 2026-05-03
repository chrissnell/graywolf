# Action: iss
# Grammar:  @@<otp>#iss
# Args:     none
# Reply:    "ISS <lat>,<lon> alt <km>km vel <kph>kph"
# Source:   wheretheiss.at v1 (free, no key)

$ErrorActionPreference = 'Stop'

try {
  $r = Invoke-RestMethod -TimeoutSec 6 https://api.wheretheiss.at/v1/satellites/25544
} catch {
  [Console]::Error.WriteLine('fetch failed')
  exit 1
}

$lat = ([string]$r.latitude).Substring(0, [Math]::Min(6, ([string]$r.latitude).Length))
$lon = ([string]$r.longitude).Substring(0, [Math]::Min(6, ([string]$r.longitude).Length))
$alt = [int][Math]::Floor([double]$r.altitude)
$vel = [int][Math]::Floor([double]$r.velocity)

"ISS $lat,$lon alt ${alt}km vel ${vel}kph"
