# Reference: PowerShell handlers (Windows)

Authoring guide for PowerShell `.ps1` handlers on Windows 10 / 11.
PS 5.1 (built-in) is sufficient; PS 7 (`pwsh`) is preferred for
better TLS and JSON. Every shipped Windows handler in
`examples/actions/windows/` is PSScriptAnalyzer-clean.

> **Read [`SKILL.md`](SKILL.md) first.** The threat model, credential
> blast-radius discussion, and universal invariants live there. This
> file is the language idioms.

## What graywolf invokes

Graywolf calls `CreateProcess` directly — Windows file associations
do not apply. The Action's **Command path** in the operator UI must
point at a small `.cmd` launcher, which then invokes
`powershell.exe -NoProfile -File <name>.ps1` with the inherited
environment.

argv (`$args[0..]`) layout:

| Index | `kv` mode | `freeform` mode |
|---|---|---|
| `$args[0]` | action name | action name |
| `$args[1]` | sender callsign | sender callsign |
| `$args[2]` | `true` / `false` (OTP verified) | same |
| `$args[3..N]` | `k=v` tokens, schema order | the entire raw payload, one token |

Environment is identical to POSIX: `GW_ACTION_NAME`,
`GW_SENDER_CALL`, `GW_OTP_VERIFIED`, `GW_OTP_CRED_NAME`, `GW_SOURCE`,
`GW_INVOCATION_ID`, `GW_ARG_<KEY>` (kv) or `GW_ARG` (freeform).
**Prefer the env vars over `$args`** — they survive arg-schema
ordering changes.

## Mandatory prelude

```powershell
# <NAME>.ps1 -- <one-sentence purpose>
# Wire: @@<otp>#<name> <kv|freeform shape>
# Reply: <first-line success snippet>
# Config: <env vars + credential scope>
# Deps:   <cmdlets / external tools>

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
```

- The `# AUDIT THIS:` block, when present, sits above the
  `Set-StrictMode` line.
- `Set-StrictMode -Version Latest` turns reading an undefined variable,
  calling a non-existent property, and out-of-range index access into
  hard errors. A typo like `$payloud` fails loudly instead of
  silently being `$null`.
- `$ErrorActionPreference = 'Stop'` upgrades non-terminating cmdlet
  errors to terminating ones — the first failure halts the script.
- Both are non-negotiable. PSScriptAnalyzer will not catch their
  absence; the skill must verify them by reading the file.

## The `.cmd` launcher

For every `<name>.ps1` ship a sibling `<name>.cmd`:

```bat
@echo off
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0<name>.ps1" %*
```

Notes:

- `%~dp0` resolves to the directory the `.cmd` lives in, so the
  launcher works no matter where graywolf calls it from.
- `-NoProfile` skips loading the user's PowerShell profile — fewer
  surprises and a tiny startup speedup.
- `-ExecutionPolicy Bypass` is scoped to this single invocation; it
  does not change the system policy.
- `%*` passes all argv through unmodified.
- The Action's **Command path** in the operator UI points at the
  `.cmd`, not the `.ps1`.

If the operator runs PowerShell 7, change `powershell.exe` to
`pwsh.exe` (and confirm it is on `PATH` for the graywolf service
account).

## Reading inputs

### kv mode

```powershell
$to     = $env:GW_ARG_TO
$msg    = $env:GW_ARG_MSG
$sender = $env:GW_SENDER_CALL

if (-not $to)  { [Console]::Error.WriteLine("missing to=");  exit 64 }
if (-not $msg) { [Console]::Error.WriteLine("missing msg="); exit 64 }
```

`Set-StrictMode` makes a typo like `$env:GW_ARG_MSGS` fail
immediately rather than silently yielding `$null`. Always check
required args explicitly even so — the strict-mode failure mode is
"throw at first read," which makes the operator's diagnostic less
clear than the explicit check above.

### freeform mode

```powershell
$action      = $args[0]
$sender      = $args[1]
$otpVerified = $args[2]
$payload     = if ($env:GW_ARG) { $env:GW_ARG } else { $args[3] }

if (-not $payload) {
    [Console]::Error.WriteLine("payload missing")
    exit 64
}

# .NET regex with named groups validates AND splits in one match.
$rx = '^(?<num>\+[1-9][0-9]{6,14})\s+(?<msg>.+)$'
$match = [regex]::Match($payload, $rx)
if (-not $match.Success) {
    [Console]::Error.WriteLine("expected '+<E164> <message>'")
    exit 64
}
$number  = $match.Groups['num'].Value
$message = $match.Groups['msg'].Value
```

