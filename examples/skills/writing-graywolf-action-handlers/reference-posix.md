# Reference: POSIX shell (bash) handlers

Authoring guide for `bash` handlers that run on Linux + macOS. Every
shipped handler in `examples/actions/posix/` is shellcheck-clean and
exercised by CI; mirror their style.

> **Read [`SKILL.md`](SKILL.md) first.** This file assumes the
> interview is already complete. The threat model, the credential
> blast-radius discussion, and the universal invariants are stated
> there and are not repeated here.

## What graywolf invokes

The runner exec's the binary directly via `execve(2)` — no shell, no
`PATH` re-search, no `system()`. argv is exactly:

```
/abs/path/to/handler.sh <action> <sender> <true|false> [<arg> ...]
```

Environment carries:

- Always: `GW_ACTION_NAME`, `GW_SENDER_CALL`, `GW_OTP_VERIFIED`,
  `GW_OTP_CRED_NAME`, `GW_SOURCE` (`rf` or `is`), `GW_INVOCATION_ID`.
- kv: `GW_ARG_<KEY>=<value>` per declared arg, key uppercased,
  non-alphanumeric `→` `_`.
- freeform: `GW_ARG=<raw payload>`, single variable, no key suffix.

**Use the env vars, not argv.** They survive arg-schema reordering
and are easier to audit.

## Mandatory prelude

Every handler starts with these lines, in this order:

```bash
#!/usr/bin/env bash
# <Action name> -- <one-sentence purpose>
# Wire format: @@<otp>#<name> <kv|freeform shape>
# Args: <list>
# Reply: <first-line success snippet>
# Config: <env vars and required scope>
# Deps: <external commands invoked>
set -euo pipefail
```

- `#!/usr/bin/env bash` not `#!/bin/sh` — these patterns rely on
  bash's `[[ =~ ]]` and `BASH_REMATCH`.
- The header comment is operator-facing documentation. Include the
  credential scope (e.g. `IAM: dedicated user, single sns:Publish
  statement on arn:aws:sns:us-east-1:123456789012:graywolf-alerts`).
- `set -euo pipefail`:
  - `-e` aborts on any non-zero exit.
  - `-u` treats unset variables as errors — typos in `$GW_ARG_*` fail
    loudly instead of silently expanding to empty.
  - `-o pipefail` makes a pipeline fail if any stage fails, not just
    the last.

## Reading inputs

### kv mode

```bash
to="${GW_ARG_TO:?missing to=}"
msg="${GW_ARG_MSG:?missing msg=}"
sender="${GW_SENDER_CALL:-?}"
```

`${VAR:?msg}` aborts with a clear stderr line if `VAR` is unset or
empty. Use it on every required arg. `${VAR:-default}` is the
fallback for optional args.

Quote every reference. Always. `"$to"` not `$to`. shellcheck flags
this; do not disable.

### freeform mode

```bash
ACTION="$1"
SENDER="$2"
OTP_VERIFIED="$3"
PAYLOAD="${GW_ARG:-${4:-}}"

if [[ -z "$PAYLOAD" ]]; then
    echo "payload missing" >&2
    exit 64
fi

# One regex match validates AND splits in one step.
# BASH_REMATCH[1..N] hold the capture groups on success.
if [[ ! "$PAYLOAD" =~ ^(\+[1-9][0-9]{6,14})[[:space:]]+(.+)$ ]]; then
    echo "expected '+<E164> <message>'" >&2
    exit 64
fi
NUMBER="${BASH_REMATCH[1]}"
MESSAGE="${BASH_REMATCH[2]}"
```

Why `[[ =~ ]]`:

- It is a pure in-process operation. No fork, no exec, no command
  substitution. Bytes in `$PAYLOAD` cannot become shell.
- It validates *and* splits in a single step. There is no partial
  state between "did the format check pass" and "did we extract the
  fields".
- `BASH_REMATCH` indices follow the parenthesis order in the regex,
  exactly like other regex engines.

## Revalidate inside the handler

Graywolf's sanitizer applies the Action's `arg_schema` regex and
strips control characters in freeform mode, but the handler's safety
must not depend on that. Re-check:

```bash
# Reject control characters (POSIX [:cntrl:] is 0x00-0x1F + 0x7F).
if [[ "$MESSAGE" =~ [[:cntrl:]] ]]; then
    echo "message contains control characters" >&2
    exit 65
fi

# Length range. Tighten to the downstream API's actual limit.
if (( ${#MESSAGE} < 1 || ${#MESSAGE} > 160 )); then
    echo "message length out of range (1..160)" >&2
    exit 65
fi
```

