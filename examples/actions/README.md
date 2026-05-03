# Example Action scripts

Drop-in scripts you can wire to the **Actions** subsystem
(`/#/actions` in the operator UI). Two flavors:

- `posix/` — bash for Linux + macOS (Raspberry Pi too).
- `windows/` — PowerShell, paired with a tiny `.cmd` launcher you point
  graywolf at as the command path.

## How graywolf invokes a script

The runner exec's your binary directly (no shell). Every script in this
tree reads its inputs from environment variables, never from `argv`,
and never from `eval`. See [docs/handbook/actions.html](../../docs/handbook/actions.html)
for the full execution contract.

Variables you can use:

| Env var | Meaning |
|---|---|
| `GW_ACTION_NAME` | Action's configured name (e.g. `weather`) |
| `GW_SENDER_CALL` | Sender's APRS callsign (with SSID) |
| `GW_OTP_VERIFIED` | `true` / `false` |
| `GW_OTP_CRED_NAME` | OTP credential label, if any |
| `GW_SOURCE` | `rf` or `is` |
| `GW_INVOCATION_ID` | Audit row primary key |
| `GW_ARG_<KEY>` | One per declared arg; key uppercased, non-alnum → `_` |

Reply is the first line of stdout. Keep it short — graywolf snippets to
50 runes for the on-air reply ("`ok: <snippet>`"). Exit non-zero with a
short stderr/stdout to surface as `error: <detail>`.

## Wiring a script

1. Drop the script somewhere stable (e.g. `/usr/local/lib/graywolf/actions/weather.sh`).
2. `chmod +x weather.sh` (POSIX only).
3. Web UI → **Actions** → **New** → fill in:
   - **Name**: `weather` (matches the `@@<otp>#weather` users will send).
   - **Type**: command.
   - **Command path**: full absolute path to the script (or to the
     `.cmd` launcher on Windows).
   - **Arg schema**: one row per `GW_ARG_*` you read. Set
     `required` and a `regex` if the input shape matters.
   - **OTP credential**: optional but strongly recommended for any
     action that costs money or moves a physical thing.
   - **Sender allowlist**: also recommended.
   - **Timeout**: 10s default; bump for SMS/HA round-trips if your
     network is slow.
4. **Test** with the per-row Test dialog before letting it loose
   on-air. Test bypasses OTP + allowlist but exercises the executor.

## Per-script config (env vars to set in the graywolf service unit)

Many scripts need credentials. The runner inherits graywolf's process
env, so set them in the systemd unit (`Environment=` lines), in
`/etc/default/graywolf`, or in the service's environment file. **Do
not** put secrets in the script itself or pass them as Action args.

| Script | Env required |
|---|---|
| `echo` | — |
| `weather` | — |
| `sms` | `TWILIO_ACCOUNT_SID`, `TWILIO_AUTH_TOKEN`, `TWILIO_FROM`; optional `APRSFI_API_KEY` for location follow-up |
| `ha-lights` | `HA_URL`, `HA_TOKEN` |
| `ha-garage` | `HA_URL`, `HA_TOKEN`, `HA_GARAGE_ENTITY` |
| `solar` | — |
| `iss` | — |
| `uptime` | — |

## Suggested arg schemas (paste into the Edit Action modal)

`echo`:
- `msg` — required, regex `.+`

`weather`:
- `station` — required, regex `^[A-Za-z0-9]{4}$` (ICAO airport code)

`sms`:
- `to` — required, regex `^\+[1-9][0-9]{6,14}$`
- `msg` — required, regex `.+`

`ha-lights`:
- `entity` — required, regex `^light\.[a-z0-9_]+$`
- `state` — required, regex `^(on|off)$`
- `brightness` — optional, regex `^([0-9]|[1-9][0-9]|1[0-9]{2}|2[0-4][0-9]|25[0-5])$`

`ha-garage`:
- `action` — required, regex `^(open|close|toggle)$`

`solar`, `iss`, `uptime` — no args.

## Example on-air sessions

```
> @@123456#echo msg=hello
< ok: KE0XYZ said: hello

> @@123456#weather station=KDEN
< ok: KDEN 030253Z 27008KT 10SM CLR 22/M01 A3025

> @@123456#solar
< ok: SFI 142 A 8 K 2 SN 78

> @@123456#ha-garage action=open
< ok: garage open

> @@123456#sms to=+15551234567 msg=on my way
< ok: sms sent to +15551234567
```

## Security notes

- Scripts run as the graywolf service user. Treat command actions
  exactly like a `cron` entry: anything that user can do, an authorized
  sender can ask you to do.
- Quote every env var. `do-thing -- "$GW_ARG_ROOM"` is safe;
  `do-thing $(echo $GW_ARG_ROOM)` and `eval` are not.
- For irreversible or destructive actions (open garage, unlock door,
  send money), set `OTPRequired = true` AND a sender allowlist. The
  classifier checks the allowlist before the OTP probe, so a denied
  sender can't enumerate which OTP digits validate.
- `sms` and `ha-*` make outbound network calls. Audit the script
  before installing — the same pattern can be re-pointed at any URL.

## Windows specifics

Each `.ps1` ships next to a same-named `.cmd` launcher. Set the
Action's **Command path** to the `.cmd` file, not the `.ps1` —
graywolf calls `CreateProcess` directly and Windows file associations
don't apply. The `.cmd` just runs `powershell.exe -File <same-name>.ps1`
with the inherited environment.

PowerShell 5.1 (built into Windows 10/11) is enough. If you have
PowerShell 7 (`pwsh.exe`), edit the `.cmd` files to call `pwsh` for
better TLS and JSON handling.
