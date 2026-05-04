# sms-freeform.ps1 -- Send an SMS via Twilio. Freeform Action variant.
#
# Wire it as an Action with arg_mode=freeform. Senders write:
#
#     @@<otp>#sms +15555551212 hello there
#
# graywolf invokes this script as:
#
#     powershell.exe -NoProfile -File sms-freeform.ps1 sms KE0XYZ true "+15555551212 hello there"
#                                                       ^   ^      ^   ^
#                                                       |   |      |   $payload (positional $args[3]):
#                                                       |   |      |   the entire freeform payload
#                                                       |   |      OTP_VERIFIED: "true" or "false"
#                                                       |   GW_SENDER_CALL: APRS callsign
#                                                       GW_ACTION_NAME: always "sms" here
#
# Required environment (set in the system context, NOT in this script):
#   TWILIO_ACCOUNT_SID
#   TWILIO_AUTH_TOKEN
#   TWILIO_FROM            (your Twilio number, e.g. +14155551111)
#
# Defense layers (top-to-bottom, fail-fast):
#   1. Set-StrictMode + $ErrorActionPreference='Stop' -- typos and
#      cmdlet errors are hard failures, not silent $null.
#   2. Single regex match validates AND splits the payload.
#   3. Revalidate the message body (control chars, length).
#   4. No Invoke-Expression, no cmd.exe /c -- argv-style cmdlet calls.
#   5. Hashtable -Body so Invoke-RestMethod URL-encodes for us.

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

# Positional args from the runner. Only $payload is used here.
$action      = $args[0]
$sender      = $args[1]
$otpVerified = $args[2]
$payload     = $args[3]

if (-not $payload) {
    [Console]::Error.WriteLine("payload missing")
    exit 64
}

# --- 1. Validate + split in one regex ---------------------------------
#
# .NET regex with named capture groups. Pattern:
#   ^                               anchor at start
#   (?<num>\+[1-9][0-9]{6,14})      named group 'num': E.164 number
#                                     \+        literal '+'
#                                     [1-9]     leading digit (E.164 forbids 0)
#                                     [0-9]{6,14} 6-14 more digits
#   \s+                             one or more whitespace separators
#   (?<msg>.+)                      named group 'msg': message body
#   $                               anchor at end
#
# A single match enforces both the format AND the split atomically.
# If the match fails, $match.Success is $false -- no partial state.
#
# This is purely an in-process string operation: no Invoke-Expression,
# no subshell, no command substitution. An attacker cannot inject
# PowerShell through it.

$rx = '^(?<num>\+[1-9][0-9]{6,14})\s+(?<msg>.+)$'
$match = [regex]::Match($payload, $rx)
if (-not $match.Success) {
    [Console]::Error.WriteLine("expected '+<E164> <message>'")
    exit 64
}
$number  = $match.Groups['num'].Value
$message = $match.Groups['msg'].Value

# --- 2. Revalidate the message ----------------------------------------
#
# \p{Cc} is the Unicode "Other, Control" category (the .NET regex
# counterpart of POSIX [:cntrl:]). graywolf's sanitizer already strips
# these, but checking again here means the script remains safe even
# if the operator widens the Action regex.

if ($message -match '\p{Cc}') {
    [Console]::Error.WriteLine("message contains control characters")
    exit 65
}
if ($message.Length -lt 1 -or $message.Length -gt 160) {
    [Console]::Error.WriteLine("message length out of range (1..160)")
    exit 65
}

# --- 3. Verify required env (helpful failure mode) --------------------
foreach ($v in 'TWILIO_ACCOUNT_SID','TWILIO_AUTH_TOKEN','TWILIO_FROM') {
    if (-not (Get-Item "env:$v" -ErrorAction SilentlyContinue)) {
        [Console]::Error.WriteLine("$v not set")
        exit 1
    }
}

$sid  = $env:TWILIO_ACCOUNT_SID
$tok  = $env:TWILIO_AUTH_TOKEN
$auth = [Convert]::ToBase64String([Text.Encoding]::ASCII.GetBytes("${sid}:${tok}"))
$headers = @{ Authorization = "Basic $auth" }
$uri = "https://api.twilio.com/2010-04-01/Accounts/$sid/Messages.json"

# --- 4. Send -----------------------------------------------------------
#
# Hashtable -Body causes Invoke-RestMethod to serialize as
# application/x-www-form-urlencoded and URL-encode each value. We
# never concatenate user data into a request body string, so a '&'
# or '=' in the message cannot smuggle extra form fields.

$form = @{
    From = $env:TWILIO_FROM
    To   = $number
    Body = $message
}

try {
    $resp = Invoke-RestMethod -Method Post -Uri $uri -Headers $headers `
        -Body $form -TimeoutSec 8
    if ($resp.sid) {
        "sent $($resp.sid)"
        exit 0
    }
} catch {
    [Console]::Error.WriteLine("twilio rejected: $($_.Exception.Message)")
    exit 1
}

[Console]::Error.WriteLine("twilio rejected: no sid in response")
exit 1