For numeric args, validate range with `[[ =~ ^[0-9]+$ ]]` *and* a
numeric comparison — the regex prevents word-splitting attacks, the
range check prevents semantic abuse.

## Verifying required env

```bash
: "${TWILIO_ACCOUNT_SID:?TWILIO_ACCOUNT_SID not set}"
: "${TWILIO_AUTH_TOKEN:?TWILIO_AUTH_TOKEN not set}"
: "${TWILIO_FROM:?TWILIO_FROM not set}"
```

`:` is the no-op builtin. Combined with `${VAR:?msg}` this is a
one-line "abort with clear error if the operator forgot to set this".
Run these checks immediately after argument parsing — fail fast
before the script touches the network.

## Calling external commands

### Always argv, never strings

`curl` (or any binary) accepts user data via flag-pinned arguments,
not as part of a command string:

```bash
# GOOD — each value is its own argv entry, curl URL-encodes for us.
curl -fsS --max-time 8 -X POST \
    --data-urlencode "From=${TWILIO_FROM}" \
    --data-urlencode "To=${NUMBER}" \
    --data-urlencode "Body=${MESSAGE}" \
    -u "${TWILIO_ACCOUNT_SID}:${TWILIO_AUTH_TOKEN}" \
    -- "https://api.twilio.com/2010-04-01/Accounts/${TWILIO_ACCOUNT_SID}/Messages.json"

# BAD — string concatenation. '&' in $MESSAGE smuggles extra fields.
curl ... -d "Body=${MESSAGE}&Hidden=evil"   # never do this

# BAD — eval. Full RCE.
eval "twilio send $NUMBER \"$MESSAGE\""     # never do this
```

`--data-urlencode` makes curl encode each value into the
`application/x-www-form-urlencoded` body. The `--` before the URL is
habit: it terminates option parsing so a value that starts with `-`
cannot be misinterpreted.

### `--` on every command that accepts it

Always pass `--` before any positional argument that could start with
a `-`. This is one character of typing that prevents an entire class
of attack.

```bash
# Without -- a payload starting with '-' becomes a flag.
do-thing -- "$NUMBER" "$MESSAGE"
```

### `jq` for JSON, not regex

When parsing API responses, prefer `jq` over `grep`/`sed`. `jq`
handles escapes correctly and never executes anything:

```bash
sid=$(printf '%s' "$response" | jq -r '.sid // empty')
```

If `jq` is not available, the very narrow `grep -o` pattern is
acceptable but should be a last resort.

## Reply contract

Graywolf snippets the **first line of stdout** for the on-air reply
(50-rune cap). Diagnostics go to stderr. Exit non-zero on failure
with a short stderr line (also snippeted).

```bash
echo "sent ${sid}"          # success: first line of stdout
exit 0
# ----
echo "twilio rejected: ..." >&2   # failure: stderr
exit 1
```

Standard exit codes (loosely follow `sysexits.h`):

| Exit | Meaning |
|---|---|
| 0 | success |
| 1 | generic failure |
| 64 | usage / payload format error |
| 65 | data error (revalidation rejected) |
| 69 | service unavailable |
| 75 | temporary failure (retry hint) |
| 77 | permission denied (OTP-verified defense-in-depth check failed) |

## Anti-pattern table

| Anti-pattern | Fix |
|---|---|
| `eval "$cmd"` | Validate args, then call the binary directly. |
| `sh -c "$cmd"` / `bash -c "$cmd"` / `zsh -c "$cmd"` / `dash -c "$cmd"` | Same as eval. The shell binary doesn't matter — *any* `-c` form re-parses the payload as shell. |
| `xargs sh -c '...' _ "$arg"` / `find ... -exec sh -c '...' _ "$arg" \;` | Nested shell on the far side of an argv-clean call. Refactor to call the target binary directly: `find ... -exec target -- {} +`. |
| `ssh host "do-thing $arg"` | The remote shell parses the string. Use `ssh host -- do-thing -- "$arg"` only if the remote `do-thing` is itself argv-safe; otherwise restructure. |
| `journalctl -u "$service"` with passthrough `$service` | `-u` accepts globs and unit-instance syntax. Allowlist exact unit names: `case "$service" in nginx\|postgresql) ;; *) exit 65 ;; esac`. |
| `cat "/var/log/$file"` | Path traversal + arbitrary file read. Allowlist filenames; canonicalize with `realpath` and confirm prefix. |
| Unquoted `$GW_ARG_FOO` | Quote always. shellcheck SC2086 catches this. |
| `curl ... -d "Body=${MESSAGE}&extra=evil"` | `--data-urlencode "Body=${MESSAGE}"`. |
| `cmd $NUMBER $MESSAGE` (no `--`) | `cmd -- "$NUMBER" "$MESSAGE"`. |
| Inline API key | Read from env (`${API_KEY:?...}`). |
| `# shellcheck disable=...` | Fix the underlying issue. The diagnostic is the bug. |
| Reading from stdin | The runner does not pipe stdin. Read env / argv only. |
| `do-thing "$@"` passthrough | Argv passthrough still re-routes attacker control to the target. Validate every value first. |

