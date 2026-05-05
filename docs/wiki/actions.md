# Actions subsystem

Operator-defined trigger surface that turns inbound APRS messages of
the form `@@<otp>#<action> [k=v ...]` into local commands or webhook
calls, replies on-air with the outcome, and audits every attempt.
Lives in [`../../pkg/actions/`](../../pkg/actions/) with persistence
in [`../../pkg/configstore/`](../../pkg/configstore/) and ingress
hooks in [`../../pkg/app/`](../../pkg/app/).

## Wire grammar

```
@@<otp>#<action> [k=v ...]
```

- `@@` is the sentinel that diverts the message from the messages
  router into the Actions runner. Without it, the message lands in
  the inbox unchanged.
- `<otp>` is empty (when the matching Action has `OTPRequired = false`)
  or exactly six ASCII digits. `@@123456#unlock`.
- `<action>` is the Action's `name` (1..32 chars,
  `[A-Za-z0-9._-]`). **Case-insensitive on the wire** — senders may
  type any case; the classifier uppercases before lookup, the
  configstore stores names uppercase (Action.BeforeSave hook), and the
  case-insensitive `GetActionByName` resolves any case to the
  canonical row. Audit rows and on-air replies always carry the
  uppercase form. Migration 18 backfills any pre-change rows.
- `[k=v ...]` is space-separated key/value tokens, validated against
  the Action's `arg_schema` (a JSON list of `ArgSpec`).

Source: [`../../pkg/actions/parser.go`](../../pkg/actions/parser.go),
[`../../pkg/actions/sanitize.go`](../../pkg/actions/sanitize.go).

### Argument mode

Each Action has an `arg_mode` column with values `kv` (default) or
`freeform`:

| Mode | Wire grammar | After-the-verb shape |
|---|---|---|
| `kv` | `@@<otp>#<action> [k=v ...]` | tokenized, validated per-key against `arg_schema` |
| `freeform` | `@@<otp>#<action> <text>` | one untokenized payload, validated against a single-row schema |

The classifier branches in `Classify` (`pkg/actions/classifier.go`):
`kv` calls `Sanitize`, `freeform` calls `SanitizeFreeform`. The
freeform path applies a server-side ceiling of 200 bytes and an
unconditional control-character floor (0x00..0x1F, 0x7F) regardless
of operator regex.

The cmd executor (`buildArgv`/`buildEnv` in `cmd_executor.go`)
detects freeform via the synthetic `FreeformArgKey == "arg"` and
emits a bare positional argv plus `GW_ARG=<value>`. Webhook
templates expose the value as the bare `{{arg}}` token; the new
filter syntax (`|json` / `|url` / `|html`) covers escaping for
operator-set body templates.

Operator-facing documentation lives in
[`../handbook/actions-handler-safety.html`](../handbook/actions-handler-safety.html).

## Trigger surface

An inbound message is a candidate when its addressee matches **any**
of:

1. The station's primary callsign (base-call match, SSID stripped).
2. An enabled tactical alias (`pkg/messages/TacticalSet`, shared
   with the messages router).
3. An operator-defined listener addressee
   (`action_listener_addressees` table, snapshotted live in
   `pkg/actions/AddresseeSet`).

The same `messages.MatchAddressee` helper that the messages router
uses is exported precisely so the classifier and the router agree on
"is this for us." Source:
[`../../pkg/messages/router.go`](../../pkg/messages/router.go).

## Hot path

```
RF or IS frame
  -> aprs.Parse
  -> classifier.Classify(pkt)
       hit on @@ + addressed-to-us?
         -> messages.Preflight.SendAutoAck (every copy)
         -> messages.Preflight.CheckDedup
              hit?  -> consumed, no Submit (first copy already replied)
              miss? -> runner.Submit (or Reply for short-circuits)
                       -> executor.Execute
                       -> reply + audit
       miss? -> messages.Router (normal inbox)
                -> messages.Preflight.SendAutoAck (same instance)
                -> messages.Preflight.CheckDedup (same window)
                -> persist + reply-ack correlation
```

## Preflight: shared auto-ACK + dedup

`messages.Preflight` (`pkg/messages/preflight.go`) owns the
(from, msg_id, text_hash) dedup ring and the auto-ACK transport
(RF via `txgovernor.TxSink`, IS via `IGateLineSender`). One instance
is constructed by `messages.Service.NewService` and shared between
`messages.Router` and `actions.Classifier`. This is the only place
auto-ACK and dedup are implemented for inbound APRS messages -- both
subsystems consult the same window so an `@@`-prefixed action and a
plain DM with the same `(from, msgid, text)` would (in the
hypothetical case of overlap) collapse into one ACK and one execution.

