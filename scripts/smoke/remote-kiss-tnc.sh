#!/usr/bin/env bash
#
# scripts/smoke/remote-kiss-tnc.sh — end-to-end smoke test for the
# Remote KISS TNC feature (plan: .context/2026-04-20-kiss-tcp-client-and-channel-backing.md).
#
# This script validates the TX end of the path that ships in Phases 3
# and 4:
#
#   Go KissManager --dial--> mock KISS TCP server
#   TransmitOnChannel -> instanceTxQueue -> writer goroutine ->
#     KISS-framed AX.25 UI frame on the wire
#
# It brings up:
#   1. A throwaway TCP listener (Python, one-shot) that accepts a
#      single connection, reads whatever the client sends within a
#      generous window, and dumps the bytes to a temp file.
#   2. The smoke-kiss-tcp-client helper under graywolf/cmd/, which
#      wires a kiss.Manager tcp-client supervisor with Mode=TNC and
#      AllowTxFromGovernor=true, waits for connected, submits one UI
#      frame via Manager.TransmitOnChannel, then exits.
#
# The assertion is simple and mechanical: the temp file must contain at
# least one KISS frame boundary byte (0xC0, "FEND"). Any KISS-encoded
# payload starts and ends with FEND, so its presence plus a plausible
# byte length is proof-of-life that Graywolf's tcp-client path dialed,
# connected, framed, and wrote successfully.
#
# Exit codes:
#   0 — smoke passed: the mock received KISS bytes.
#   1 — smoke failed: build, listener, helper, or assertion failed.
#   2 — usage error: dependencies missing.
#
# Idempotent. Re-runnable. Cleans up children and temp dirs on any exit
# path via trap.

set -euo pipefail

SCRIPT_NAME="$(basename "$0")"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

log()  { printf '%s: %s\n' "$SCRIPT_NAME" "$*" >&2; }
fail() { log "FAIL: $*"; exit 1; }

# ---------- dependency checks ----------

command -v python3 >/dev/null 2>&1 || { log "python3 required"; exit 2; }
command -v go      >/dev/null 2>&1 || { log "go required";      exit 2; }

# ---------- workspace ----------

TMPDIR="$(mktemp -d "${TMPDIR:-/tmp}/graywolf-smoke-kiss.XXXXXX")"
PEER_BYTES="$TMPDIR/peer-received.bin"
LISTENER_LOG="$TMPDIR/listener.log"
HELPER_LOG="$TMPDIR/helper.log"

PEER_PORT_FILE="$TMPDIR/peer.port"

LISTENER_PID=""
HELPER_BIN=""

cleanup() {
  local rc=$?
  if [[ -n "$LISTENER_PID" ]] && kill -0 "$LISTENER_PID" 2>/dev/null; then
    kill "$LISTENER_PID" 2>/dev/null || true
    wait "$LISTENER_PID" 2>/dev/null || true
  fi
  if [[ $rc -ne 0 ]]; then
    log "leaving temp dir for inspection: $TMPDIR"
    if [[ -s "$LISTENER_LOG" ]]; then
      log "listener log:"; sed 's/^/  /' "$LISTENER_LOG" >&2 || true
    fi
    if [[ -s "$HELPER_LOG" ]]; then
      log "helper log:"; sed 's/^/  /' "$HELPER_LOG" >&2 || true
    fi
  else
    rm -rf "$TMPDIR"
  fi
  exit $rc
}
trap cleanup EXIT INT TERM

# ---------- build the helper ----------

log "building smoke-kiss-tcp-client helper"
HELPER_BIN="$TMPDIR/smoke-kiss-tcp-client"
(
  cd "$REPO_ROOT/graywolf"
  go build -o "$HELPER_BIN" ./cmd/smoke-kiss-tcp-client/
) || fail "helper build failed"

# ---------- start the mock KISS TCP listener ----------
#
# Python listener that binds 127.0.0.1:0, writes the selected port to
# $PEER_PORT_FILE, accepts exactly one connection, reads up to 4 KiB
# (blocking) with a 6s overall deadline, writes received bytes to
# $PEER_BYTES, then exits.