## Patterns the skill explicitly blesses

### Switch-case verb dispatcher (when one Action covers multiple verbs)

When the operator wants `@@<otp>#run <verb>` to map to a small,
fixed set of commands, a switch-case is the safe shape — but only
with these guardrails. Skip any one and the dispatcher becomes a
remote shell:

```bash
verb="${GW_ARG_VERB:?missing verb=}"

# 1. Anchor the regex on the wire AND inside the script.
if [[ ! "$verb" =~ ^(restart-nginx|rotate-logs|backup)$ ]]; then
    echo "unknown verb: $verb" >&2
    exit 65
fi

# 2. Each branch calls a fixed argv. No variable holds a command.
# 3. The default branch (*) MUST reject -- never fall through to a
#    generic exec.
case "$verb" in
    restart-nginx)
        systemctl restart -- nginx
        ;;
    rotate-logs)
        /usr/local/bin/rotate-logs.sh -- "$(date +%F)"
        ;;
    backup)
        /usr/local/bin/backup.sh
        ;;
    *)
        # Defense in depth. If the regex above ever widens, this still rejects.
        echo "unknown verb (defense in depth): $verb" >&2
        exit 65
        ;;
esac
```

**Hard rules:**

- The wire regex is anchored AND duplicated in-script.
- No branch contains `eval`, `sh -c`, `bash -c`, `& $name`, or any
  variable that holds a command string.
- The `*)` default rejects with non-zero exit and a stderr line.
  Never fall through to a generic `exec "$@"` or
  `systemctl restart "$verb"`.

### Path-containment helper

When the script must build a filesystem path from user input, use
`realpath` to canonicalize and verify containment:

```bash
base=/usr/local/lib/graywolf/files
name="${GW_ARG_NAME:?missing name=}"

# Reject path metacharacters early.
if [[ ! "$name" =~ ^[a-z0-9_.-]+$ ]]; then
    echo "invalid name" >&2
    exit 65
fi

# Canonicalize and confirm the result stays under $base.
candidate=$(realpath -m -- "$base/$name")
case "$candidate" in
    "$base"/*) ;;
    *) echo "path escape" >&2; exit 65 ;;
esac

cat -- "$candidate"
```

The leading `^[a-z0-9_.-]+$` blocks `..`-style paths even before the
canonicalization runs; the `realpath` + `case` check is the
load-bearing defense — file names like `foo..bar` pass the regex but
correctly resolve under `$base`.

## Linter — mandatory

```bash
shellcheck -s bash -S style /path/to/handler.sh
```

Diagnostics are bugs. Fix them. Never disable. The skill must run
this command on the generated file and confirm clean output before
declaring the script ready.

Common diagnostics worth knowing:

- **SC2086** Double quote to prevent globbing and word splitting.
- **SC2046** Quote this to prevent word splitting.
- **SC2155** Declare and assign separately to avoid masking return values.
- **SC2059** Don't use variables in the printf format string.
- **SC1090** Source includes a variable; consider quoting.

## Skeleton — copy and adapt

### kv-mode skeleton