Why named groups: indexing by name is durable. Adding a non-capturing
group later does not shift positional indices. Match objects are
in-process; no `Invoke-Expression`, no subshell.

## Revalidate inside the handler

```powershell
# \p{Cc} is Unicode "Other, Control" -- the .NET counterpart of POSIX [:cntrl:].
if ($message -match '\p{Cc}') {
    [Console]::Error.WriteLine("message contains control characters")
    exit 65
}
if ($message.Length -lt 1 -or $message.Length -gt 160) {
    [Console]::Error.WriteLine("message length out of range (1..160)")
    exit 65
}
```

For numeric args, validate with `[int]::TryParse` plus a range
check — not just a regex, because `1e5` looks numeric to a regex but
is not an integer.

## Verifying required env

```powershell
foreach ($v in 'TWILIO_ACCOUNT_SID','TWILIO_AUTH_TOKEN','TWILIO_FROM') {
    if (-not (Get-Item "env:$v" -ErrorAction SilentlyContinue)) {
        [Console]::Error.WriteLine("$v not set")
        exit 1
    }
}
```

Use `Get-Item env:$v` not `$env:$v` — the latter cannot interpolate
the variable name. Run this loop immediately after the input checks
so the script fails fast before touching the network.

## Calling external services

### Use cmdlet hashtable bodies, not strings

```powershell
$form = @{
    From = $env:TWILIO_FROM
    To   = $number
    Body = $message
}

try {
    $resp = Invoke-RestMethod -Method Post -Uri $uri -Headers $headers `
        -Body $form -TimeoutSec 8
} catch {
    [Console]::Error.WriteLine("downstream rejected: $($_.Exception.Message)")
    exit 1
}
```

When `-Body` is a hashtable on a `POST`/`PUT`, `Invoke-RestMethod`
serializes it as `application/x-www-form-urlencoded` and URL-encodes
each value. We never construct the request body via string
concatenation. An `&` in `$message` cannot smuggle extra form fields.

For JSON bodies, use `ConvertTo-Json` on a hashtable and pass the
result through `-Body` with `-ContentType 'application/json'`. Never
hand-build JSON with string formatting.

### `Start-Process` and external binaries

If you must shell out to an external command:

```powershell
# GOOD -- array form. Each element is its own argv entry.
Start-Process -FilePath 'C:\Tools\send.exe' `
    -ArgumentList @('--to', $number, '--body', $message) `
    -Wait -NoNewWindow

