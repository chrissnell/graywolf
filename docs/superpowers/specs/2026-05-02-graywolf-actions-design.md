# Graywolf Actions — design

Status: design (approved by user 2026-05-02; not yet implemented)
Issue: https://github.com/chrissnell/graywolf/issues/54
UI mockup: [`2026-05-02-graywolf-actions-mockup.png`](./2026-05-02-graywolf-actions-mockup.png)

## 1. Motivation

Operators want to trigger predefined commands or webhooks on the Graywolf
host by sending an APRS message. Use cases include turning on lights at an
off-grid cabin, cycling power on a remote repeater controller, or
publishing simple read-only data (e.g. a Weather bot). Issue #54 captures
the user-facing intent.

The feature must be safe to expose on RF and APRS-IS — both are open,
unauthenticated transports. The design treats every inbound trigger as
hostile by default and constrains every input source.

## 2. Concepts

- **Action** — a named operator-defined unit of work. Has a type (command
  or webhook), an executable target, an arg schema, an OTP requirement,
  a sender allowlist, rate-limit and queue settings.
- **OTP credential** — a TOTP secret stored on the server. Each
  credential has a name (operator label), an issuer + account string
  (used in the `otpauth://` URI shown to the operator on creation), and
  an algorithm/digits/period (TOTP/SHA1/6/30s in v1).
- **Invocation** — a single attempt to run an Action, recorded in the
  audit log regardless of outcome.

## 3. Trigger surface

The Actions subsystem subscribes to inbound APRS messages addressed to
**any of**:

- Graywolf's primary station call+SSID (e.g. `NW5W-4`).
- Any tactical alias registered for the station
  (`pkg/messages/tactical_set.go`).
- Any addressee in a new "Action listener addressees" list configured in
  Actions settings (e.g. `GWACT`, `CABIN`). Lets one Graywolf instance
  listen on a dedicated bot-style call independent of the station call.

Both RF and APRS-IS sources are accepted. There is **no per-Action
source toggle** — if the inbound matches the trigger surface, it is
classified by the Actions subsystem regardless of channel.

The text body must begin with the literal prefix **`@@`** to be
considered an Action invocation. Anything else falls through to the
existing messages router unchanged.

## 4. Message grammar

```
@@<otp>#<action> [k1=v1 k2=v2 ...]
```

- `<otp>` — exactly 6 ASCII digits when the matched Action requires OTP.
  When the matched Action does not require OTP, this field may be empty
  (i.e. the body is `@@#<action> [args]`). Implementation note: parse
  `@@`-then-optional-digits-then-`#`. If digits present, treat as OTP
  candidate; if absent, treat as no-OTP attempt.
- If the matched Action has `otp_required=false` but the message
  includes OTP digits anyway, the digits are **ignored** (not
  validated, not rejected). This lets operators use a single
  muscle-memory format for all Actions.
- `#` — literal separator.
- `<action>` — the Action name (must match an existing Action,
  case-sensitive).
- `[k1=v1 ...]` — zero or more space-separated `key=value` pairs. Order
  is not significant.

### Examples

- `@@482910#TurnOnGarageLights room=garage state=on`
- `@@#Weather sta=KSFO`  (no OTP)
- `@@123456#PingPager msg=hello`

### Sanitization defaults

- Default global allowed-arg regex: `^[A-Za-z0-9,_-]{1,32}$`
  (letters, digits, comma, underscore, hyphen; max 32 chars per value).
- Per-Action per-key override may tighten the regex (e.g. `state` →
  `^(on|off)$`) or loosen it within reasonable bounds.
- Keys not declared in the Action's `allowed args` list are rejected
  (reply: `bad arg: <key> not allowed`).
- Required keys not present in the message are rejected (reply: `bad
  arg: <key> missing`).
- Whole-message size cap: 67 chars (APRS message text limit). Anything
  beyond is the AX.25 layer's problem; Actions does not need to enforce.

## 5. OTP scheme

- Algorithm: **TOTP / SHA1 / 6 digits / 30 second step** (matches
  Google Authenticator, Authy, 1Password, all common authenticator
  apps). Implemented via `github.com/pquerna/otp/totp`.
