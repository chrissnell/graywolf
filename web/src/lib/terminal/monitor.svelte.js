// Monitor-mode session for APRS-only and APRS+packet channels. Opens
// its own /api/ax25/terminal WebSocket, sends raw_tail_subscribe, and
// re-emits each raw_tail envelope as a TNC2 line that an xterm.js
// viewport can render. Conforms to the same shape that
// TerminalViewport expects from a real LAPB session: a state object
// with `onDataRX(Uint8Array)`, plus an outbound `sendData()` (no-op
// here -- monitor mode is read-only).
//
// Filter and saved-filter persistence live with the consuming view;
// this module just owns the WebSocket and the byte stream.

const READY_STATE_OPEN = 1;
const MAX_LINE = 1024;

function newID() {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID();
  }
  return 'mon-' + Math.random().toString(36).slice(2, 10);
}

function wsScheme() {
  return location.protocol === 'https:' ? 'wss:' : 'ws:';
}

function fmt(entry) {
  // Server already formats raw_tail entries as TNC2-style monitor
  // lines and sanitizes non-printables. Client just frames each line
  // with CRLF -- no timestamp, no extra prefix, just the packet, like
  // a packet BBS monitor would show it.
  const raw = (entry.raw ?? '').slice(0, MAX_LINE);
  return raw + '\r\n';
}

export function createMonitorSession({ channel, initialFilter = '' } = {}) {
  const url = `${wsScheme()}//${location.host}/api/ax25/terminal`;

  const state = $state({
    id: newID(),
    kind: 'monitor',
    channelId: channel?.id ?? 0,
    channelName: channel?.name ?? '',
    channelMode: channel?.mode ?? 'aprs',
    peer: 'monitor',
    stateName: 'MONITOR',
    status: 'connecting', // connecting | open | closed | error
    filter: initialFilter,
    appliedFilter: '',
    lineCount: 0,
    errorMessage: null,
    onDataRX: null,
    onStateChange: null,
  });

  let ws = null;
  let enc = new TextEncoder();

  function emit(text) {
    state.lineCount += 1;
    if (!state.onDataRX) return;
    try { state.onDataRX(enc.encode(text)); } catch { /* viewport disposed */ }
  }

  function sendSubscribe() {
    if (!ws || ws.readyState !== READY_STATE_OPEN) return;
    const args = { channel_id: state.channelId };
    const f = state.filter.trim();
    if (f) args.substring = f;
    state.appliedFilter = f;
    try {
      ws.send(JSON.stringify({ kind: 'raw_tail_subscribe', raw_tail_sub: args }));
    } catch (err) {
      state.errorMessage = String(err);
    }
  }

  function open() {
    try {
      ws = new WebSocket(url);
    } catch (err) {
      state.status = 'error';
      state.errorMessage = String(err);
      return;
    }
    ws.binaryType = 'arraybuffer';
    ws.onopen = () => {
      state.status = 'open';
      sendSubscribe();
    };
    ws.onmessage = (ev) => {
      let env;
      try {
        env = JSON.parse(typeof ev.data === 'string' ? ev.data : new TextDecoder().decode(ev.data));
      } catch {
        return;
      }
      if (env.kind === 'raw_tail' && env.raw_tail) {
        emit(fmt(env.raw_tail));
      } else if (env.kind === 'error' && env.error) {
        state.errorMessage = env.error.message ?? env.error.code ?? 'monitor error';
      }
    };
    ws.onerror = () => { state.status = 'error'; };
    ws.onclose = () => { state.status = 'closed'; };
  }

  function setFilter(text) {
    state.filter = text ?? '';
    sendSubscribe();
  }

  function clearScreen() {
    if (!state.onDataRX) return;
    try { state.onDataRX(enc.encode('\x1b[2J\x1b[H')); } catch { /* ignore */ }
  }

  function close() {
    try { ws?.close(1000, 'monitor closed'); } catch { /* ignore */ }
    ws = null;
  }

  // sendData is a no-op: monitor mode is read-only. Operator keystrokes
  // would otherwise loop into the WS without any peer to receive them.
  function sendData(_bytes) {
    // intentionally empty
  }

  open();

  return {
    state,
    sendData,
    setFilter,
    clearScreen,
    close,
  };
}
