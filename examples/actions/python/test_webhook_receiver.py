"""Tests for webhook_receiver_flask.

Run: pytest examples/actions/python/
"""

from __future__ import annotations

import pytest


@pytest.fixture
def app(monkeypatch, tmp_path):
    db_path = tmp_path / "test.db"
    monkeypatch.setenv("GW_WEBHOOK_SECRET", "test-secret")
    monkeypatch.setenv("GW_RECEIVER_DB", str(db_path))
    # Re-import to pick up the env vars.
    import importlib

    import webhook_receiver_flask
    importlib.reload(webhook_receiver_flask)
    webhook_receiver_flask.init_db()
    webhook_receiver_flask.app.config["TESTING"] = True
    yield webhook_receiver_flask.app


def test_rejects_missing_auth(app):
    client = app.test_client()
    resp = client.post("/aprs-webhook", data={"action": "x", "sender_callsign": "K", "source": "rf", "arg": "hi"})
    assert resp.status_code == 401


def test_rejects_wrong_auth(app):
    client = app.test_client()
    resp = client.post(
        "/aprs-webhook",
        headers={"X-Graywolf-Auth": "wrong"},
        data={"action": "x", "sender_callsign": "K", "source": "rf", "arg": "hi"},
    )
    assert resp.status_code == 401


def test_accepts_valid_request(app):
    client = app.test_client()
    resp = client.post(
        "/aprs-webhook",
        headers={"X-Graywolf-Auth": "test-secret"},
        data={"action": "notify", "sender_callsign": "KE0XYZ", "source": "rf", "arg": "hello world"},
    )
    assert resp.status_code == 200
    assert b"KE0XYZ" in resp.data


def test_rejects_control_chars(app):
    client = app.test_client()
    resp = client.post(
        "/aprs-webhook",
        headers={"X-Graywolf-Auth": "test-secret"},
        data={"action": "notify", "sender_callsign": "KE0XYZ", "source": "rf", "arg": "hi\x00there"},
    )
    assert resp.status_code == 400


def test_rejects_oversize_value(app):
    client = app.test_client()
    resp = client.post(
        "/aprs-webhook",
        headers={"X-Graywolf-Auth": "test-secret"},
        data={"action": "notify", "sender_callsign": "KE0XYZ", "source": "rf", "arg": "a" * 201},
    )
    assert resp.status_code == 400


def test_xss_payload_is_escaped(app):
    client = app.test_client()
    resp = client.post(
        "/aprs-webhook",
        headers={"X-Graywolf-Auth": "test-secret"},
        data={"action": "notify", "sender_callsign": "KE0XYZ", "source": "rf", "arg": "<script>alert(1)</script>"},
    )
    assert resp.status_code == 200
    # Jinja autoescape converts < and >; the literal <script> tag must
    # not appear in the rendered body.
    assert b"<script>" not in resp.data
    assert b"&lt;script&gt;" in resp.data