Operational consequence: prior to this hookup, `@@` packets bypassed
the router entirely, so the action sender never saw an ACK and kept
retrying every 30 s for ~5 min. iGate fan-out compounded the problem
by delivering 3-10 identical copies. Each copy executed the action
(or, after the first, bounced as `rate_limited`). The preflight
collapses the storm: every copy is acked (APRS101 §14.2), but only
the first copy reaches the runner.

Classifier hooks live in:
- RF: [`../../pkg/app/rxfanout.go`](../../pkg/app/rxfanout.go)
  `dispatchRxFrame`, **before** `aprsSubmit.submit`. If consumed,
  the packet does not reach the messages router; station cache still
  updates so action senders remain visible in the heard-station table.
- IS: [`../../pkg/app/wiring.go`](../../pkg/app/wiring.go)
  `onIGateIsRxPacket`, **before** `Router.SendPacket`. Same skip
  semantics.

Third-party APRS101 ch 20 envelopes are unwrapped before
classification — gated traffic dispatches the same as direct.

## Failure modes (all on-air replies)

| Status | When | Notes |
|---|---|---|
| `ok` | executor returned no error | first 50-rune output line snippeted |
| `unknown` | `@@`-prefixed but parse error or no Action by that name | distinct from store failure |
| `error: store` | DB lookup failed for non-NotFound reason | logged separately so operators see real outages |
| `error: schema:<name>` | Action's `arg_schema` JSON failed to decode | operator config bug |
| `error: panic` | executor panicked (recover guard in runner) | worker survives |
| `denied` | sender allowlist miss | runs before OTP probe so a denied sender can't probe digit validity |
| `no_credential` | `OTPRequired=true` but FK is null or credential row missing | wiring/operator config gap |
| `bad_otp[: missing|replay|verify]` | OTP wrong, empty when required, or replayed within ring TTL | distinct details |
| `bad_arg: <key>` | sanitize failed against the schema | first offending key |
| `disabled` | Action exists but `Enabled=false` | runner short-circuit |
| `busy` | per-Action queue full | `QueueDepth` reached |
| `rate_limited` | within `RateLimitSec` of last fire | `lastFire` rolled back on busy reject so window is honest |
| `timeout` | executor exceeded `TimeoutSec` | enforced by executor, hint via `ExecRequest.Timeout` |

Source:
[`../../pkg/actions/types.go`](../../pkg/actions/types.go),
[`../../pkg/actions/classifier.go`](../../pkg/actions/classifier.go),
[`../../pkg/actions/runner.go`](../../pkg/actions/runner.go).

## Multi-line replies

Actions can emit up to `Action.MaxReplyLines` separate APRS messages
on success. Each non-empty stdout line becomes one outbound DM with
its own message id, ack ladder, and retry budget. Default 1; hard
ceiling `actions.MaxReplyLinesCeiling = 5` enforced by `validateAction`.

- Only `Status == ok` paths fan out. Failure statuses (`bad_arg`,
  `timeout`, `error`, `denied`, `bad_otp`, `disabled`, `unknown`,
  `no_credential`, `busy`, `rate_limited`) always collapse to a
  single reply — multi-line is meaningful only for executor success
  output.
- Line 1 carries the `ok: ` prefix; lines 2..N are bare. Each line
  is sanitized of control chars and capped at `MaxReplyLen = 67`
  runes; longer lines are truncated with the `…` rune and
  `Truncated = true` on the audit row. Blank lines are dropped.
- Source-aware reply transport (RF inbound → IS-fallback override;
  IS inbound → IS-only override) is applied to every line
  independently because each `SendReply` call constructs its own
  `messages.SendMessageRequest`.
- Audit row stores `\n`-joined text in `ReplyText` and the count of
  produced lines in `ReplyLineCount`. The audit row reflects what
  the runner formatted and dispatched; per-line transport ack state
  lives in the messages thread, not in the invocation row.

Source:
[`../../pkg/actions/reply.go`](../../pkg/actions/reply.go) `FormatReplies`,
[`../../pkg/actions/runner.go`](../../pkg/actions/runner.go) `replyAndAudit`.

**Airtime warning surfaced in the operator UI:** an Action set to 5
lines turns one trigger into 5 outbound RF frames plus per-line acks
and retries. Operators should keep `MaxReplyLines` at 1 unless the
script genuinely needs to deliver multiple short messages (e.g.
weather summaries).

## Source-aware reply transport

`MessagesReplySender` echoes the inbound transport back to the
originator by overriding `messages.SendMessageRequest.FallbackPolicyOverride`
on a per-call basis:

| Inbound | Reply policy | Rationale |
|---|---|---|
| RF | `is_fallback` | RF first, IS as backup. The operator's general preference still applies if it differs (caveat: see below). |
| IS | `is_only` | The sender obviously has IS reach; RF is not guaranteed. |

The override is one-shot — only the first dispatch honors it. Retry
manager re-attempts use the operator's stored preference because the
inbound transport context is no longer available.

Source:
[`../../pkg/actions/reply_messages.go`](../../pkg/actions/reply_messages.go),
[`../../pkg/messages/sender.go`](../../pkg/messages/sender.go) (`SendWithPolicy`),
[`../../pkg/messages/service.go`](../../pkg/messages/service.go) (`FallbackPolicyOverride`).

**Known limitation:** the inbound `Channel` is currently dropped on
the reply path. Replies route on the operator's configured TX
channel (`MessagesConfig.TxChannel`), not the channel the action
arrived on. Multi-channel installs (e.g. 144.39 + 144.34) reply on
the default. Tracked as a follow-up.

## Concurrency

| Concern | Mechanism | Source |
|---|---|---|
| Inbound preflight (auto-ACK + dedup) | shared `messages.Preflight` constructed by `messages.Service`, consulted by both `messages.Router` and `actions.Classifier` (mutex-guarded `dedupMap`, atomic auto-ACK channel). Default 5-min window. | `pkg/messages/preflight.go` |
| Per-Action queue + worker | `actionQueue` in runner; lazily spawned on first `Submit`; `q.mu` held across rate-limit reservation and channel send | `pkg/actions/runner.go` |
| Listener-addressee snapshot | `atomic.Pointer[map[string]struct{}]`, mirrors `messages.TacticalSet` semantics | `pkg/actions/addressees.go` |
| OTP replay ring | per-(credID, step, sha256(code)) entry with TTL = 3 steps + 30s; ±1-step probe covers boundary | `pkg/actions/otp.go` |
| OTP ring sweeper | 5-minute ticker started by `Service`; `sync.Once`-guarded stop | `pkg/actions/otp.go` (`StartOTPSweeper`) |
| Audit pruner | 24-hour ticker, retains last 1000 rows OR 30 days, whichever larger | `pkg/actions/audit.go` |
| Executor panic recovery | `runner.executeWithRecover` maps panic to `StatusError "panic"` so the worker goroutine survives | `pkg/actions/runner.go` |

## Lifecycle

`actions.Service` is the composition root. Constructed in
`wireActions` (in [`../../pkg/app/wiring.go`](../../pkg/app/wiring.go))
**after** `wireMessages` so the reply adapter rides
`messages.Service`. Registered as `actionsComponent` in
`startOrder`:

```
... -> messagesComponent -> actionsComponent -> httpComponent
```

Reverse-startup stop ordering means `actionsComponent.stop` runs
**before** `messagesComponent.stop`, so any in-flight reply send
still has a live `messages.Service` to push through. `Service.Stop`
is idempotent: stops the OTP sweeper, the audit pruner, then drains
runner queues.

`wireActions` is non-fatal: a construction error logs and leaves
`a.actions` nil; the rxfanout and IS hooks tolerate nil.

## Database schema

Migration 15 (`pkg/configstore/migrate_actions.go`, raw SQL — not
AutoMigrate). Four tables:

| Table | Notes |
|---|---|
| `actions` | unique `name`, FK `otp_credential_id -> otp_credentials(id)` ON DELETE SET NULL. The `Enabled` and `OTPRequired` columns deliberately omit `default:true` from their gorm tags even though the SQL DDL keeps `DEFAULT 1` (downgrade-safety). Reason: gorm uses the gorm-tag default as the value to send when the Go field is its zero value, which would conflate a genuine `false` from the wire with "field not set" and silently flip a freshly-created disabled action back to enabled. Application layer always provides the explicit value. Also carries a per-Action `MaxReplyLines` column (1..5; default 1) gating the multi-line on-air reply fan-out. |
| `otp_credentials` | unique `name`, plaintext `secret_b32` (per spec — UI surfaces it once at create time, never reads back) |
| `action_listener_addressees` | unique `addressee` (uppercase, 1..9 chars) |
| `action_invocations` | append-only audit; FK `action_id -> actions(id)` ON DELETE SET NULL; FK `otp_credential_id -> otp_credentials(id)` ON DELETE SET NULL; `action_name_at` and `OTPCredName` are denormalized so a row stays readable after deletion. Also carries a `ReplyLineCount` column tracking the number of reply lines the runner produced for this invocation. |

All four models are deliberately *not* in the AutoMigrate list — the
migration is the single source of truth for their schema.

## Operator UI

