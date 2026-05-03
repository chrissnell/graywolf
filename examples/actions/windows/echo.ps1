# Action: echo
# Grammar:  @@<otp>#echo msg=<text>
# Args:     msg  (required) -- text to echo
# Reply:    "<sender> said: <msg>"

$ErrorActionPreference = 'Stop'

$msg = $env:GW_ARG_MSG
$sender = if ($env:GW_SENDER_CALL) { $env:GW_SENDER_CALL } else { '?' }

if (-not $msg) {
  [Console]::Error.WriteLine('msg empty')
  exit 1
}

"$sender said: $msg"