- Window: ±1 step (so a code is valid for the previous, current, or
  next 30s window). Tolerates ~30s clock skew on the sender side.
- **Replay protection (required):** for each successful OTP
  verification, store `(credential_id, time_step, code_hash)` in a
  small in-memory ring with TTL = 2 × step + grace = ~120 seconds.
  Re-presentation of the same `(cred_id, step, code)` tuple within the
  TTL is rejected with `bad otp` (reply: "code already used").
- One credential per Action. The chosen credential is stored as a
  foreign-key reference (`ON DELETE SET NULL`); when the credential
  is deleted, an Action with `otp_required=true` and a now-null
  `otp_credential_id` becomes un-runnable. The runner refuses such
  invocations with a new status `no-credential` (operator UI surfaces
  the broken state on the Actions row).

### OTP credential storage

- Stored in the existing `graywolf.db` (SQLite, GORM) as plaintext.
  Same protection model as the existing APRS-IS passcode and other
  graywolf creds. The UI surfaces a one-line warning that
  `graywolf.db` should be treated as sensitive.
- Reliability over crypto theater: no passphrase prompt at startup, no
  separate keyfile. The service must always come back up from a
  reboot without operator intervention. (User-affirmed decision.)

### Onboarding flow (creating a credential)

1. Operator clicks **+ New Credential** on the Actions tab.
2. Modal collects: Name (operator label), Issuer (default `Graywolf`),
   Account (default `<station-call>:<name>`).
3. On Save: server generates a fresh 160-bit secret, persists the row,
   and returns the secret + base32 string + `otpauth://` URI **in the
   create response only**.
4. The modal then displays a QR code (rendered client-side from the
   `otpauth://` URI), the base32 secret in a copyable input, and the
   `otpauth://` URI in a second copyable input. A warning banner
   reminds the operator the secret will not be shown again.
5. After the operator clicks **I've saved it — close**, the modal
   closes. Subsequent reads of the credential never return the secret.
6. There is **no "show again"** path. To recover, the operator deletes
   the credential and creates a new one (Actions referencing the old
   one will need to be reassigned).

## 6. Action configuration

Per-Action fields (full list, all editable in the Edit Action modal):

| Field | Type | Notes |
|---|---|---|
| `name` | string, unique | Used as the message keyword. Letters, digits, dot, dash, underscore. Case-sensitive. |
| `description` | string | Operator-facing free text. Displayed under the name in the Actions list. |
| `type` | enum: `command` \| `webhook` | Switches the rest of the form. |
| `command_path` | string (when `type=command`) | Absolute path. Must exist and be executable by the `graywolf` user at save time (validated). |
| `working_dir` | string \| empty | Empty = directory containing the script. |
| `webhook_method` | enum: `GET` \| `POST` (when `type=webhook`) | |
| `webhook_url` | string | Token-substitution allowed. |
| `webhook_headers` | map<string,string> | Operator-defined extra headers (e.g. `Authorization`). Token-substitution allowed in values. |
| `webhook_body_template` | string | When `POST`, optional override; default = `application/x-www-form-urlencoded` form encoding of the args + standard fields. |
| `timeout_sec` | int (default 10, range 1–60) | Hard kill at expiry. |
| `otp_required` | bool (default true) | When false, the OTP field in the message may be empty. |
| `otp_credential_id` | FK (nullable) | Required when `otp_required=true`. Single credential. |
| `sender_allowlist` | string (CSV of patterns) | Comma-separated callsigns or `CALL-*` SSID wildcards. Empty = anyone (OTP still applies). |
| `arg_schema` | list of `{key, regex_override?, max_len?, required}` | Default regex `^[A-Za-z0-9,_-]{1,32}$` + max-len 32 if not overridden. Empty list = no args accepted. |
| `rate_limit_sec` | int (default 5) | Minimum interval between successive invocations of this Action. |
| `queue_depth` | int (default 8, range 0–32) | Per-Action FIFO; overflow returns `busy`. `0` disables queueing (parallel invocations allowed for read-only Actions). |
| `enabled` | bool (default true) | Disabled Actions never fire. |

