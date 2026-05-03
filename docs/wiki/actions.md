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
  `[A-Za-z0-9._-]`). Case-sensitive.
- `[k=v ...]` is space-separated key/value tokens, validated against
  the Action's `arg_schema` (a JSON list of `ArgSpec`).

Source: [`../../pkg/actions/parser.go`](../../pkg/actions/parser.go),
[`../../pkg/actions/sanitize.go`](../../pkg/actions/sanitize.go).

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
       hit?  -> runner.Submit (or Reply for short-circuits)
                -> executor.Execute
                -> reply + audit
       miss? -> messages.Router (normal inbox)
```

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
| `actions` | unique `name`, FK `otp_credential_id -> otp_credentials(id)` ON DELETE SET NULL. `OTPRequired` column has gorm `default:true`; an explicit `false` from a `bool` wire field is indistinguishable from omitted, so the persisted row reads back `true`. Tests that round-trip a created action and then PUT it must override `OTPRequired` after the create response. |
| `otp_credentials` | unique `name`, plaintext `secret_b32` (per spec — UI surfaces it once at create time, never reads back) |
| `action_listener_addressees` | unique `addressee` (uppercase, 1..9 chars) |
| `action_invocations` | append-only audit; FK `action_id -> actions(id)` ON DELETE SET NULL; FK `otp_credential_id -> otp_credentials(id)` ON DELETE SET NULL; `action_name_at` and `OTPCredName` are denormalized so a row stays readable after deletion |

All four models are deliberately *not* in the AutoMigrate list — the
migration is the single source of truth for their schema.

## Cross-references

- Plan / design intent:
  [`../superpowers/plans/2026-05-02-graywolf-actions.md`](../superpowers/plans/2026-05-02-graywolf-actions.md)
- Operator handbook page: see Phase J (forthcoming).
- Wire grammar lives only in
  [`../../pkg/actions/parser.go`](../../pkg/actions/parser.go); the
  classifier never reparses on its own.
- The `@@` sentinel is the sole hot-path discriminator; if you add
  another trigger, update this page and `pkg/actions/classifier.go`
  in the same change.
