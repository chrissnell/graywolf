# Reference: Webhook handlers

Authoring guide for HTTP receivers that graywolf POSTs to as a
**webhook** Action. Examples in Python (Flask / aiohttp); the
patterns are language-independent. Every shipped Python receiver in
`examples/actions/python/` is ruff-clean and exercised by pytest.

> **Read [`SKILL.md`](SKILL.md) first.** Threat model, credential
> blast-radius, and universal invariants live there.

## When to choose webhook over a script

A webhook is the right answer when:

- The downstream is already an HTTP API (do not write `curl` in bash;
  use the webhook directly).
- The handler lives on a different host than graywolf.
- Several Actions share a single audit / logging surface.
- You want graywolf's templating filters (`|json` / `|url` / `|html`)
  to do the escaping rather than scripting it yourself.

A webhook is the wrong answer when the side effect is a local-host
operation (toggling a USB-attached relay, reading the host's load
average) — write a shell or PowerShell handler instead and skip the
network round trip.

## What graywolf POSTs

By default, graywolf POSTs `application/x-www-form-urlencoded` with
these fields:

- Always: `action`, `sender_callsign`, `otp_verified` (`"true"` /
  `"false"`), `otp_cred`, `source` (`"rf"` / `"is"`).
- kv: one field per declared arg (key = your schema key).
- freeform: a single field named `arg` with the raw payload.

Headers carry whatever the operator configured on the Action's
**Headers** editor — typically `X-Graywolf-Auth: <shared secret>` or
an HMAC.

### Custom body templates

Each Action can override the body with a templated string. Tokens:

- `{{action}}`, `{{sender-callsign}}`, `{{otp-verified}}`,
  `{{otp-cred}}`, `{{source}}`