## 7. Sanitization & safe execution

### 7.1 Sanitization

- Each inbound `key=value` pair is validated against the Action's
  `arg_schema`. Failure rejects the whole invocation with
  `bad arg: <key>` and is logged.
- Sanitization happens **once**, before execution dispatch. Sanitized
  values are the only thing handed to the executor.
- Operator-facing UI explains the default regex and shows the per-key
  override editor, with a clear note that args failing the regex are
  rejected (so operators can tune the regex if their script needs
  different chars).

### 7.2 Command execution

- Always exec via argv (`os/exec.Command(path, args...)`). Never via
  `sh -c`. The executor never templates user-supplied data into a
  shell string.
- Argv layout: `[command_path, action_name, sender_callsign,
  otp_verified_bool, "k1=v1", "k2=v2", ...]`. Args are passed in the
  order declared in the `arg_schema` for stability.
- Env vars set on the child process:
  - `GW_ACTION_NAME` — Action name
  - `GW_SENDER_CALL` — full sender callsign (e.g. `NW5W-7`)
  - `GW_OTP_VERIFIED` — `true` / `false`
  - `GW_OTP_CRED_NAME` — credential name (empty if not used)
  - `GW_SOURCE` — `rf` or `is`
  - `GW_INVOCATION_ID` — the audit-log row ID (so scripts can correlate)
  - `GW_ARG_<KEY>` for each arg, where `<KEY>` is uppercased and
    non-`[A-Z0-9_]` chars are replaced with `_`.
- Timeout enforced via `context.WithTimeout` + `cmd.Cancel`. SIGTERM
  on expiry, SIGKILL 2 seconds later if the process is still alive.
- Working directory: the configured `working_dir`, or the directory
  containing `command_path` if blank.
- UID: the `graywolf` service user. **No setuid, no privilege
  escalation in v1.** If an operator needs root, they can configure a
  dedicated `sudo` rule out-of-band; we will not provide a privilege
  knob in the UI.

### 7.3 Webhook execution

- Token expansion in `webhook_url`, header values, and body template:
  - `{{action}}` — Action name
  - `{{sender-callsign}}` — sender callsign
  - `{{otp-verified}}` — `true` / `false`
  - `{{otp-cred}}` — credential name or empty
  - `{{source}}` — `rf` or `is`
  - `{{arg.<key>}}` — sanitized arg value (empty if not provided)
- All token values are URL-encoded when expanded into a URL or query
  string; form-encoded when expanded into an `application/x-www-form-urlencoded`
  body; passed as-is into header values (already passed sanitization).
- Default body for `POST` (no template): `application/x-www-form-urlencoded`
  with `action`, `sender_callsign`, `otp_verified`, `otp_cred`,
  `source` plus one form field per arg.
- HTTP timeout: `timeout_sec` (default 10). Connect + total bound to
  the same value (use `http.Client` with `Timeout`).
- TLS verification on by default. No setting to disable (yet).
- Body capture: read up to 1 KiB of response body for the audit-log
  reply detail. The reply that goes back to the sender is trimmed
  further (see §8).
- Treat HTTP 2xx as success; 3xx (followed) likewise; everything else
  as failure with the status code in the reply.

## 8. Reply policy

Every invocation produces exactly one reply to the sender. Replies use
the existing `pkg/messages/sender.go` outbound path so:

- They get a real APRS msg-id, are subject to the standard retry/ACK
  semantics, and appear in the operator's Messages outbound view (as
  any outbound DM does).
- They mirror the auto-ACK path: RF inbound → reply on the same RF
  channel; APRS-IS inbound → reply over APRS-IS only.

### Reply content

Status word always present, drawn from this set:

