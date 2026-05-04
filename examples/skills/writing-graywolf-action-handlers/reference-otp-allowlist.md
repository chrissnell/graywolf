# Reference: OTP credentials, sender allowlist, arg schemas

How to size the three Action-level controls that gate a handler
*before* its code runs. Read [`SKILL.md`](SKILL.md) first for the
threat model.

## OTP credential

The OTP credential is a TOTP secret (RFC 6238) shared between the
operator and the trusted senders. The sender bakes it into their
APRS client; graywolf verifies the six-digit code on every fire.

### When OTP is mandatory

- **Any irreversible action** (open a garage, unlock a door, send
  money, send SMS, send email).
- **Any action that consumes a paid credential** (Twilio, AWS, any
  metered API).
- **Any action that talks to an actuator on the physical world.**

### When OTP is optional

- Pure read-only lookups (weather, ISS pass, uptime, solar weather).
  These are fine to leave unauthenticated; the worst-case abuse is
  small RF chatter and graywolf's per-Action rate limit caps it.

### Single-user station — what the UI does *not* show

Graywolf is a single-user station (see
`feedback_single_user_station`). The credentials editor deliberately
hides issuer / account fields:

- **Issuer** is always `Graywolf`.
- **Account** is always the station callsign.

Do not fabricate UI fields that the operator must populate. The skill
must not generate documentation that says "set issuer to ...".

### TOTP digit handling

The `@@`-prefix grammar treats the digits as **optional**; the
per-Action `OTPRequired` toggle gates enforcement (see
`project_actions_grammar`). Concretely:

| `OTPRequired` | Wire format accepted | Wire format rejected |
|---|---|---|
| `false` | `@@#name args` (no digits) and `@@123456#name args` | nothing |
| `true` | `@@123456#name args` | `@@#name args` (returns `bad_otp: missing`) |

When `OTPRequired = true`, the runner accepts a one-step skew on
either side (±30 s by default) and refuses replays of the same code
within a 3-step + 30 s ring.

### Minting a credential

- `/#/actions` → **Credentials** → **New**.
- The secret is shown **once**. The operator must capture it now —
  graywolf will never display it again.
- The secret is `secret_b32` (Base32, RFC 4648). Most TOTP apps
  (1Password, Bitwarden, Aegis, Google Authenticator) accept either
  the QR code or the Base32 secret directly.

### Pointing an Action at the credential

- In the Action editor, select the credential from the dropdown.
- Toggle `OTPRequired = true`.
- Save.

Reminder for the skill: the Action's OTP credential dropdown is
empty until at least one credential exists. If the operator wants
OTP and has no credential, **mint the credential first**.

## Sender allowlist

A comma-separated list of callsigns that are permitted to fire this
Action. Empty = anyone may fire. Non-empty = only listed senders
pass; everyone else gets `denied`.

### Matching rules

- **Base-call match.** SSID is stripped before comparison. `KE0XYZ-9`
  and `KE0XYZ` both match an allowlist entry of `KE0XYZ`.
- **Case-insensitive.** APRS callsigns are conventionally upper-case
  but the matcher does not enforce it.
- **Wildcards are not supported.** Each callsign is exact (post-SSID).

### When an allowlist is mandatory

- Anything with a paid credential downstream (Twilio, AWS, etc.) —
  even with OTP, an allowlist limits the blast radius if the OTP
  secret leaks.
- Anything physical / irreversible.
- Anything where the operator can enumerate the trusted senders
  (themselves, their spouse, their club's officers).

### When an allowlist is optional

- Read-only public-good Actions (weather lookup, ISS pass) — leave
  empty so anyone in the field can use the service.
- Demo / test Actions on a closed band.

### Ordering — why this matters

The classifier checks the allowlist **before** the OTP probe (see
`pkg/actions/classifier.go`). A denied sender cannot enumerate which
OTP digits validate by trial-and-error — they get `denied` and the
classifier short-circuits before touching the OTP ring.

The operator does not need to design around this; it is just the
property the skill should mention if asked.

## Argument schemas

Each `kv` argument has a row in the Action editor with three columns:

| Column | Meaning | Default |
|---|---|---|
| `key` | argument name on the wire | required, `[a-z][a-z0-9_]*` |
| `required` | reject if missing | `false` |
| `regex` | sanitizer-side validation | empty (anything passes) |

For `freeform` Actions, the schema has a single synthetic row keyed
`arg` and the same regex column applies as a first-pass filter.

### Anchoring

**Always anchor with `^` and `$`.** A regex like `\+[1-9][0-9]+`
matches *anywhere* in the string, so `garbage+15551234567` would
slip through. Use `^\+[1-9][0-9]{6,14}$`.

The Action editor's regex validator runs `new RegExp(...)` on blur;
it does not enforce anchoring. The skill must.

### Common shapes

| Field | Recommended regex |
|---|---|
| E.164 phone | `^\+[1-9][0-9]{6,14}$` |
| ICAO airport | `^[A-Z0-9]{4}$` |
| Boolean state | `^(on\|off)$` |
| Garage state | `^(open\|close\|toggle)$` |
| HA light entity | `^light\.[a-z0-9_]+$` |
| HA switch entity | `^switch\.[a-z0-9_]+$` |
| Brightness 0-255 | `^([0-9]\|[1-9][0-9]\|1[0-9]{2}\|2[0-4][0-9]\|25[0-5])$` |
| Slug | `^[a-z0-9-]{1,32}$` |
| Hex digest 64 | `^[a-f0-9]{64}$` |
| Tactical alias | `^[A-Z0-9]{1,9}$` |

### What the regex *cannot* protect against

The schema regex runs in graywolf's process and is the *first* line
of defense. The handler script must still revalidate (see the
platform reference files). Reasons:

- The operator might widen the regex later for a different reason
  and the script keeps running.
- The schema does not protect against semantic abuse (a numeric arg
  in range but addressing the wrong entity).
- For freeform Actions, graywolf strips control characters
  unconditionally but does not enforce the regex by default — the
  script still revalidates.

## Rate limiting + queue depth

Two more knobs in the Action editor:

- **`RateLimitSec`** — minimum seconds between fires of the same
  Action. Honest: `lastFire` rolls back on `busy` rejects so the
  window is not poisoned by queued requests.
- **`QueueDepth`** — per-Action worker queue. Once full, further
  fires return `busy` immediately rather than piling up.
- **`TimeoutSec`** — executor enforces this; the script's network
  timeouts should be lower so the executor's timeout is the *outer*
  bound.

Set conservative values for any Action that consumes a paid
credential. A `RateLimitSec` of 30 + `QueueDepth` of 1 caps a runaway
sender to two fires per minute — usually plenty for an SMS or HA
toggle, and a hard ceiling on credential abuse.
