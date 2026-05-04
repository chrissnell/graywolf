---
name: writing-graywolf-action-handlers
description: Use when authoring a Graywolf Actions handler — POSIX/bash, PowerShell, or webhook receiver — that fires on inbound APRS messages of the form `@@<otp>#<action>`. Triggers when an operator says "write me an Action script", "scaffold an Action handler", "I need an SMS/garage/notify Action", or asks how to wire a script to a remote sender safely. Covers OTP/allowlist sizing, kv-vs-freeform mode, argument validation, and the safety invariants that keep an over-the-air payload from compromising the host.
---

# Writing Graywolf Action handlers

## Overview

A Graywolf **Action handler** runs locally — or on a webhook receiver
you control — when a remote APRS sender transmits
`@@<otp>#<name> ...`. The payload arrives over the air and must be
treated as **untrusted input from a public radio frequency**. A handler
that mishandles it can shell-inject the host, smuggle SMS to arbitrary
numbers, unlock the wrong door, or DOS a downstream API.

This skill walks an operator through the **interview** that shapes a
correct handler and then **emits** an idiomatic, lint-clean script that
follows every invariant the Graywolf project documents. Three flavors:

- **POSIX shell** (`bash` for Linux + macOS) — see [`reference-posix.md`](reference-posix.md)
- **PowerShell** (Windows 10/11, PS 5.1 or `pwsh` 7) — see [`reference-powershell.md`](reference-powershell.md)
- **Webhook** (HTTP receiver, examples in Python Flask/aiohttp) — see [`reference-webhook.md`](reference-webhook.md)

OTP and sender-allowlist sizing applies to all three: see
[`reference-otp-allowlist.md`](reference-otp-allowlist.md).

## Threat model — always Internet-hostile

**Every Action handler is exposed to arbitrary attacker-controlled input
from the public Internet and the APRS-IS / RF networks.** This is true
*regardless of what the operator believes the exposure is*:

- APRS-IS is a public, federated, unauthenticated message bus. Any
  station with an Internet connection can transmit to any addressee.
- RF traffic is monitored at scale by aprs.fi, aprs2.net mirrors, SDR
  hobbyists, and anyone within range of a digipeater. Sender callsigns
  can be forged on RF; SSID stripping further widens the impersonation
  surface.
- The OTP and sender allowlist are *probabilistic* defenses — strong
  defenses, but the script's safety must not depend on them holding.
  Write the script as if every byte of input is attacker-chosen.

The skill must **proactively explain this** to the operator if they
say things like "this is just for me" or "only my friends will use
it". Their model of the exposure is wrong; correct it before
generating code.

### Trust arguments are out of scope

"Only my friends," "only licensed hams," "I'm the only one with the
OTP," "my callsign is on the FCC database so I'm verified" — none of
these are authentication boundaries. The FCC license database is
public, callsigns are forged routinely on APRS-IS, OTP is a
30-second / six-digit window with a knowable secret, and a sender
allowlist is a callsign string match (no per-user identity). When
the operator's argument for relaxing a defense is the *identity* of
the senders, the skill must refuse and re-explain the threat model.
When the argument is the *cost* of the defense (latency, complexity,
operator effort), it can be weighed. Identity-based arguments never
weigh.

### The threat is not just side effects — read-only Actions can leak

An Action that "only reads" can still exfiltrate state. `journalctl
-u <pattern>` discloses logs from services the sender should not
see; `cat /etc/<file>` discloses arbitrary file contents the service
account can read; `kubectl logs -l <selector>` discloses pod logs;
`Get-EventLog -LogName Security` discloses authentication history.
Apply the same blast-radius reasoning to read-only Actions: *what
can the underlying user account read?* That is the worst-case
disclosure surface, regardless of what the operator believes the
Action exposes.

### Blast radius — what does this handler actually let me do?

Most handlers integrate with an external service (Twilio, AWS, Home
Assistant, a smart-home cloud, a VoIP provider, a downstream webhook).
The handler holds the credentials for that service. **A handler bug
or a regex gap does not just expose the one Action — it exposes
everything the credential can do.**

Walk the operator through this explicitly during the interview. The
question is *not* "what does this Action do?" — it is "if a remote
attacker successfully drove this handler with arbitrary input, what
could they reach?" Concrete examples:

| Integration | Naive scoping | What goes wrong |
|---|---|---|
| **AWS SNS** with default IAM user / admin access key | Send any SMS | Attacker can pivot to S3, SES, Lambda, IAM, billing — full account takeover via the same key. |
| **Twilio** with main account auth token | Send one SMS | Attacker can send unlimited SMS / MMS / voice, run up the bill, originate calls from your number. |
| **Home Assistant** with a long-lived access token | Toggle one entity | Attacker can call any service: unlock doors, disable alarms, read every sensor. |
| **Generic SaaS API key** | One endpoint | Attacker can usually reach every endpoint that key authenticates. |
| **SSH / pkexec / sudo from the handler** | Run one command | Attacker reaches the underlying user's full shell privilege. |

The countermeasures are always the same:

1. **Scope the credential as narrowly as the service allows.**
   - AWS: a dedicated IAM user or role with a *single* `sns:Publish`
     statement, scoped via `Resource:` to one topic ARN, plus a
     `Condition:` on the destination phone number prefix where
     possible. Never the AWS root key, never an admin policy, never
     `Resource: "*"`.
   - Twilio: a sub-account auth token, not the master account.
   - Home Assistant: a long-lived access token tied to a *least-privilege
     user* with limited entity access, not the operator's admin user.
   - SSH: a dedicated key with a `command="..."` restriction in
     `authorized_keys`, or `forced-command` via the agent.
2. **Validate every byte of user input that touches the request.**
   E.164 number? Anchor the regex. State `on|off`? Anchor it. Entity
   name? Anchor it.
3. **Cap the rate.** Set `RateLimitSec` on the Action so a runaway
   sender cannot drain the credential's quota.
4. **Log the credential scope** in the script's header comment so a
   future reader can audit it without reading the IAM console.

The skill must explain the blast-radius concept in language the
operator can act on, then ask the operator: *"Show me — or describe —
the credential this script will use. What is its scope?"* If the
operator does not know the answer, generate the script anyway but
include a prominent `# AUDIT THIS:` block at the top listing the
scoping work that must happen before the Action goes live.

## When to use

- Operator asks for a handler script for a specific Action (SMS, home
  automation, notification, lookup, status, etc.).
- Operator has a draft script and wants it reviewed against the
  handler-safety contract.
- A new Action will perform anything irreversible, anything that
  touches money, anything that triggers a physical actuator, or
  anything that talks to an external API.
- Operator asks "kv or freeform?", "do I need OTP?", "is this safe?".

**Skip this skill** for non-Action work (general bash, generic
PowerShell, unrelated webhook receivers). The patterns here are
specific to the Graywolf Actions execution contract.

## Interview — ask these questions in order

Don't generate code until every question is answered. Ask the operator
one block at a time; combine answers into the script.

### 1. What does the Action do?

One sentence. Examples:

- *Sends an SMS via Twilio.*
- *Toggles a Home Assistant light.*
- *Returns current solar weather.*
- *Posts a status note into a webhook receiver that logs to SQLite.*

Capture the verb and the side effect. The verb decides the Action's
`name`. The side effect decides everything below.

### 2. What is the side-effect class?

| Class | Examples | Implications |
|---|---|---|
| **Read-only** | weather lookup, ISS pass, uptime | OTP optional, allowlist optional, no extra hardening |
| **Notification** | SMS, push, log entry | OTP recommended, **allowlist required if cost-per-fire** (Twilio etc.) |
| **State change, reversible** | turn light on/off, post note | OTP required, allowlist recommended |
| **State change, irreversible / physical** | open garage, unlock door, ignition, money movement | **OTP required, allowlist required**, narrow regex on every arg |

If the operator is uncertain, default up one tier. Ask: "If a stranger
sent this Action with valid OTP, what is the worst they could do?"

### 3. Who can trigger it?

- *Just me.* → Sender allowlist with the operator's callsign(s) only.
- *A small group (family, club).* → Allowlist with each callsign.
  Mention SSID handling: graywolf strips SSID for the comparison
  (base-call match), so `KE0XYZ-9` and `KE0XYZ` both match `KE0XYZ`.
- *Anyone on the band.* → No allowlist. Only acceptable for read-only
  Actions or Actions whose side effect is harmless if abused.

The classifier checks the allowlist **before** the OTP probe, so a
denied sender cannot enumerate which OTP digits validate. Operators do
not need to design around this — just recommend the allowlist when the
side-effect class warrants it.

### 4. OTP credential — yes or no, and is the script defending in depth?

- **Yes** for any side-effect class except read-only.
- **Yes** if the Action consumes a paid API.
- **No** is acceptable for pure-information Actions whose worst-case
  abuse is a small amount of RF chatter.