```bash
#!/usr/bin/env bash
# <NAME> -- <one-sentence purpose>
# Wire: @@<otp>#<name> key1=<shape> key2=<shape>
# Args: key1 (required, ^...$), key2 (optional, ^...$)
# Reply: success: "ok <result-id> for <sender>"; failure: "<error snippet>"
# Config:
#   MY_API_KEY      required. Scope: <e.g. "tied to a single project, no admin role">
# Deps:   curl, jq
set -euo pipefail

# 0. OTP defense in depth. The Action editor's OTPRequired toggle is
#    the first defense; this check is the second. Skip ONLY for
#    genuinely read-only Actions.
[[ "${GW_OTP_VERIFIED:-false}" == "true" ]] || { echo "otp not verified" >&2; exit 77; }

SENDER="${GW_SENDER_CALL:-?}"

# 1. Read args via env (preferred over $4..).
key1="${GW_ARG_KEY1:?missing key1=}"
key2="${GW_ARG_KEY2:-default}"

# 2. Revalidate (defense in depth).
if [[ ! "$key1" =~ ^...$ ]]; then
    echo "invalid key1: $key1" >&2
    exit 65
fi

# 3. Verify env.
: "${MY_API_KEY:?MY_API_KEY not set}"

# 4. Call downstream with argv-style args.
response=$(curl -fsS --max-time 8 -X POST \
    --data-urlencode "field1=${key1}" \
    --data-urlencode "field2=${key2}" \
    -H "Authorization: Bearer ${MY_API_KEY}" \
    -- "https://api.example.com/v1/do-thing")

# 5. Parse response, emit first-line reply (≤50 runes).
result=$(printf '%s' "$response" | jq -r '.id // empty')
if [[ -n "$result" ]]; then
    echo "ok ${result:0:8} for ${SENDER}"
    exit 0
fi

echo "downstream rejected: $(printf '%s' "$response" | head -c 80)" >&2
exit 1
```

### freeform-mode skeleton

```bash
#!/usr/bin/env bash
# <NAME> -- <one-sentence purpose>
# Wire: @@<otp>#<name> <freeform shape, e.g. "+<E164> <message>">
# Reply: success: "sent <id> to <sender>"; failure: "<error snippet>"
# Config:
#   SECRET          required. Scope: <e.g. "single sns:Publish statement, ARN-pinned">
# Deps:   curl
set -euo pipefail

# OTP defense in depth.
[[ "${GW_OTP_VERIFIED:-false}" == "true" ]] || { echo "otp not verified" >&2; exit 77; }

ACTION="$1"
SENDER="$2"
OTP_VERIFIED="$3"
PAYLOAD="${GW_ARG:-${4:-}}"

if [[ -z "$PAYLOAD" ]]; then
    echo "payload missing" >&2
    exit 64
fi

# Single regex match validates AND splits.
if [[ ! "$PAYLOAD" =~ ^(<group1>)[[:space:]]+(<group2>)$ ]]; then
    echo "expected '<format>'" >&2
    exit 64
fi
field1="${BASH_REMATCH[1]}"
field2="${BASH_REMATCH[2]}"

# Revalidate (control chars, length).
if [[ "$field2" =~ [[:cntrl:]] ]]; then
    echo "field2 contains control characters" >&2
    exit 65
fi
if (( ${#field2} < 1 || ${#field2} > <max> )); then
    echo "field2 length out of range" >&2
    exit 65
fi

# Verify env.
: "${SECRET:?SECRET not set}"

# Call downstream argv-style.
response=$(curl -fsS --max-time 8 -X POST \
    --data-urlencode "f1=${field1}" \
    --data-urlencode "f2=${field2}" \
    -H "Authorization: Bearer ${SECRET}" \
    -- "https://api.example.com/v1/do") || {
    echo "downstream rejected" >&2
    exit 69
}

# First line of stdout is the on-air reply (≤50 runes).
id=$(printf '%s' "$response" | jq -r '.id // empty')
echo "sent ${id:-ok} to ${SENDER}"
```

## Operator postamble template

Always print this after the script:

```
Install path:    /usr/local/lib/graywolf/actions/<name>.sh
Make executable: chmod +x /usr/local/lib/graywolf/actions/<name>.sh

Lint (mandatory): shellcheck -s bash -S style /usr/local/lib/graywolf/actions/<name>.sh

Env vars (add to /etc/systemd/system/graywolf.service.d/override.conf
or /etc/default/graywolf):
  Environment=MY_API_KEY=...
  Environment=...
Then: systemctl daemon-reload && systemctl restart graywolf

Wire it up: /#/actions → New
  Name: <name>
  Type: command
  Command path: /usr/local/lib/graywolf/actions/<name>.sh
  Arg schema: <one row per declared arg>
  OTP credential: <chosen credential | none>
  Sender allowlist: <callsigns | empty>
  Timeout: <seconds>

Test: per-row Test dialog before letting it loose on-air.
```