Web entry: `/#/actions` (route file
[`../../web/src/routes/Actions.svelte`](../../web/src/routes/Actions.svelte)).
Page-level state (actions, credentials, listeners, invocations) lives
in the singleton store
[`../../web/src/lib/actions/store.svelte.js`](../../web/src/lib/actions/store.svelte.js)
and is hydrated through the openapi-fetch wrappers in
[`../../web/src/lib/actions/api.js`](../../web/src/lib/actions/api.js).

### Components

| File | Role |
|---|---|
| `web/src/components/actions/ActionsTable.svelte` | List of actions; per-row Edit / Test / Delete. Edit/Test buttons fan out to the modals below. |
| `web/src/components/actions/CredentialsTable.svelte` | OTP credential list. New-credential modal lands in Phase I. |
| `web/src/components/actions/InvocationsPanel.svelte` | Recent invocation log with filter bar + 5s live-poll. |
| `web/src/components/actions/EditActionModal.svelte` | Single Edit/New form covering every field in `dto.Action`. Type-aware: command vs webhook sub-block swaps. Re-uses `ArgSchemaEditor`, `SenderAllowlistEditor`, `HeadersEditor`. |
| `web/src/components/actions/ArgSchemaEditor.svelte` | Per-key arg-schema row editor. Validates each row's regex via `new RegExp` on blur and exposes `hasErrors()` so the parent can block save. |
| `web/src/components/actions/SenderAllowlistEditor.svelte` | Chip-style callsign editor that round-trips a CSV string. |
| `web/src/components/actions/HeadersEditor.svelte` | Two-column key/value grid for webhook headers. Validates header names, exposes `hasErrors()`. |
| `web/src/components/actions/TestActionDialog.svelte` | Per-row Test dialog. Renders one input per arg in the action's `arg_schema`, calls `actionsApi.testFire(id, args)`, shows a status-badged result panel. OTP / sender-allowlist checks are bypassed because the operator is already authenticated to the web UI. |
| `web/src/lib/actions/status.js` | Shared `statusVariant(status)` and `badArgKey(replyText)` helpers consumed by `InvocationsPanel` and `TestActionDialog` so colors and bad-arg parsing stay in lockstep. |
| `web/src/lib/actions/grammar.js` | Tiny formatter for the example-message banner + the `parseAllowlist` CSV splitter shared with the chip editor. |

### Test fire endpoint

The Test dialog hits `POST /api/actions/{id}/test-fire` (handler in
[`../../pkg/webapi/action_test_fire.go`](../../pkg/webapi/action_test_fire.go)).
Per spec §11, this short-circuits OTP and sender-allowlist checks but
exercises the full executor + sanitization + audit-row write path,
making it the operator's primary debug surface for newly defined
Actions. The dialog warns about the bypass in its subhead so the
operator does not conclude their action is "unprotected" based on test
results.

### Things worth knowing

- `dto.ArgSpec.regex` and `dto.ArgSpec.required` are JSON
  `omitempty`. Wire payloads for the simplest valid row are
  `{"key":"foo"}`. The Edit modal normalizes those undefined fields
  to `''` / `false` on load before binding to chonky-ui's `Input` /
  `Toggle`, which would otherwise crash with
  `props_invalid_value`.
- The OTP credential select binds to a string for chonky-ui
  compatibility but the wire field is `*uint`. The mirroring effect
  in `EditActionModal` translates between the two; do not bind the
  Select directly to `form.otp_credential_id`.
- Issuer / account fields are intentionally hidden from the
  credentials table; per the single-user-station design, the issuer
  is always Graywolf and the account is always the station callsign.
  See `feedback_single_user_station` memory for the rationale.

## Cross-references

- Plan / design intent:
  [`../superpowers/plans/2026-05-02-graywolf-actions.md`](../superpowers/plans/2026-05-02-graywolf-actions.md)
- Operator handbook page: [`../handbook/actions.html`](../handbook/actions.html).
- Wire grammar lives only in
  [`../../pkg/actions/parser.go`](../../pkg/actions/parser.go); the
  classifier never reparses on its own.
- The `@@` sentinel is the sole hot-path discriminator; if you add
  another trigger, update this page, `pkg/actions/classifier.go`,
  and ensure the new path also calls into `messages.Preflight` so
  duplicates do not slip past dedup.
- Outbound counterpart (macro/credential CRUD + Messages drawer):
  [`remote-actions.md`](remote-actions.md). The two subsystems share
  only `parser.go` (exported `ValidActionName`, `MaxActionNameLen`).
  Operator-facing invariant: the **Secret Key** value shown in the
  inbound credential UI here is the *same string* the sender pastes
  into the outbound `EditCredentialModal` — there is no key exchange,
  operators copy-paste the value over a side channel they trust.