If yes, the operator must mint an OTP credential in
`/#/actions` → Credentials before saving the Action. The skill should
remind them: *"Create the OTP credential first; the Action's OTP
credential dropdown will be empty until you do."*

The station is single-user; do not invent issuer / account fields. The
issuer is always Graywolf and the account is always the station
callsign (see `feedback_single_user_station`).

**Defense in depth: the script must also check `GW_OTP_VERIFIED`.**
The Action editor's `OTPRequired` toggle is the *first* defense; an
operator can flip it off in the UI without re-deploying the script.
Every generated handler that performs a side effect must therefore
re-assert OTP at the script level:

```bash
# bash
[[ "${GW_OTP_VERIFIED:-false}" == "true" ]] || { echo "otp not verified" >&2; exit 77; }
```

```powershell
# PowerShell
if ($env:GW_OTP_VERIFIED -ne 'true') {
    [Console]::Error.WriteLine("otp not verified")
    exit 77
}
```

```python
# Webhook receiver (Flask)
if request.form.get("otp_verified", "false") != "true":
    abort(401)
```

Skip this only for genuinely read-only Actions where unauthenticated
RF chatter is acceptable. Do not skip for anything that costs money,
moves a physical thing, or persists data downstream.

### 5. Wire format — kv or freeform?

| Question | If yes | If no |
|---|---|---|
| Are there 2+ structurally different inputs (entity + state, number + body)? | **kv** | go on |
| Will the natural sender phrasing be one chunk of text (an SMS, a notification body)? | **freeform** | **kv** |
| Will the value be passed straight through to a downstream API as one string? | **freeform** | **kv** |

Default to **kv** when in doubt. Freeform is friendlier on-air for
text-y payloads but pushes the splitting + revalidation onto the
handler script, which the operator must then write correctly.

### 6. Arguments — names, regexes, required?

For **kv** mode, list every argument with:

- `key` — short, lowercased, `[a-z][a-z0-9_]*`
- `required` — yes/no
- `regex` — anchor with `^` and `$`. Make it as narrow as the input
  shape allows. Examples in [`reference-otp-allowlist.md`](reference-otp-allowlist.md).

For **freeform** mode, the schema has a single synthetic row keyed
`arg`. The script does the splitting in one regex match (see the
platform-specific reference). The schema's regex still applies as a
first-pass filter; do not skip it.

### 7. Platform + invocation surface?

- *Linux / macOS* → POSIX shell. Use `reference-posix.md`.
- *Windows* → PowerShell. Use `reference-powershell.md`. Remember:
  graywolf calls `CreateProcess` directly, not the Windows shell, so
  the **Command path** in the UI must point at the `.cmd` launcher,
  which then calls `powershell.exe -NoProfile -File <name>.ps1`.
- *Different host or already-running service* → Webhook. Use
  `reference-webhook.md`.

If the operator's downstream is a SaaS endpoint (HTTP API), prefer a
**webhook Action** over a shell handler that just invokes `curl`.
Graywolf's templating + filter syntax (`|json` / `|url` / `|html`) does
the escaping correctly, removes the script-process round trip, and
keeps secrets in graywolf's config rather than in a shell environment.

### 8. Secrets, configuration, and credential scope

Enumerate every credential the script will need (API keys, tokens,
URLs, entity IDs). All of these go in the **graywolf service's
environment**, never in the script body, never as Action arguments,
never committed to the example handlers shipped with the project.

- Linux: `Environment=` lines in the systemd unit, or
  `/etc/default/graywolf`.
- macOS launchd: `EnvironmentVariables` dict in the plist.
- Windows: System Environment Variables panel; restart the graywolf
  service for changes to take effect.

The handler reads them via the language's normal env mechanism
(`$VAR` / `$env:VAR` / `os.environ[]`).

