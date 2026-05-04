#!/usr/bin/env python3
"""aiohttp webhook receiver -- same contract as the Flask example, with
async handlers and stdlib-style sqlite usage. Provided so operators
who already run aiohttp don't need to add Flask just for graywolf.
"""

from __future__ import annotations

import hmac
import os
import re
import sqlite3
from typing import Final

from aiohttp import web

SHARED_SECRET: Final = os.environ.get("GW_WEBHOOK_SECRET", "")
MAX_BODY_BYTES: Final = 8 * 1024
MAX_ARG_LEN: Final = 200
DB_PATH: Final = os.environ.get("GW_RECEIVER_DB", "/var/lib/graywolf-receiver/log.db")
CONTROL_CHARS = re.compile(r"[\x00-\x1f\x7f]")


def verify_secret(provided: str | None) -> bool:
    if not SHARED_SECRET or provided is None:
        return False
    return hmac.compare_digest(provided.encode(), SHARED_SECRET.encode())


def revalidate(value: str) -> None:
    if len(value) > MAX_ARG_LEN:
        raise web.HTTPBadRequest(reason="value too long")
    if CONTROL_CHARS.search(value):
        raise web.HTTPBadRequest(reason="control chars in value")


async def handle(request: web.Request) -> web.Response:
    if not verify_secret(request.headers.get("X-Graywolf-Auth")):
        raise web.HTTPUnauthorized()
    if (request.content_length or 0) > MAX_BODY_BYTES:
        raise web.HTTPRequestEntityTooLarge(MAX_BODY_BYTES, request.content_length or 0)

    form = await request.post()
    action = str(form.get("action", ""))
    sender = str(form.get("sender_callsign", ""))
    source = str(form.get("source", ""))
    payload = str(form.get("arg") or "")

    for v in (action, sender, source, payload):
        revalidate(v)

    # Synchronous sqlite is fine for low-rate APRS traffic; switch to
    # aiosqlite if you expect bursts.
    with sqlite3.connect(DB_PATH) as db:
        db.execute(
            """INSERT INTO deliveries
               (action, sender, otp_verified, source, payload)
               VALUES (?, ?, ?, ?, ?)""",
            (action, sender, int(form.get("otp_verified") == "true"), source, payload),
        )

    # No HTML rendered; respond with text/plain so XSS surface is zero.
    return web.Response(text=f"received from {sender}", content_type="text/plain")


def init_db() -> None:
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


def main() -> None:
    init_db()
    app = web.Application(client_max_size=MAX_BODY_BYTES)
    app.router.add_post("/aprs-webhook", handle)
    web.run_app(app, host="127.0.0.1", port=5000)


if __name__ == "__main__":
    main()
