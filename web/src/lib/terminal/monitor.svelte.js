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
  const ts = new Date(entry.ts).toISOString().slice(11, 19);
  const src = entry.from || entry.source || '';
  const raw = (entry.raw ?? '').replace(/[\x00-\x09\x0b-\x1f\x7f]/g, '?');
  const line = `${ts} ${src} ${raw}`.slice(0, MAX_LINE);
  return line + '\r\n';
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
  let backlog = [];

  function emit(text) {
    state.lineCount += 1;
    if (state.onDataRX) {
      try { state.onDataRX(enc.encode(text)); } catch { /* viewport disposed */ }
    } else {
      // Buffer until viewport attaches its callback.
      backlog.push(text);
      if (backlog.length > 200) backlog.shift();
    }
  }

  function flushBacklog() {
    if (!state.onDataRX || backlog.length === 0) return;
    for (const line of backlog) {
      try { state.onDataRX(enc.encode(line)); } catch { /* ignore */ }
    }
    backlog = [];
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
      // Startup banner identifies what's being shown so an empty channel
      // doesn't look like a broken page.
      const banner = `[monitor] channel ${state.channelName || state.channelId} `
        + `(${state.channelMode}) -- live packets follow\r\n`;
      emit(banner);
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

  // Defer attaching the onDataRX side-effect: viewport sets
  // state.onDataRX after construction; replay any pre-mount lines.
  // A simple polling effect is overkill -- we instead expose
  // flushBacklog() and call it when sendData/setFilter is invoked,
  // and directly via this watcher.
  if (typeof window !== 'undefined') {
    queueMicrotask(() => {
      const tick = () => {
        if (state.onDataRX) {
          flushBacklog();
          return;
        }
        setTimeout(tick, 50);
      };
      tick();
    });
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
