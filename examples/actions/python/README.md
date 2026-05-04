# Python webhook receivers for graywolf Actions

Two minimal examples that accept an Action's webhook POST, verify a
shared secret, and persist the delivery to SQLite. Pick whichever
matches your existing Python stack:

| File | Framework | Use when |
|---|---|---|
| `webhook_receiver_flask.py` | Flask 3 | You already run Flask, or you want auto-escaped HTML rendering. |
| `webhook_receiver_aiohttp.py` | aiohttp | You're already async and want non-blocking handlers. |

## Wiring

1. Pick a shared secret. Treat it like a password -- generate with
   `openssl rand -hex 32`.
2. Install requirements:
   ```
   pip install -r requirements.txt
   ```
3. Run the receiver behind a TLS-terminating reverse proxy (nginx,
   caddy, traefik). The example binds to `127.0.0.1:5000`; do not
   expose port 5000 to the internet directly.
4. In graywolf, create a webhook Action:
   - **Type**: webhook
   - **Method**: POST
   - **URL**: `https://your-host/aprs-webhook` (the public URL fronted
     by your reverse proxy)
   - **Headers**: `X-Graywolf-Auth: <your-shared-secret>`
   - **Body template**: leave blank to use the default form encoding.
     This is the safest choice -- graywolf URL-encodes every value.
5. Set the receiver's environment variables in your service unit:
   - `GW_WEBHOOK_SECRET=<your-shared-secret>`
   - `GW_RECEIVER_DB=/var/lib/graywolf-receiver/log.db`

## Testing

```
pytest examples/actions/python/
```

## Why this is safe

Both examples implement the defense layers documented in
`docs/handbook/actions-handler-safety.html`:

1. **HMAC-comparison auth** (no timing oracle).
2. **Body-size cap** before parsing (defense in depth against bugs in
   graywolf's own caps).
3. **Form-encoded parse** (graywolf's default), avoiding `eval` or
   raw JSON-with-user-bytes-in-the-template.
4. **Per-field revalidation** (length + control-char floor),
   independent of what the operator-set Action regex allows.
5. **Parameterized SQL** -- `?` placeholders, never f-strings.
6. **Auto-escaped templating** -- Jinja's default; never `|safe` on
   user input. The aiohttp version returns `text/plain` to avoid
   the question entirely.

## What we deliberately don't do

- **No `eval` or `exec`** on the payload, ever.
- **No subprocess shelling out** in the examples. If you need to call
  an external command, use `subprocess.run([...], shell=False)` with a
  list argv and a `--` terminator before any user value.
- **No HTML rendering of unescaped user input.** The Flask example
  uses Jinja autoescape; the aiohttp example returns plain text.
- **No raw JSON body parsing** when graywolf's default form encoding
  is available -- form parsing has a smaller surface than ad-hoc JSON.
