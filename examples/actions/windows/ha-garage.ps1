# Action: ha-garage
# Grammar:  @@<otp>#ha-garage action=<open|close|toggle>
# Args:     action  (required) -- open, close, or toggle
# Reply:    "garage <action>"
# Config:   HA_URL, HA_TOKEN, HA_GARAGE_ENTITY (e.g. cover.garage_door)
#
# DO NOT install without OTPRequired=true and a sender allowlist.

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$action = $env:GW_ARG_ACTION
if (-not $action) {
  [Console]::Error.WriteLine('action required')
  exit 1
}
foreach ($v in 'HA_URL','HA_TOKEN','HA_GARAGE_ENTITY') {
  if (-not (Get-Item "env:$v" -ErrorAction SilentlyContinue)) {
    [Console]::Error.WriteLine("$v not set")
    exit 1
  }
}

switch ($action) {
  'open'   { $svc = 'open_cover'  }
  'close'  { $svc = 'close_cover' }
  'toggle' { $svc = 'toggle'      }
  default {
    [Console]::Error.WriteLine('action must be open|close|toggle')
    exit 1
  }
}

try {
  Invoke-RestMethod -Method Post -TimeoutSec 6 `
    -Headers @{ Authorization = "Bearer $($env:HA_TOKEN)"; 'Content-Type' = 'application/json' } `
    -Uri "$($env:HA_URL)/api/services/cover/$svc" `
    -Body (@{ entity_id = $env:HA_GARAGE_ENTITY } | ConvertTo-Json -Compress) | Out-Null
} catch {
  [Console]::Error.WriteLine('ha call failed')
  exit 1
}

"garage $action"