# BAD -- string form. PowerShell re-splits on whitespace and
# re-parses quotes. A space in $message becomes a new flag.
Start-Process -FilePath 'send.exe' -ArgumentList "--to $number --body $message"
```

`-ArgumentList` accepts an array. Always use the array form when any
element comes from user input.

### Auth headers

```powershell
$auth = [Convert]::ToBase64String([Text.Encoding]::ASCII.GetBytes("${sid}:${tok}"))
$headers = @{ Authorization = "Basic $auth" }
```

For HMAC-style auth, compute the digest with
`[System.Security.Cryptography.HMACSHA256]` against the request body
*after* the body is serialized to a fixed byte sequence; do not HMAC
the hashtable.

## Reply contract

```powershell
"sent $($resp.sid)"     # success: stdout, first line is the on-air reply
exit 0
# ----
[Console]::Error.WriteLine("downstream rejected: $msg")
exit 1
```

Same exit-code conventions as POSIX (`64` usage, `65` data,
`69` service unavailable, `75` retry hint).

## Anti-pattern table

| Anti-pattern | Fix |
|---|---|
| `Invoke-Expression $payload` (or `iex`) | Validate, then call cmdlets directly. |
| `cmd.exe /c "send.exe $payload"` | `Start-Process -ArgumentList @(...)` with an array. |
| `Start-Process -ArgumentList "..."` (string) | Always use the array form. |
| `Invoke-RestMethod -Body "From=$f&To=$t"` (string) | Hashtable `-Body @{}`. |
| Omit `Set-StrictMode` | Typoed variables silently expand to `$null`. Always set strict mode. |
| `Invoke-RestMethod $url` with `$url` from payload | SSRF. The URL must be a constant or built from validated tokens. |
| `Write-Host` for diagnostics | `[Console]::Error.WriteLine(...)` — `Write-Host` does not go to stderr. |
| Storing the API key in the script | Read from `$env:VAR`. |
| `Invoke-RestMethod -Body $msg -ContentType 'application/json'` (string) | `ConvertTo-Json @{ msg = $msg }`. |
| `Start-Process cmd.exe -ArgumentList @('/c', $payload)` | The array form does **not** save you when the target binary is itself a shell. cmd.exe parses its own arguments — `&`, `&&`, `\|`, `>` all chain. Don't put `cmd.exe`/`bash`/`pwsh -Command` in `-FilePath`; call the real target binary directly. |
| `Stop-Process -Name $userInput` (cmdlet wildcards) | Cmdlets accept `*`, `?`, `[abc]`, `,`. `Stop-Process -Name *` kills every process the service can signal. Resolve to a PID first: `(Get-Process -Name $name).Id` and confirm exactly one match, *or* forbid `*?[],` in the regex **and** keep an in-script allowlist of process names. |
| `Stop-Service -Name $userInput` without an in-script allowlist | The schema regex says *shape* (charset, length); it does not say *which services*. `WinDefend`, `vss`, `EventLog` are all valid shapes and disastrous targets. Anchored regex + `if ($name -notin $allowed) { exit 65 }`. |
| `& "C:\scripts\$userInput.ps1"` | Path traversal (`..\..\evil`) plus arbitrary-script-trigger by design. Refuse the dispatcher; convert each script to its own Action. If a dispatcher is unavoidable: `[IO.Path]::GetFullPath((Join-Path 'C:\scripts' "$name.ps1"))` and confirm the result `StartsWith('C:\scripts\', [StringComparison]::OrdinalIgnoreCase)` *and* allowlist the names. |
| `Invoke-Command -ComputerName $h -ScriptBlock { ... $args[0] ... }` with payload | `ScriptBlock` is PowerShell source on the remote — same class as `Invoke-Expression`. Pass parameters via `-ArgumentList` and use named parameters inside the block, never string-interpolate user data into the block source. |
| `pwsh -Command $payload` / `powershell -Command $payload` | Same as `Invoke-Expression`. Both accept a string of PowerShell source. |
| `Set-Content -Path "C:\out\$file"` with payload as `$file` | Path traversal on writes, too. Canonicalize and StartsWith-check the same way as for execution. |

## Linter — mandatory

```powershell
Install-Module PSScriptAnalyzer -Scope CurrentUser   # one-time setup
Invoke-ScriptAnalyzer -Path .\handler.ps1 -Severity Information,Warning,Error
```

Diagnostics are bugs. Fix them. Never use `[Diagnostics.CodeAnalysis.SuppressMessageAttribute]`
to silence a finding. The skill must run this command on the
generated file and confirm clean output before declaring the script
ready.

Common diagnostics worth knowing:

- **PSAvoidUsingInvokeExpression** — never use `Invoke-Expression` /
  `iex`.
- **PSUseDeclaredVarsMoreThanAssignments** — flags dead variables;
  often a sign of a typo.
- **PSAvoidUsingPlainTextForPassword** — credentials must come from
  env or `Get-Credential`.
- **PSAvoidUsingWMICmdlet** — `Get-CimInstance` instead of `Get-WmiObject`.

## Skeleton — copy and adapt

### kv-mode skeleton

```powershell
# <NAME>.ps1 -- <one-sentence purpose>
# Wire: @@<otp>#<name> key1=<shape> key2=<shape>
# Reply: success: "ok <id> for <sender>"; failure: "<error snippet>"
# Config:
#   MY_API_KEY      required. Scope: <e.g. "non-admin user, project-scoped">
# Deps:   Invoke-RestMethod

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

# OTP defense in depth. Skip ONLY for genuinely read-only Actions.
if ($env:GW_OTP_VERIFIED -ne 'true') {
    [Console]::Error.WriteLine("otp not verified")
    exit 77
}

$sender = $env:GW_SENDER_CALL
$key1 = $env:GW_ARG_KEY1
$key2 = $env:GW_ARG_KEY2
if (-not $key1) { [Console]::Error.WriteLine("missing key1="); exit 64 }

if ($key1 -notmatch '^...$') {
    [Console]::Error.WriteLine("invalid key1: $key1")
    exit 65
}

if (-not (Get-Item env:MY_API_KEY -ErrorAction SilentlyContinue)) {
    [Console]::Error.WriteLine("MY_API_KEY not set")
    exit 1
}

$body = @{
    field1 = $key1
    field2 = $key2
}
$headers = @{ Authorization = "Bearer $($env:MY_API_KEY)" }

try {
    $resp = Invoke-RestMethod -Method Post `
        -Uri 'https://api.example.com/v1/do-thing' `
        -Headers $headers -Body $body -TimeoutSec 8
    if ($resp.id) {
        $shortId = ($resp.id.ToString()).Substring(0, [Math]::Min(8, $resp.id.ToString().Length))
        "ok $shortId for $sender"
        exit 0
    }
} catch {
    [Console]::Error.WriteLine("downstream rejected: $($_.Exception.Message)")
    exit 1
}

[Console]::Error.WriteLine("downstream rejected: no id in response")
exit 1
```

### freeform-mode skeleton

```powershell
# <NAME>.ps1 -- <one-sentence purpose>
# Wire: @@<otp>#<name> <freeform shape>
# Reply: success: "sent <id> to <sender>"; failure: "<error snippet>"
# Config:
#   SECRET          required. Scope: <e.g. "least-privilege, no admin role">
# Deps:   Invoke-RestMethod

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

# OTP defense in depth.
if ($env:GW_OTP_VERIFIED -ne 'true') {
    [Console]::Error.WriteLine("otp not verified")
    exit 77
}

$action      = $args[0]
$sender      = $args[1]
$otpVerified = $args[2]
$payload     = if ($env:GW_ARG) { $env:GW_ARG } else { $args[3] }

if (-not $payload) {
    [Console]::Error.WriteLine("payload missing")
    exit 64
}

$rx = '^(?<g1><pattern1>)\s+(?<g2><pattern2>)$'
$match = [regex]::Match($payload, $rx)
if (-not $match.Success) {
    [Console]::Error.WriteLine("expected '<format>'")
    exit 64
}
$f1 = $match.Groups['g1'].Value
$f2 = $match.Groups['g2'].Value

if ($f2 -match '\p{Cc}') {
    [Console]::Error.WriteLine("f2 contains control characters")
    exit 65
}
if ($f2.Length -lt 1 -or $f2.Length -gt <max>) {
    [Console]::Error.WriteLine("f2 length out of range")
    exit 65
}

if (-not (Get-Item env:SECRET -ErrorAction SilentlyContinue)) {
    [Console]::Error.WriteLine("SECRET not set")
    exit 1
}

$body = @{ f1 = $f1; f2 = $f2 }
$headers = @{ Authorization = "Bearer $($env:SECRET)" }

try {
    Invoke-RestMethod -Method Post -Uri 'https://api.example.com/v1/do' `
        -Headers $headers -Body $body -TimeoutSec 8 | Out-Null
    "ok"
    exit 0
} catch {
    [Console]::Error.WriteLine("downstream rejected: $($_.Exception.Message)")
    exit 1
}
```

## Operator postamble template

```
Install path:  C:\graywolf\actions\<name>.ps1
Launcher:      C:\graywolf\actions\<name>.cmd  (paired -- contents below)

  @echo off
  powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0<name>.ps1" %*

Lint (mandatory):
  Install-Module PSScriptAnalyzer -Scope CurrentUser   # one-time
  Invoke-ScriptAnalyzer -Path C:\graywolf\actions\<name>.ps1 -Severity Information,Warning,Error

Env vars (System Properties -> Environment Variables, or set per-service
via sc.exe / NSSM, then restart graywolf):
  MY_API_KEY=...
  ...

Wire it up: /#/actions -> New
  Name: <name>
  Type: command
  Command path: C:\graywolf\actions\<name>.cmd       <-- the .cmd, not the .ps1
  Arg schema: <one row per declared arg>
  OTP credential: <chosen credential | none>
  Sender allowlist: <callsigns | empty>
  Timeout: <seconds>

Test: per-row Test dialog before letting it loose on-air.
```