**For every credential, ask:** *"What can this credential do that
the Action does not need?"* Walk the operator through the
[blast-radius table](#blast-radius--what-does-this-handler-actually-let-me-do)
above. Specifically:

- AWS: confirm there is a dedicated IAM user or role *just for this
  Action*, with a policy that names the exact API operation
  (`sns:Publish`, `ses:SendEmail`, etc.) and a `Resource:` ARN that
  pins it to a single topic / queue / bucket. If the operator is
  about to use their root account key or an admin user, **stop and
  fix the IAM scope first** — generate a sample policy snippet for
  them.

  Example policy for SMS-via-SNS (note that direct phone-number
  publish requires `Resource: "*"` because there is no per-phone
  ARN; the `Condition` block is what scopes it):

  ```json
  {
    "Version": "2012-10-17",
    "Statement": [{
      "Effect": "Allow",
      "Action": "sns:Publish",
      "Resource": "*",
      "Condition": {
        "StringLike": {
          "sns:PhoneNumber": "+1*"
        }
      }
    }]
  }
  ```

  Example policy for SES email send (real ARN, no wildcard):

  ```json
  {
    "Version": "2012-10-17",
    "Statement": [{
      "Effect": "Allow",
      "Action": "ses:SendEmail",
      "Resource": "arn:aws:ses:us-east-1:123456789012:identity/alerts@example.com"
    }]
  }
  ```
- Twilio / Plivo / Vonage: confirm sub-account credentials, not the
  master account. Confirm the From number is fixed in env, not
  drawn from sender input.
- Home Assistant: confirm the long-lived token belongs to a
  least-privilege HA user with no admin role. If `entity` is a
  `GW_ARG_*`, narrow the regex to the specific domain
  (e.g. `^light\.[a-z0-9_]+$`, never `^.+$`).
- Anything that gives shell or SQL: this is almost always wrong —
  push the operator toward a dedicated service account or a webhook
  that brokers the call instead.

If the operator cannot describe the scope, the generated script
must carry a top-of-file `# AUDIT THIS:` comment block listing the
credential and the scoping work that must happen before deployment.

#### Webhook receiver shared secrets — entropy floor

For webhook Actions, the `X-Graywolf-Auth` shared secret (or any
HMAC key) **must be at least 32 bytes (256 bits) of entropy**.
Translate operator phrasing immediately:

| Operator says | Bytes of entropy | Verdict |
|---|---|---|
| `openssl rand -hex 16` | 16 (128 bits) | **REJECT** — too short, ask for 32 |
| `openssl rand -hex 32` | 32 (256 bits) | OK |
| `openssl rand -base64 32` | 32 (256 bits) | OK |
| "a 32-character hex string" | 16 | **REJECT** (32 hex chars = 16 bytes) |
| "a 64-character hex string" | 32 | OK |
| "a memorable passphrase I'll remember" | unknown / low | **REJECT** — generate it |

Hex doubles the character count, so "32 hex characters" is only 16
bytes. The skill must catch this language during the interview and
push the operator to `openssl rand -hex 32` (or equivalent) before
generating any code that depends on it.

## Universal safety invariants

These apply to **every** handler regardless of platform or wire mode.
They are non-negotiable. The platform reference files restate them in
the local idiom.

1. **Treat every byte of input as attacker-controlled.** APRS-IS is
   a public unauthenticated bus; RF callsigns can be forged. OTP and
   allowlist are real defenses but the script's correctness must not
   depend on them. The operator's belief that "only friends will use
   this" is irrelevant — write the script as if every byte came from
   a hostile stranger on the open Internet.
2. **Revalidate every argument inside the script.** Graywolf's
   sanitizer applies the Action's `arg_schema` regex, but the script's
   safety must not depend on the operator setting that regex strictly.
3. **Never re-introduce a shell.** The runner exec's your binary
   directly; do not undo that. This forbids **every** form that hands
   user data to an interpreter:
   - `eval`, `source`, `.` with user data
   - `sh -c`, `bash -c`, `zsh -c`, `dash -c`, `ksh -c`
   - `cmd /c`, `cmd.exe /c`, `pwsh -c`, `pwsh -Command`,
     `powershell -Command`, `Invoke-Expression`, `iex`
   - `Start-Process cmd.exe -ArgumentList @('/c', $userdata)` — the
     array form does not save you when the *target binary itself
     parses arguments as a shell* (cmd, sh, bash, sqlcmd, etc.).
     Call the real target binary directly.
   - `xargs sh`, `find -exec sh -c '...' {}`, `ssh host "$cmd"`,
     `Invoke-Command -ScriptBlock { ... $userdata ... }` — same
     class: nested shell on the far side of an argv-clean call.
4. **Pass user data as argv, not as a substring of a command line.**
   Use `--` terminators on commands that accept them. PowerShell:
   array `-ArgumentList`, never the string form. Python: `subprocess`
   with a list, `shell=False`.

   **Argv-clean is not the same as input-safe.** A binary can accept
   its arguments via argv and still interpret them as patterns,
   globs, paths, or sub-shell instructions:

   | Argv-clean call | Hidden footgun |
   |---|---|
   | `journalctl -u "$pattern"` | `-u` accepts globs / instance syntax (`*`, `foo@.service`) — discloses other services' journals. |
   | `Stop-Process -Name "$name"` | `-Name` accepts `*`, `?`, `[abc]`, `,` — `*` kills every process. |
   | `Stop-Service -Name "$name"` | Schema regex says shape, not identity — `WinDefend`, `vss`, `EventLog` are valid shapes. |
   | `& "C:\scripts\$name.ps1"` | Path traversal via `..\..\evil`. |
   | `cat "/var/log/$file"` | Same — discloses arbitrary files. |
   | `kubectl logs -l "$selector"` | Selectors and namespaces accept wildcards. |
   | `find / -name "$pattern" -delete` | Patterns + a destructive verb. |

   For each downstream binary, ask: *what wildcard / pattern / path
   syntax does this flag accept?* Validate the value to forbid
   metacharacters the binary would honor, **and** keep an in-script
   allowlist of the specific identities (services, units, files,
   selectors) that the Action is allowed to touch. The schema regex
   constrains *shape*; the in-script allowlist constrains *identity*.
   Both. Not either.
5. **Build HTTP request bodies via the library's encoder, not string
   concatenation.** `curl --data-urlencode`,
   `Invoke-RestMethod -Body @{...}`, `requests.post(..., data={...})`.
6. **Parameterized queries on every database write.** Never f-string,
   never `%`-format, never concatenate.
7. **Auto-escape on every templated render.** Jinja default,
   PowerShell's `[System.Web.HttpUtility]::HtmlEncode`, etc. Never
   `Markup(...)`, never `|safe`, never `{{ ...|safe }}`.
8. **Reply on the first line of stdout, ≤50 runes** (graywolf snippets
   it for the on-air `ok: <snippet>` reply). Diagnostics go to stderr.
9. **Exit non-zero on failure** with a short stderr line. Graywolf
   echoes the snippet as `error: <detail>` on-air.
10. **Run the linter for the language.** `shellcheck` for bash,
    `Invoke-ScriptAnalyzer` for PowerShell, `ruff` for Python. Fix
    every diagnostic; never disable rules.
11. **Test through the Test dialog** in `/#/actions` before letting
    the Action loose on-air. The dialog bypasses OTP + allowlist but
    exercises the executor and sanitizer.
12. **Webhooks only**: authenticate (HMAC or shared secret in a
    custom header), cap body size **before** parsing, terminate TLS
    in front of the receiver. See [`reference-webhook.md`](reference-webhook.md).
13. **Cmdlet / flag values are still attacker-controlled.** PowerShell
    cmdlets do not re-shell, but many accept wildcards (`-Name *`),
    comma-separated lists (`-Name a,b`), or paths that resolve outside
    their intended root. The same applies to POSIX flag values
    (`journalctl -u <pattern>`, `find -name <glob>`, `tar -C
    <path>`). Validate the *value*, not just the *call shape*.
14. **No "run-this-by-name" dispatcher Actions.** A handler that
    accepts a verb / script-name / service-name / file-name and
    routes to a corresponding command is a remote shell with extra
    steps. Each verb gets its own Action with its own parameterized
    handler. If you must dispatch (e.g. a small enum of verbs),
    use a switch-case on a tightly-anchored regex with **no
    fallthrough default that runs anything** — the `*)` branch must
    reject and exit non-zero. Never `eval`, `exec`, or
    `& "$path/$name"` a value derived from the wire.

## What graywolf passes to the handler

This is the execution contract. Memorize it; the platform references
restate it in their language.

### Command Actions (shell or PowerShell)

The runner exec's the binary directly — no shell, no `PATH` lookup,
no `system()`. argv layout:

| Index | `kv` mode | `freeform` mode |
|---|---|---|
| `$1` / `$args[0]` | action name | action name |
| `$2` / `$args[1]` | sender callsign | sender callsign |
| `$3` / `$args[2]` | `true` / `false` (OTP verified) | same |
| `$4..$N` / `$args[3..N]` | `k=v` tokens in schema order | the entire raw payload, one token |

Environment variables, set on every invocation:

- Always: `GW_ACTION_NAME`, `GW_SENDER_CALL`, `GW_OTP_VERIFIED`,
  `GW_OTP_CRED_NAME`, `GW_SOURCE` (`rf` or `is`), `GW_INVOCATION_ID`.
- kv: one `GW_ARG_<KEY>=<value>` per declared arg. Key is uppercased,
  non-alphanumeric becomes `_`.
- freeform: one variable named `GW_ARG=<raw payload>` (no key suffix).

**Prefer the env vars over argv** in scripts. They survive ordering
changes in the schema and they are easier to read.

### Webhook Actions

Graywolf POSTs `application/x-www-form-urlencoded` by default with
fields:

- Always: `action`, `sender_callsign`, `otp_verified`, `otp_cred`,
  `source`.
- kv: one field per declared arg (key = your schema key).
- freeform: one field named `arg` with the raw payload.

A custom body template can be configured per-Action with token
substitution and filters:

- `{{action}}`, `{{sender-callsign}}`, `{{otp-verified}}`,
  `{{otp-cred}}`, `{{source}}`
- kv: `{{arg.<key>}}`
- freeform: `{{arg}}`
- Filters (mandatory when interpolating into a structured
  destination): `{{arg|json}}` for JSON string contents,
  `{{arg|url}}` for URL paths or query strings, `{{arg|html}}` for
  HTML attribute / text content.

## Generation — how the skill emits the script

After the interview, follow this order. **Do not skip the lint step
and do not show the script to the operator until lint passes clean.**

1. **Pick the reference file** matching the platform answer.
2. **Read the template skeleton** in that reference; do not improvise
   from memory.
3. **Substitute** the action name, args, regexes, env-var names, and
   downstream call.
4. **Self-validate** the generated script against the
   universal-invariants checklist above. If anything is missing, fix
   it before the next step.
5. **Write the script to a file in the working directory** — do not
   only display it inline. The lint step in step 6 needs a file on
   disk.
6. **Lint the file (mandatory).** This is non-negotiable. Run the
   appropriate command and read its output:
   - POSIX shell: `shellcheck -s bash -S style <file.sh>`. Diagnostics
     are bugs. Fix the script — never add `# shellcheck disable`
     directives. If the operator does not have shellcheck installed,
     suggest `brew install shellcheck` / `apt install shellcheck` /
     `pacman -S shellcheck` and refuse to declare the script ready
     until it has been linted on their machine.
   - PowerShell: `Invoke-ScriptAnalyzer -Path <file.ps1> -Severity
     Information,Warning,Error`. Same rule — fix the script, do not
     suppress rules. If the operator does not have PSScriptAnalyzer,
     `Install-Module PSScriptAnalyzer -Scope CurrentUser`.
   - Webhook (Python): `ruff check <file.py>` and `ruff format
     --check <file.py>`. For Bandit users (recommended for webhook
     receivers), also run `bandit <file.py>` and triage every
     finding.
7. **Run a sanity test pass** for the safety invariants the linter
   does not catch:
   - `grep -nE '\beval\b|\b(sh|bash|zsh|dash|ksh)[[:space:]]+-c\b|Invoke-Expression|\biex\b|cmd(\.exe)?[[:space:]]+/c|pwsh[[:space:]]+(-c|-Command)|powershell[[:space:]]+-Command' <file>`
     must return no hits. This catches every shell-with-`-c` form,
     not just `sh -c`.
   - `grep -nE '\bxargs[[:space:]]+sh\b|find[[:space:]].*-exec[[:space:]]+sh' <file>` — nested-shell
     escalations of an argv-clean call.
   - `grep -n 'GW_ARG' <file>` — every reference must be quoted
     (POSIX) or pass through `Set-StrictMode` (PS).
   - PowerShell only: `grep -nE 'Stop-Process[[:space:]]+-Name|Stop-Service[[:space:]]+-Name|& *"[^"]*\$' <file>` —
     surface cmdlet-wildcard and `& "$path/$name"` patterns for
     manual review of the in-script allowlist.
   - For webhooks: confirm the receiver verifies a shared secret /
     HMAC and caps body size before parsing.
   - For any handler that builds a path from user input: confirm
     `realpath` / `[IO.Path]::GetFullPath` containment check is
     present.
   If any check fails, fix the script and re-run the linter.
8. **Emit the postamble in the chat reply, not as a file.** The
   script goes to disk; the postamble is operator-facing instructions
   that follow the script in the chat thread. It must include:
   - Suggested install path (`/usr/local/lib/graywolf/actions/<name>.sh`
     or equivalent).
   - The `chmod +x` (POSIX) or `.cmd` launcher (Windows) command.
   - The exact lint command that just passed (so the operator can
     re-run it on their host) **and** the install command if the
     linter is missing on the host.
   - The env-var wiring snippet for systemd / launchd / Windows
     services, with the credential names from the interview.
   - For webhook handlers: the systemd unit's `User=`,
     `ProtectSystem=strict`, `PrivateTmp=yes`, and
     `ReadWritePaths=` lines, plus a sample Caddy / nginx vhost
     terminating TLS in front of `127.0.0.1:5000`.
   - Action-editor field values from the interview (Name, Type,
     Command path / URL, Arg schema rows, OTP credential, Sender
     allowlist, Timeout, RateLimitSec, QueueDepth).
   - A reminder to use the per-row **Test** dialog in `/#/actions`
     before letting the Action loose on-air, and to recheck linting
     on any future edit.

**Linter unavailable on the host?** If the operator's machine does
not have shellcheck / PSScriptAnalyzer / ruff / bandit installed,
the skill **does not declare the script ready**. Instead, emit the
install command for their platform (`brew install shellcheck`,
`Install-Module PSScriptAnalyzer -Scope CurrentUser`,
`pip install ruff bandit`) and tell the operator: "*Install the
linter, run it on the file, fix any findings, and only then deploy.
The skill cannot certify a handler that has not been linted on the
target host.*" Visual review by the model is not a substitute.

Keep the generated script's comment density **high**. Operators are
not always strong scripters; the comments are the operator-facing
documentation for what each line is doing and why. Mirror the comment
style of `examples/actions/posix/sms-freeform.sh` and
`examples/actions/windows/sms-freeform.ps1`.

## Anti-patterns the skill must reject

If the operator asks for any of these, **push back** with the safer
alternative. Do not generate the unsafe form.

| Operator request | Counter |
|---|---|
| "Just `eval` the args, it's easier." | Re-parses payload as shell — full RCE. Use named args + regex. |
| "Build the URL with `+ $GW_ARG_FOO` so I can use it as a query string." | Use `--data-urlencode` (curl) / hashtable `-Body` (PS) / `params={}` (requests). String-concat smuggles `&` and `=`. |
| "Skip OTP, the allowlist is enough." | Allowlist proves the *callsign*, not the operator pressing the key. OTP proves possession of the credential. Use both for irreversible actions. |
| "Inline the API key in the script." | The script lands in git eventually. Use the graywolf service env. |
| "Skip the auth header on the webhook, the URL is unguessable." | URL leaks via TLS SNI, DNS, server logs, and shoulder surfing. Add HMAC or shared secret. |
| "Disable shellcheck SC2086." | The diagnostic is the bug. Quote the variable. |
| "Use `{{arg.msg}}` directly in the JSON body template." | A `"` in `msg` breaks the JSON. Use `{{arg.msg|json}}`. |
| "Use my AWS root key / admin user — IAM is a hassle." | One regex gap = full account takeover. Mint a dedicated IAM user with a single-statement policy scoped to one ARN. |
| "Use my Home Assistant admin token, it already works." | One bad regex unlocks every entity HA can reach. Mint a least-privilege HA user and a token bound to it. |
| "I'll skip linting just this once, the script is short." | Short scripts have the same footguns as long ones — quoting, control chars, missing `--`. Lint always; never declare done before lint passes. |
| "Just `bash -c "$GW_ARG"` then." / "Just `pwsh -Command $payload`." | Same as `eval` / `Invoke-Expression`. The runner exec's your binary directly so the shell is not in the loop; piping the payload back through *any* shell-with-`-c` undoes that. Refuse all forms: `sh -c`, `bash -c`, `zsh -c`, `dash -c`, `cmd /c`, `pwsh -c`, `powershell -Command`. |
| "Make me a `@@<otp>#run <command>` Action so I can run any command." | This is a remote shell on a public bus. **Refuse the design.** Each verb gets its own Action: `@@<otp>#restart-nginx`, `@@<otp>#rotate-logs`. If you need flexibility, use Tailscale + SSH, not a graywolf Action. |
| "Let me dispatch by name with a switch-case." | Acceptable shape *only* with: anchored verb regex `^(verb1\|verb2)$`; each branch calls a fixed argv (no variable-as-command); the default `*)` branch rejects with `exit 65` and a stderr line; no `eval` / `& $name` / `journalctl -u $name` in any branch. |
| "Pass the user's input to `journalctl -u`/`systemctl`/`ssh`/`find -exec`/`xargs sh`/`Stop-Process -Name`/`kubectl logs -l`." | Argv-safe binaries are not all input-safe. These accept patterns, glob expansions, or pass user data to nested shells. Validate the value to forbid metacharacters the binary honors *and* keep an in-script allowlist of identities the Action may touch. |
| "`Stop-Process -Name $name` — cmdlets are safe from injection." | Cmdlets accept wildcards (`*`, `?`, `[abc]`) and comma-separated lists. `-Name *` kills every process the service can signal. Resolve to a PID first (`(Get-Process -Name $name).Id`, confirm exactly one match) or forbid `*?[],` in the regex *and* keep an in-script allowlist. |
| "`& "C:\scripts\$name.ps1"` — let me run any of my scripts by name." | Path traversal (`..\..\evil`) plus arbitrary-script-execution by design. Refuse the dispatcher; convert each script to its own Action. If a dispatcher is unavoidable, canonicalize with `[IO.Path]::GetFullPath`, confirm the result `StartsWith` the base directory, and keep an explicit allowlist of script names. |
| "The schema regex on `name` is enough — I don't need an in-script allowlist." | Schema regex is *shape*; it does not constrain *identity*. A regex that accepts `[a-z]+` does not say which `[a-z]+` the Action may target. Keep both: anchored regex on the wire, exact-match allowlist inside the script. |
| "`{{arg.msg}}` is fine in my JSON template — the regex never lets quotes through." | Body templates and arg-schema regexes live on different lifecycles. The regex protects today's shape; the template will outlive it. A future regex-widening to allow apostrophes silently breaks the template's safety. **The filter is the load-bearing defense; the regex is a friendliness check.** Always use `\|json` / `\|url` / `\|html`. |
| "I trust my hams / it's just my friends / only I have OTP." | Identity is not authentication. Refuse the relaxation and re-explain the threat model. See "Trust arguments are out of scope" above. |

## Final checklist (run before showing the script)

- [ ] First line of stdout is the on-air reply, ≤50 runes.
- [ ] Every error path writes a short stderr line and exits non-zero.
- [ ] Every external command receives user data via argv, not concatenation.
- [ ] Every regex is anchored with `^` and `$`.
- [ ] Every env-var reference is quoted (shell) or strict-mode-checked (PS).
- [ ] No `eval`, `Invoke-Expression`, `iex`, `sh -c`, `bash -c`, `zsh -c`, `dash -c`, `cmd /c`, `pwsh -c`, `pwsh -Command`, `powershell -Command`. (Run the extended pre-emit grep from step 7.)
- [ ] No nested-shell escalations: `xargs sh`, `find -exec sh -c`, `ssh host "$cmd"`.
- [ ] No `Start-Process` / `subprocess` whose target binary is itself a shell (`cmd.exe`, `bash`, `sqlcmd`).
- [ ] Every downstream binary's flag values reviewed for wildcard / pattern / glob acceptance (cmdlet `-Name *`, `journalctl -u <pat>`, `find -name <glob>`).
- [ ] In-script identity allowlist present for any verb-style Action (services, processes, files, hosts).
- [ ] Path-from-input handlers canonicalize and verify containment (`realpath` / `[IO.Path]::GetFullPath` + `StartsWith` check).
- [ ] **Linter actually run on the file and clean.** Not "linter command included" — linter *executed* and the output was empty / clean.
- [ ] Webhook only: HMAC/secret check + body-size cap before parse + OTP-verified recheck + `|json`/`|url`/`|html` filter on every templated user value.
- [ ] OTP credential, allowlist, and arg schema match the interview answers.
- [ ] Credential scope walked through with the operator; if scoping is incomplete, an `# AUDIT THIS:` block names what to do before deployment.
- [ ] If the operator argued for a relaxation based on *who* the senders are (rather than *cost*), the relaxation was refused.
- [ ] Comments explain *why* for every non-obvious line.

## Cross-references

- Wiki page (architecture, hot path, failure modes):
  [`docs/wiki/actions.md`](../../docs/wiki/actions.md)
- Operator handbook (long-form prose, anti-pattern tables):
  [`docs/handbook/actions-handler-safety.html`](../../docs/handbook/actions-handler-safety.html),
  with sub-pages for shell, PowerShell, and webhooks.
- Shipped example handlers (every one of these is shellcheck /
  PSScriptAnalyzer / ruff clean and exercised by CI):
  [`examples/actions/`](../../examples/actions/).