- kv: `{{arg.<key>}}`
- freeform: `{{arg}}`
- Filters (use these for any non-default destination):
  - `{{x|json}}` — JSON string contents (escapes `"`, `\`, control chars)
  - `{{x|url}}` — URL paths or query strings (percent-encodes)
  - `{{x|html}}` — HTML attribute values or text content (entity-encodes)

```
{
  "to":  "{{arg.to|json}}",
  "msg": "{{arg.msg|json}}"
}
```

Never write `"{{arg.msg}}"` (no filter) into a JSON template — a `"`
or `\` in the value will break the JSON. The filter is mandatory when
templating into a structured destination.

**Why filters are mandatory even when the arg-schema regex looks
tight.** The schema regex and the body template live on different
lifecycles. The regex protects today's arg shape; the template will
outlive any individual regex. Operators routinely widen a regex
months later — to allow apostrophes in SMS bodies, dashes in slugs,
quotes in subject lines — without revisiting body templates that
quietly assumed the old regex held. `{{arg.msg|json}}` is safe
regardless of what the regex permits; `{{arg.msg}}` is safe only as
long as nobody ever broadens the regex to allow `"` or `\`. **The
filter is the load-bearing defense; the regex is a friendliness
check.** Never invert that.

The same lifecycle reasoning applies to `|url` (path / query string
templates) and `|html` (HTML attribute / text content). Use the
filter for the destination context, not for the input character
class.

## Required defenses (every receiver)

Six defenses must be present **in this order**:

1. **Body-size cap before parsing.** Cap at the framework layer
   *and* in middleware so chunked transfer encoding still gets
   bounded.
2. **Authentication on every request.** Constant-time comparison of
   a shared secret in a custom header, or HMAC of the body. Reject
   401 immediately if missing or wrong. **Secret must be ≥32 bytes
   / 256 bits of entropy** (`openssl rand -hex 32` or
   `openssl rand -base64 32`).
3. **TLS in front.** Run the receiver on `127.0.0.1` and put a
   TLS-terminating reverse proxy (Caddy / nginx / Traefik) in front.
   Never expose the receiver on `0.0.0.0` over plaintext HTTP.
4. **OTP defense in depth.** Even though graywolf is supposed to
   only POST when `OTPRequired=true` matched, the receiver should
   still check `request.form.get("otp_verified") == "true"` and
   `abort(401)` otherwise. Cheap, catches the case where the
   operator flips OTPRequired off in the UI without telling the
   receiver.
5. **Per-field revalidation.** Length cap, control-char rejection,
   schema-specific regex for every field that came from the wire.
6. **Parameterized everything downstream.** SQL placeholders,
   auto-escaping templates, argv-style subprocess. The same
   universal invariants from `SKILL.md` apply on this side too.

Skipping any one of these is a CVE waiting to happen. The skill must
verify all six appear in the generated code before declaring it
ready.

## Authentication patterns

### Shared secret in a header

Simplest. Use when the channel is already encrypted (TLS) and the
receiver does not need to verify message integrity beyond
authentication.

```python
import hmac, os
from flask import abort, request

SHARED_SECRET = os.environ["GW_WEBHOOK_SECRET"]

def verify_secret(provided: str | None) -> bool:
    if not provided:
        return False
    return hmac.compare_digest(provided.encode(), SHARED_SECRET.encode())

@app.before_request
def auth_gate():
    if not verify_secret(request.headers.get("X-Graywolf-Auth")):
        abort(401)
```

`hmac.compare_digest` runs in time proportional to the longer string
regardless of where the first mismatch occurs. A naive `==`
comparison leaks length and prefix information through timing.

The Action's Headers editor carries `X-Graywolf-Auth: <secret>`.

**Generate the secret with `openssl rand -hex 32` (32 bytes / 256
bits) — never less.** Hex doubles the character count, so "32 hex
characters" = 16 bytes = 128 bits, which is below the floor. If the
operator says "I'll use a 32-character hex string," correct them
before generating any code: it must be **64 hex characters**, which
is what `openssl rand -hex 32` produces. `openssl rand -base64 32`
is also acceptable (43 base64 characters, same 32 bytes).

Store the secret in the graywolf service environment as
`GW_WEBHOOK_SECRET` and in the receiver's environment under the
same name.

### HMAC over the body

Stronger — verifies that the body has not been tampered with in
flight. Use when the channel is not end-to-end TLS (older relays,
shared infrastructure) or when you want non-repudiation.

```python
import hmac, hashlib, os
from flask import abort, request

KEY = os.environ["GW_WEBHOOK_HMAC_KEY"].encode()

def verify_hmac():
    sig_hex = request.headers.get("X-Graywolf-Sig", "")
    body = request.get_data()  # raw bytes, before parse
    expected = hmac.new(KEY, body, hashlib.sha256).hexdigest()
    if not hmac.compare_digest(sig_hex, expected):
        abort(401)
```

The Action's body template includes the corresponding HMAC; if
graywolf does not yet emit one, fall back to the shared-secret
pattern.

## Body-size cap

```python
MAX_BODY_BYTES = 8 * 1024  # graywolf bodies are tiny

app.config["MAX_CONTENT_LENGTH"] = MAX_BODY_BYTES   # framework cap

@app.before_request
def cap_body_size():
    cl = request.content_length
    if cl is not None and cl > MAX_BODY_BYTES:
        abort(413)
```

Both layers matter. `MAX_CONTENT_LENGTH` is the load-bearing cap
(handles chunked transfer encoding without a `Content-Length`
header). The middleware bails on obviously oversized requests
without reading the body, which is friendlier on the network.

aiohttp equivalent:

```python
app = web.Application(client_max_size=8 * 1024)
```

## Per-field revalidation

```python
import re

MAX_ARG_LEN = 200
CONTROL_CHARS = re.compile(r"[\x00-\x1f\x7f]")

def revalidate(value: str) -> None:
    if len(value) > MAX_ARG_LEN:
        abort(400, "value exceeds max length")
    if CONTROL_CHARS.search(value):
        abort(400, "value contains control characters")
```

Apply on every field that originated from the wire. Even though
graywolf already validates, the receiver must not depend on graywolf
staying bug-free forever.

## Persisting the payload

```python
import sqlite3, os

with sqlite3.connect(os.environ["GW_RECEIVER_DB"]) as db:
    db.execute(
        """INSERT INTO deliveries (action, sender, payload)
           VALUES (?, ?, ?)""",
        (action, sender, payload),
    )
```

Always parameterized. Never f-string. Never `%`-format. Never
concatenate. The driver binds `?` at the protocol level so SQL
injection is impossible regardless of payload bytes.

For PostgreSQL with `psycopg`, use `%s` placeholders the same way:

```python
cur.execute("INSERT INTO deliveries (...) VALUES (%s, %s, %s)", (a, s, p))
```

## Rendering responses

```python
return render_template_string(
    "<p>received from <strong>{{ sender }}</strong>: {{ payload }}</p>",
    sender=sender, payload=payload,
)
```

Jinja autoescapes by default. `{{ payload }}` of
`<script>alert(1)</script>` renders as
`&lt;script&gt;alert(1)&lt;/script&gt;`, not a real script tag.

Forbidden patterns:

- `Markup(payload)` / `{{ payload | safe }}` / `{% autoescape off %}`
- `render_template_string(payload, ...)` — using the payload as a
  *template* re-introduces server-side template injection.
- Hand-built HTML via f-string.

If the response should be JSON, return `jsonify(...)` (Flask) /
`web.json_response(...)` (aiohttp); both encode safely.

## Shelling out from a webhook

If the receiver must call an external binary, use `subprocess` with
a list and `shell=False`:

```python
import subprocess

# GOOD -- list form, shell=False, timeout, capture.
result = subprocess.run(
    ["/usr/local/bin/send", "--to", number, "--body", message],
    shell=False,
    timeout=8,
    capture_output=True,
    check=False,
)

# BAD -- shell=True turns the string into a shell command.
subprocess.run(f"send --to {number} --body {message}", shell=True)
```

Validate inputs before they reach `subprocess`, including length
and a regex that excludes shell metacharacters even though
`shell=False` makes them inert.

## SSRF — never let the payload pick a URL

```python
# DO NOT
requests.get(payload)            # SSRF -- payload becomes a URL.
requests.get(f"https://api/{payload}")
```

If the Action allows the sender to specify any URL component, the
receiver can be turned into an HTTP scanner against the local network
(metadata service, internal admin panels, etc.).

If you need a configurable URL, accept only an *enum* from the
sender (a short list of slugs) and map it to a constant URL inside
the receiver:

```python
TARGETS = {
    "weather": "https://api.weather.gov/...",
    "tides":   "https://api.tidesandcurrents.noaa.gov/...",
}
target = TARGETS.get(payload)
if not target:
    abort(400, "unknown target")
```

## Process isolation + filesystem hygiene

- Run the receiver as a **dedicated user** (`graywolf-webhook` or
  similar). Never as root, never as a user with sudo.
- Restrict filesystem access via `systemd` `ProtectSystem=strict`,
  `PrivateTmp=yes`, `ReadWritePaths=` for only the receiver's data
  dir.
- Write files with explicit modes (`0o600` for secrets, `0o644` for
  logs); never rely on `umask`.
- Rotate the auth secret periodically; coordinate with graywolf's
  Action editor.

## Linter — mandatory

```bash
ruff check examples/actions/python/handler.py
ruff format --check examples/actions/python/handler.py
bandit examples/actions/python/handler.py
```

`ruff` covers style + a large class of static-analysis rules.
`bandit` adds security-oriented checks (uses of `subprocess` with
`shell=True`, `yaml.load` without `SafeLoader`, hard-coded passwords,
etc.). Triage every finding; do not suppress with comments.

The skill must run all three on the generated file (or, at minimum,
`ruff` and `bandit`) and confirm clean output before declaring the
receiver ready.

## Anti-pattern table

| Anti-pattern | Attack |
|---|---|
| No auth on the endpoint | Anyone who guesses the URL can POST as if they were graywolf. |
| `==` for secret comparison | Timing oracle on length / prefix. |
| `f"INSERT ... ('{payload}')"` | SQL injection. |
| `{{ payload \| safe }}` / `Markup(payload)` | Stored XSS. |
| `os.system(f"send {payload}")` | Shell injection on the receiver host. |
| `subprocess.run(..., shell=True)` with payload | Same as `os.system`. |
| `requests.get(payload)` | SSRF — payload becomes a URL the server fetches (cloud metadata, internal admin, router). |
| `requests.get(f"https://api.example/{slug}")` with `slug` = payload | Slot-style SSRF — still attacker-controlled URL component. Map `slug` to a constant URL via an enum. |
| `urlopen(payload)` / `httpx.get(payload)` | All HTTP clients have the same SSRF surface. The shape applies regardless of library. |
| Following redirects to attacker-chosen URLs | Even with a constant initial URL, `allow_redirects=True` (default in `requests`) lets the response steer the client elsewhere. Use `allow_redirects=False` for any handler the sender can influence. |
| Slicing the response *after* full read (`r.content[:200]`) | A hostile target streams gigabytes before the slice runs. Use `stream=True` + `r.iter_content(decode_unicode=False)` with a byte budget, or `r.raw.read(N)`. |
| `eval(request.get_data())` | RCE. (Yes, people do this. No, never.) |
| Logging `payload` raw | Log forging via embedded `\r\n`, terminal escapes. |
| No body-size cap | OOM via 4 GB POST. |
| `{{arg.msg}}` in JSON body template (no `\|json`) | A `"` breaks the JSON, smuggles fields. |
| Returning the payload in a redirect URL | Open-redirect / SSRF gadget. |

## Skeleton — Flask

```python
#!/usr/bin/env python3
"""<NAME> webhook receiver.

Wired as a Graywolf 'webhook' Action with method POST,
URL https://your-host/aprs-webhook, and a header
X-Graywolf-Auth: <shared secret>. Default form-encoded body.
"""

from __future__ import annotations

import hmac
import os
import re
import sqlite3
from typing import Final

from flask import Flask, abort, render_template_string, request

SHARED_SECRET: Final = os.environ["GW_WEBHOOK_SECRET"]
DB_PATH: Final = os.environ["GW_RECEIVER_DB"]
MAX_BODY_BYTES: Final = 8 * 1024
MAX_ARG_LEN: Final = 200
CONTROL_CHARS = re.compile(r"[\x00-\x1f\x7f]")

app = Flask(__name__)
app.config["MAX_CONTENT_LENGTH"] = MAX_BODY_BYTES


def verify_secret(provided: str | None) -> bool:
    if not provided:
        return False
    return hmac.compare_digest(provided.encode(), SHARED_SECRET.encode())


def revalidate(value: str) -> None:
    if len(value) > MAX_ARG_LEN:
        abort(400, "value exceeds max length")
    if CONTROL_CHARS.search(value):
        abort(400, "value contains control characters")


@app.before_request
def cap_body_size() -> None:
    cl = request.content_length
    if cl is not None and cl > MAX_BODY_BYTES:
        abort(413)


@app.post("/aprs-webhook")
def aprs_webhook():
    if not verify_secret(request.headers.get("X-Graywolf-Auth")):
        abort(401)

    form = request.form

    # OTP defense in depth: even though the Action is configured with
    # OTPRequired=true, recheck on the receiver side. Cheap insurance.
    if form.get("otp_verified", "false") != "true":
        abort(401)

    action = form.get("action", "")
    sender = form.get("sender_callsign", "")
    payload = form.get("arg", "")

    for v in (action, sender, payload):
        revalidate(v)

    with sqlite3.connect(DB_PATH) as db:
        db.execute(
            "INSERT INTO deliveries (action, sender, payload) VALUES (?, ?, ?)",
            (action, sender, payload),
        )

    return render_template_string(
        "<p>received from <strong>{{ sender }}</strong>: {{ payload }}</p>",
        sender=sender,
        payload=payload,
    )


if __name__ == "__main__":
    app.run(host="127.0.0.1", port=5000)
```

## Operator postamble template

```
Install path:    /opt/graywolf-webhook/<name>.py
Run as user:     graywolf-webhook (dedicated, no sudo)
Reverse proxy:   Caddy / nginx terminating TLS in front of 127.0.0.1:5000

Lint (mandatory):
  ruff check /opt/graywolf-webhook/<name>.py
  ruff format --check /opt/graywolf-webhook/<name>.py
  bandit /opt/graywolf-webhook/<name>.py

Env vars (systemd unit / launchd plist for the receiver, not graywolf):
  GW_WEBHOOK_SECRET=<openssl rand -hex 32>
  GW_RECEIVER_DB=/var/lib/graywolf-webhook/log.db
  ...

Wire it up: /#/actions -> New
  Name: <name>
  Type: webhook
  Method: POST
  URL: https://your-host/aprs-webhook
  Headers: X-Graywolf-Auth: <same secret as GW_WEBHOOK_SECRET>
  Body template: <empty for default form-encoded, or use {{arg|json}} filters>
  Arg schema: <one row per declared arg>
  OTP credential: <chosen credential | none>
  Sender allowlist: <callsigns | empty>
  Timeout: <seconds>

Test: per-row Test dialog before letting it loose on-air.
```
