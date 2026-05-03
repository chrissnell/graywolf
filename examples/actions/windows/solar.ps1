# Action: solar
# Grammar:  @@<otp>#solar
# Args:     none
# Reply:    "SFI <n> A <n> K <n> SN <n>"
# Source:   hamqsl.com solar XML feed (free)

$ErrorActionPreference = 'Stop'

try {
  [xml]$x = Invoke-WebRequest -TimeoutSec 8 -UseBasicParsing https://www.hamqsl.com/solarxml.php `
    | Select-Object -ExpandProperty Content
} catch {
  [Console]::Error.WriteLine('fetch failed')
  exit 1
}

$d = $x.solar.solardata

function Trim1($v) {
  if ($null -eq $v) { return '?' }
  $s = ([string]$v).Trim()
  if ($s) { $s } else { '?' }
}

$sfi = Trim1 $d.solarflux
$a   = Trim1 $d.aindex
$k   = Trim1 $d.kindex
$sn  = Trim1 $d.sunspots

"SFI $sfi A $a K $k SN $sn"
