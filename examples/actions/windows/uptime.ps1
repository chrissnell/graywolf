# Action: uptime
# Grammar:  @@<otp>#uptime
# Args:     none
# Reply:    "up <Xd Yh> load <pct cpu> root <pct used>"

$ErrorActionPreference = 'Stop'

$os = Get-CimInstance Win32_OperatingSystem
$boot = $os.LastBootUpTime
$span = (Get-Date) - $boot
$up = "$($span.Days)d$($span.Hours)h"

# Approximate "load" via CPU queue length / utilization.
try {
  $cpu = (Get-CimInstance Win32_Processor | Measure-Object -Property LoadPercentage -Average).Average
  $load = "${cpu}%"
} catch {
  $load = '?'
}

try {
  $c = Get-PSDrive C
  $total = [double]($c.Used + $c.Free)
  $pct = if ($total -gt 0) { [int](100 * $c.Used / $total) } else { 0 }
  $root = "${pct}%"
} catch {
  $root = '?'
}

"up $up load $load root $root"