log "starting mock KISS listener on 127.0.0.1 (ephemeral port)"
python3 - "$PEER_PORT_FILE" "$PEER_BYTES" >"$LISTENER_LOG" 2>&1 <<'PY' &
import os, socket, sys, time

port_file  = sys.argv[1]
bytes_file = sys.argv[2]

srv = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
srv.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
srv.bind(("127.0.0.1", 0))
srv.listen(1)
port = srv.getsockname()[1]

# Publish the port atomically so the shell only sees a valid number.
tmp = port_file + ".tmp"
with open(tmp, "w") as f:
    f.write(str(port))
    f.flush()
    os.fsync(f.fileno())
os.replace(tmp, port_file)
print(f"listener: bound 127.0.0.1:{port}", flush=True)

srv.settimeout(6.0)
try:
    conn, addr = srv.accept()
except socket.timeout:
    print("listener: no peer connected within 6s", flush=True)
    sys.exit(1)

print(f"listener: accepted {addr}", flush=True)
conn.settimeout(3.0)

buf = bytearray()
deadline = time.time() + 3.0
while time.time() < deadline:
    try:
        chunk = conn.recv(4096)
    except socket.timeout:
        break
    if not chunk:
        break
    buf.extend(chunk)
    # Any plausible single UI frame is < 400 bytes. Don't wait around
    # once we've got something real.
    if len(buf) >= 16:
        break

with open(bytes_file, "wb") as f:
    f.write(bytes(buf))

conn.close()
srv.close()
print(f"listener: wrote {len(buf)} bytes to {bytes_file}", flush=True)
PY
LISTENER_PID=$!

# Wait briefly for the listener to publish its port.
for _ in $(seq 1 50); do
  [[ -s "$PEER_PORT_FILE" ]] && break
  sleep 0.05
done
[[ -s "$PEER_PORT_FILE" ]] || fail "listener did not publish port"
PEER_PORT="$(<"$PEER_PORT_FILE")"
log "listener ready on 127.0.0.1:$PEER_PORT"

# ---------- run the helper ----------

log "running helper: dial 127.0.0.1:$PEER_PORT and submit one frame"
set +e
"$HELPER_BIN" \
  -peer "127.0.0.1:$PEER_PORT" \
  -channel 11 \
  -src "N0CALL" \
  -dst "APRS" \
  -info "smoke $(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  -connect-timeout 5s \
  -post-write-wait 750ms \
  -v >"$HELPER_LOG" 2>&1
HELPER_RC=$?
set -e

if [[ $HELPER_RC -ne 0 ]]; then
  fail "helper exited with rc=$HELPER_RC"
fi

# Wait for the listener goroutine (the python child) to exit after
# reading; bounded because Python should exit quickly after draining.
for _ in $(seq 1 40); do
  kill -0 "$LISTENER_PID" 2>/dev/null || break
  sleep 0.05
done

# ---------- assert the bytes ----------

if [[ ! -s "$PEER_BYTES" ]]; then
  fail "mock listener received no bytes (expected a KISS frame)"
fi

# Presence of FEND (0xC0) is proof-of-life for KISS framing.
if ! python3 - "$PEER_BYTES" <<'PY'
import sys
data = open(sys.argv[1], "rb").read()
if 0xC0 not in data:
    sys.stderr.write(
        f"assertion: no KISS FEND (0xC0) byte present in {len(data)} received bytes\n"
    )
    sys.exit(1)
# Must also carry a KISS data-frame command byte 0x00 somewhere after
# the leading FEND. A UI frame goes out on port 0, cmd 0, so the first
# non-FEND byte after a FEND should be 0x00 for the frame we sent.
for i, b in enumerate(data):
    if b == 0xC0 and i + 1 < len(data):
        if data[i+1] == 0x00:
            sys.exit(0)
sys.stderr.write("assertion: FEND present but no 0x00 command byte followed it\n")
sys.exit(1)
PY
then
  fail "KISS frame assertion failed (see listener/helper logs above)"
fi

BYTES_LEN="$(python3 -c "import sys,os; print(os.path.getsize(sys.argv[1]))" "$PEER_BYTES")"
log "PASS: received $BYTES_LEN KISS bytes at the mock peer"