| Status | When |
|---|---|
| `ok` | Command exit 0 / HTTP 2xx |
| `bad otp` | OTP required and failed validation (no match, expired, replay) |
| `bad arg: <key>` | One key failed sanitization (first key reported) |
| `denied` | Sender not in `sender_allowlist` |
| `disabled` | Action exists but `enabled=false` |
| `unknown` | No Action with that name |
| `no-credential` | Action requires OTP but its assigned credential was deleted |
| `busy` | Per-Action queue overflow |
| `rate-limited` | Within `rate_limit_sec` of last invocation |
| `timeout` | Command/webhook exceeded `timeout_sec` |
| `error` | Command exit non-zero, HTTP non-2xx, network error |

On success (`ok`), the reply includes the first ~50 chars of stdout
(commands) or response body (webhooks) after a colon — e.g.
`ok: lights on (2 bulbs, 38W)`. The hard message size cap is the
APRS limit (~67 chars). The full response is preserved in the audit
log; truncation in the wire reply is flagged with a trailing `…`.

### Inbound classification ordering

The Action-prefix classifier runs **before** the messages router on
the inbound RX fanout. When the inbound matches the trigger surface
**and** begins with `@@`:

- The message is **not** stored as a Message row in the existing
  Messages inbox.
- An auto-ACK is still sent if the inbound carried a msg-id (so the
  sender's APRS client stops retrying).
- The Action subsystem handles the rest.

When the inbound does not match `@@` (or addressee doesn't match), it
falls through to the existing messages router unchanged.

## 9. Architecture

New package: **`pkg/actions/`** (alongside `pkg/messages`,
`pkg/digipeater`, `pkg/igate`, etc.). Components:

| File | Role |
|---|---|
| `classifier.go` | Pre-classifier on the RX fanout. Owns the trigger-surface check + `@@` prefix detect. Diverts matching packets to the runner. |
| `parser.go` | Parses `@@<otp>#<action> [k=v ...]`. Returns a typed `ParsedInvocation` or a typed parse error (used to build the reply). |
| `otp.go` | TOTP verify wrapper around `pquerna/otp/totp`. Owns the replay-cache (in-memory ring keyed by `(cred_id, step, code_hash)`). |
| `runner.go` | Per-Action FIFO queue + worker goroutine. Calls into `cmd_executor.go` or `webhook_executor.go`, captures the result, dispatches the reply. |
| `cmd_executor.go` | argv-only command exec with timeout, env injection, stdout capture (capped at 4 KiB). |
| `webhook_executor.go` | HTTP client, token expansion, response capture (capped at 1 KiB). |
| `audit.go` | Writes invocation rows to the new `action_invocations` table. |
| `service.go` | Composition root: wires classifier, runner pool, OTP service, audit, reply sender. Exposes the public API for `pkg/app/wiring.go`. |
| `service_test.go`, `*_test.go` | Per-component table tests + an end-to-end test that drives a fake inbound through to a fake reply sink. |

### Reply path

The runner calls into a narrow `ReplySender` interface that
`pkg/messages/sender.go` satisfies. Replies flow through the same
outbound code path as a normal operator-typed message (msg-id, retry,
RF/IS routing). This avoids duplicating outbound infrastructure and
ensures the reply gets a row in the operator's outbound view.

### RX fanout integration

`pkg/app/rxfanout.go` currently dispatches inbound APRS frames to the
digipeater, KISS broadcast, and APRS-submit paths. The Actions
classifier inserts itself ahead of the existing messages router fan-out
arm: it tests if the message is a candidate; if so it diverts (consuming
the packet from the messages-router perspective so no inbox row is
created); if not it lets the packet flow on unchanged.

The classifier and the messages router share the addressee-resolver
helper from `pkg/messages` (tactical aliases) so the two surfaces never
disagree on what counts as "addressed to us".

## 10. Storage schema

New configstore tables (added by a new GORM migration in
`pkg/configstore/migrate_actions.go`).

### `actions`

```
id                  uint    pk
name                string  unique, max 32
description         string
type                string  enum: 'command' | 'webhook'
command_path        string  nullable
working_dir         string
webhook_method      string  nullable, enum: 'GET' | 'POST'
webhook_url         string  nullable
webhook_headers     json    map<string,string>, default '{}'
webhook_body_template text  nullable
timeout_sec         int     default 10
otp_required        bool    default true
otp_credential_id   uint    nullable, FK -> otp_credentials.id (ON DELETE SET NULL)
sender_allowlist    string  CSV
arg_schema          json    list of {key, regex, max_len, required}
rate_limit_sec      int     default 5
queue_depth         int     default 8
enabled             bool    default true
created_at, updated_at
```

### `otp_credentials`

```
id              uint   pk
name            string unique, max 64
issuer          string
account         string
algorithm       string default 'SHA1'
digits          int    default 6
period          int    default 30
secret_b32      string (the only place the secret is persisted)
created_at, last_used_at
```

### `action_listener_addressees`

```
id        uint   pk
addressee string unique, max 9 (APRS addressee field width)
```

Ships empty; operators add entries to extend the trigger surface.

### `action_invocations` (audit log)

```
id              uint     pk
action_id       uint     FK -> actions.id (nullable when 'unknown' status)
action_name_at  string   denormalized so deleted-Action invocations still read sensibly
sender_call     string
source          string   'rf' | 'is'
otp_credential_id uint   nullable
otp_verified    bool
raw_args_json   json     map<string,string> as parsed off the wire (pre-sanitization), each value truncated to 64 chars to bound row size
status          string   ok | bad_otp | bad_arg | denied | disabled | unknown | no_credential | busy | rate_limited | timeout | error
status_detail   string   short reason
exit_code       int      nullable (commands)
http_status     int      nullable (webhooks)
output_capture  string   first ~1 KiB of stdout / response body
reply_text      string   what we sent back over APRS
truncated       bool
created_at
```

Retention policy: keep the last `N` rows (default 1000) **or** rows
younger than 30 days, whichever bound is hit first. Pruner runs daily.

### Indexes

- `actions.name` UNIQUE
- `otp_credentials.name` UNIQUE
- `action_invocations.action_id` (for filter-by-action queries)
- `action_invocations.sender_call` (for filter-by-callsign queries)
- `action_invocations.created_at` (for time-window queries + pruning)

## 11. Web UI

New top-level main route **`/actions`** (alongside Dashboard, Live Map,
Messages, Terminal). Sidebar entry promoted to the main-items group, not
buried in Settings — Actions are interactive operations, not
configuration.

The approved mockup is in [`2026-05-02-graywolf-actions-mockup.png`](./2026-05-02-graywolf-actions-mockup.png).
Key UI elements:

1. **Page header** — title + subtitle + secondary "View audit log"
   button + primary "+ New Action" button.
2. **Help banner** — one-line explainer with the message-grammar
   example for the operator.
3. **Actions table** — columns: Name / Description (heavy name above
   normal-weight description), Type (command/webhook badge), OTP
   (required / not required pill), Sender allowlist (callsign chips),
   Last fired (relative + sender). Row actions: Edit, Test, Delete.
4. **OTP Credentials table** — columns: Name, Issuer / account,
   Algorithm, Created, Last used, Used by (count + Action names).
   Row action: Delete.
5. **Recent Invocations** — filter bar (text search + action +
   result-status + source dropdowns). Columns: Time, Sender, Src,
   Action (with arg summary), Cred, Result (colored status pill),
   Reply / detail.
6. **Edit Action modal** — aligned form with Name, Description, Type
   radio, Command path (or webhook URL/method/headers/body), Working
   directory, Timeout, OTP required toggle, OTP credential select,
   Sender allowlist, Allowed args table (per-key regex override +
   required toggle), Rate limit, Queue depth, Reply policy summary.
7. **New OTP Credential modal** — Name / Issuer / Account / Algorithm
   inputs, then a one-time-display panel with a QR code, the base32
   secret in a copyable input, the `otpauth://` URI in a second
   copyable input, and a "shown only once" warning banner.

### Test button

The Actions table's per-row **Test** button opens a small dialog where
the operator can fire the Action with arbitrary `key=value` args
without needing to send an APRS message. The test invocation
short-circuits the OTP and sender-allowlist checks (operator is already
authenticated to the web UI), but exercises the same execution path,
sanitization, timeout, and audit-log row write — making it the primary
way to debug new Actions.

### Component dependencies (chonky-ui)

The mockup uses graywolf's existing chonky-ui primitives plus one
graywolf-side override: solid-filled button variants for primary /
success / danger, mirroring how badges are already overridden in
`web/themes/graywolf.css`. This belongs to the chonky-ui change in
`~/dev/chonky` rather than a graywolf-local fork (per
`feedback_chonky_ui_workflow`). New UI components needed:

- A QR code display (renders an `otpauth://` URI). Either add to
  chonky-ui or include a small client-side library — decision deferred
  to the implementation plan.
- A copyable readonly input with a "Copy" button.
- A multi-select / checkbox list — already covered by existing
  `Checkbox`/`Listbox` primitives.

## 12. Security considerations

- **All inbound is hostile.** Every value reaching the executor has
  passed (a) the global allowed-arg regex or per-key override, and
  (b) the size cap.
- **No shell.** Commands are exec'd via argv. Webhook templates have
  values URL-encoded / form-encoded on insertion. There is no path
  through which a sender's message text reaches a shell parser.
- **TOTP replay protection.** The 30-second TOTP step plus ±1 window
  means a code is technically valid for ~90 seconds; the in-memory
  replay ring rejects reuse of the exact `(cred_id, step, code)`
  tuple, so a sniffed RF code cannot be replayed by a third party.
- **Sender allowlist** is defense in depth on top of OTP, not a
  replacement for it. APRS callsigns are spoofable.
- **Rate limit** caps how often a single Action can fire (5s default)
  even with valid OTP, capping any DoS-via-script-execution surface.
- **Per-Action FIFO queue** caps concurrent in-flight invocations at
  one. Queue overflow returns `busy` rather than dropping silently.
- **Audit log** records every attempt — successful, denied, malformed,
  rate-limited — so the operator can spot abuse patterns.
- **OTP secrets at rest** are plaintext in `graywolf.db`. UI surfaces
  this. Same protection model as APRS-IS passcode and other graywolf
  creds. Operator is responsible for filesystem-level protection of
  the DB file.
- **No privilege escalation in v1.** Commands run as the `graywolf`
  service user. Operators needing privileged operations configure
  `sudo` rules out-of-band.

## 13. Out of scope (v1)

- **Multi-action chains / scripting language.** v1 fires one Action
  per message; no conditional logic, no piping, no fan-out.
- **Per-Action OTP secret.** v1 keeps OTP secrets in a separate table
  shared across actions; one cred per action.
- **HOTP support.** v1 is TOTP only. The schema's `algorithm`/`digits`/
  `period` columns leave room to add HOTP later.
- **Encrypted secrets at rest.** v1 stores plaintext (decision: user
  prefers reliability over crypto theater).
- **Composing Actions through the Messages tab UI.** A future "Send
  command with OTP" composer in Messages can drop a properly-formatted
  `@@<otp>#<action> [args]` body for the operator. v1 ships without it.
- **Privileged execution.** No setuid knob.
- **Multi-station OTP credentials with per-cred ACL.** v1 has one
  credential per Action and a separate sender-callsign allowlist.

## 14. Open questions

- **QR rendering.** Server-side (returning a PNG) or client-side
  (rendering from the `otpauth://` URI in JS)? Client-side avoids a
  new server-side dependency; server-side is one fewer JS lib. Defer
  to the implementation plan.
- **Action listener addressees** — should there be a per-listener
  config for "this listener's Actions are read-only by default" or
  similar policy? Not needed for v1 but worth noting.
- **Listener addressee uniqueness against tactical aliases.** Tactical
  aliases live in `pkg/messages/tactical_set.go`; we should refuse to
  register a listener addressee that collides with a registered
  tactical alias (or vice versa) to keep classification deterministic.
- **Test button auth.** The Test button bypasses OTP/sender-allowlist
  because the operator is already authenticated to the web UI. This
  should be stated clearly in the dialog so the operator does not
  conclude their action is "unprotected" based on test results.

## 15. Change log

- 2026-05-02 — initial design, approved by user after mockup review.
