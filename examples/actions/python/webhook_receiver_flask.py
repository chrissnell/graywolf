#!/usr/bin/env python3
"""Flask webhook receiver for graywolf Actions.

Wire this up as a 'webhook' Action with method POST. Set the URL to
http://your-host:5000/aprs-webhook and add a header
``X-Graywolf-Auth: <SHARED_SECRET>`` so this receiver can verify the
caller is your graywolf instance and not a random internet host that
guessed your URL.

Defense layers, in order, mirror the script-side example:
  1. Constant-time secret comparison (no early-out timing oracle).
  2. Body-size cap before parsing (defense against future graywolf bugs).
  3. Form-encoded parse (graywolf's default), NOT JSON eval.
  4. Per-field revalidation (length, character class).
  5. Parameterized SQL -- never string-concat user input.
  6. Auto-escaped templating -- Jinja escapes by default; we keep the
     default and never use ``Markup`` or ``|safe`` on user input.
  7. argv-style subprocess if shelling out (we don't here, but the
     pattern is shown in the comments).
"""

from __future__ import annotations

import hmac
import os
import re
import sqlite3
from typing import Final

from flask import Flask, abort, render_template_string, request

# ---- Configuration ----------------------------------------------------

SHARED_SECRET: Final = os.environ.get("GW_WEBHOOK_SECRET", "")
MAX_BODY_BYTES: Final = 8 * 1024  # graywolf bodies are tiny; cap aggressively
MAX_ARG_LEN: Final = 200          # mirrors actions.FreeformValueCeiling
DB_PATH: Final = os.environ.get("GW_RECEIVER_DB", "/var/lib/graywolf-receiver/log.db")

CONTROL_CHARS = re.compile(r"[\x00-\x1f\x7f]")

# ---- App --------------------------------------------------------------

app = Flask(__name__)
# Cap request bodies at the Werkzeug layer too, so chunked transfer
# encoding (no Content-Length header) still gets bounded.
app.config["MAX_CONTENT_LENGTH"] = MAX_BODY_BYTES


def init_db() -> None:
    """Create the audit table if missing. Schema is small on purpose."""
    with sqlite3.connect(DB_PATH) as db:
        db.execute(
            """CREATE TABLE IF NOT EXISTS deliveries (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                received_at TEXT DEFAULT CURRENT_TIMESTAMP,
                action TEXT NOT NULL,
                sender TEXT NOT NULL,
                otp_verified INTEGER NOT NULL,
                source TEXT NOT NULL,
                payload TEXT NOT NULL
            )"""
        )


def verify_secret(provided: str | None) -> bool:
    """Constant-time comparison.

    A naive ``provided == SHARED_SECRET`` leaks length and prefix
    information through string-equality timing. ``hmac.compare_digest``
    runs in time proportional to the longer string regardless of where
    the first mismatch occurs.
    """
    if not SHARED_SECRET or provided is None:
        return False
    return hmac.compare_digest(provided.encode(), SHARED_SECRET.encode())


def revalidate(value: str) -> None:
    """Reject control chars and oversize values. Raises 400 on violation."""
    if len(value) > MAX_ARG_LEN:
        abort(400, "value exceeds max length")
    if CONTROL_CHARS.search(value):
        abort(400, "value contains control characters")


@app.before_request
def cap_body_size() -> None:
    """Reject oversize bodies before parsing them.

    Werkzeug's ``MAX_CONTENT_LENGTH`` (set above) is the load-bearing
    cap; this middleware runs first and rejects requests that
    advertise an oversize Content-Length without ever reading the
    body, which is friendlier on the network for obviously-huge
    requests.
    """
    cl = request.content_length
    if cl is not None and cl > MAX_BODY_BYTES:
        abort(413, "request body too large")


@app.post("/aprs-webhook")
def aprs_webhook():
    # ---- Authenticate -----------------------------------------------
    if not verify_secret(request.headers.get("X-Graywolf-Auth")):
        abort(401)

    # ---- Parse (form-encoded; that's graywolf's default body shape) -
    form = request.form
    action = form.get("action", "")
    sender = form.get("sender_callsign", "")
    otp_verified = form.get("otp_verified", "false") == "true"
    source = form.get("source", "")
    payload = form.get("arg", "")  # graywolf's freeform key

    # ---- Revalidate -------------------------------------------------
    for v in (action, sender, source, payload):
        revalidate(v)

    # ---- Persist ----------------------------------------------------
    #
    # Parameterized query. The ``?`` placeholders are bound by the
    # driver -- there is no string concatenation of user input into the
    # SQL, so SQL injection is impossible regardless of payload bytes.
    with sqlite3.connect(DB_PATH) as db:
        db.execute(
            """INSERT INTO deliveries
               (action, sender, otp_verified, source, payload)
               VALUES (?, ?, ?, ?, ?)""",
            (action, sender, int(otp_verified), source, payload),
        )

    # ---- Render the reply -------------------------------------------
    #
    # Jinja autoescapes by default. ``payload`` contains user-controlled
    # bytes -- if we used ``Markup(payload)`` or piped through ``|safe``,
    # an attacker could inject HTML into our response. We don't.
    return render_template_string(
        "<p>received from <strong>{{ sender }}</strong>: {{ payload }}</p>",
        sender=sender,
        payload=payload,
    )


if __name__ == "__main__":
    init_db()
    # Bind to localhost; put a TLS-terminating reverse proxy in front.
    app.run(host="127.0.0.1", port=5000)
