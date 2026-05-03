# Action: ha-lights
# Grammar:  @@<otp>#ha-lights entity=light.<id> state=<on|off> [brightness=<0-255>]
# Args:     entity     (required) -- HA light entity_id
#           state      (required) -- on or off
#           brightness (optional) -- 0..255, only on
# Reply:    "<entity> <state>"
# Config:   HA_URL, HA_TOKEN

$ErrorActionPreference = 'Stop'

$entity = $env:GW_ARG_ENTITY
$state = $env:GW_ARG_STATE
$brightness = $env:GW_ARG_BRIGHTNESS

if (-not $entity -or -not $state) {
  [Console]::Error.WriteLine('entity and state required')
  exit 1
}
foreach ($v in 'HA_URL','HA_TOKEN') {
  if (-not (Get-Item "env:$v" -ErrorAction SilentlyContinue)) {
    [Console]::Error.WriteLine("$v not set")
    exit 1
  }
}

switch ($state) {
  'on'  { $svc = 'turn_on' }
  'off' { $svc = 'turn_off' }
  default {
    [Console]::Error.WriteLine('state must be on or off')
    exit 1
  }
}

$body = @{ entity_id = $entity }
if ($state -eq 'on' -and $brightness) {
  $body.brightness = [int]$brightness
}

try {
  Invoke-RestMethod -Method Post -TimeoutSec 6 `
    -Headers @{ Authorization = "Bearer $($env:HA_TOKEN)"; 'Content-Type' = 'application/json' } `
    -Uri "$($env:HA_URL)/api/services/light/$svc" `
    -Body ($body | ConvertTo-Json -Compress) | Out-Null
} catch {
  [Console]::Error.WriteLine('ha call failed')
  exit 1
}

"$entity $state"
