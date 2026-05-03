# Action: sms
# Grammar:  @@<otp>#sms to=<E164> msg=<text>
# Args:     to   (required) -- E.164 phone number (+15551234567)
#           msg  (required) -- message body
# Reply:    "sms sent to <to>"
# Behavior: SMS via Twilio; if APRSFI_API_KEY is set, also sends a second
#           SMS with a Google Maps link to the sender's last APRS position.
# Config:   TWILIO_ACCOUNT_SID, TWILIO_AUTH_TOKEN, TWILIO_FROM,
#           APRSFI_API_KEY (optional)

$ErrorActionPreference = 'Stop'

$to = $env:GW_ARG_TO
$msg = $env:GW_ARG_MSG
$sender = if ($env:GW_SENDER_CALL) { $env:GW_SENDER_CALL } else { '?' }

if (-not $to -or -not $msg) {
  [Console]::Error.WriteLine('to and msg required')
  exit 1
}
foreach ($v in 'TWILIO_ACCOUNT_SID','TWILIO_AUTH_TOKEN','TWILIO_FROM') {
  if (-not (Get-Item "env:$v" -ErrorAction SilentlyContinue)) {
    [Console]::Error.WriteLine("$v not set")
    exit 1
  }
}

$sid = $env:TWILIO_ACCOUNT_SID
$tok = $env:TWILIO_AUTH_TOKEN
$auth = [Convert]::ToBase64String([Text.Encoding]::ASCII.GetBytes("${sid}:${tok}"))
$headers = @{ Authorization = "Basic $auth" }
$uri = "https://api.twilio.com/2010-04-01/Accounts/$sid/Messages.json"

function Send-Sms($body) {
  $form = @{ From = $env:TWILIO_FROM; To = $to; Body = $body }
  Invoke-RestMethod -Method Post -Uri $uri -Headers $headers -Body $form -TimeoutSec 8 | Out-Null
}

try {
  Send-Sms "From $sender (APRS): $msg"
} catch {
  [Console]::Error.WriteLine('twilio failed')
  exit 1
}

# Best-effort location follow-up.
if ($env:APRSFI_API_KEY) {
  try {
    $loc = Invoke-RestMethod -TimeoutSec 6 `
      "https://api.aprs.fi/api/get?name=$sender&what=loc&apikey=$($env:APRSFI_API_KEY)&format=json"
    $e = $loc.entries | Select-Object -First 1
    if ($e -and $e.lat -and $e.lng) {
      Send-Sms "Location of $sender: https://maps.google.com/?q=$($e.lat),$($e.lng)"
    }
  } catch { }
}

"sms sent to $to"
