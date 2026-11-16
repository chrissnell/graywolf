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

import { packetPalette } from '../packetTraffic.js';
import { formatMonitorPacket } from '../packetMonitorColors.js';

const READY_STATE_OPEN = 1;
const HISTORY_LIMIT = 500;

function newID() {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID();
  }
  return 'mon-' + Math.random().toString(36).slice(2, 10);
}

function wsScheme() {
  return location.protocol === 'https:' ? 'wss:' : 'ws:';
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
  let colorEnabled = false;
  let history = [];
  let themeObserver = null;
  let contrastQuery = null;

  function palette() {
    const theme = typeof document !== 'undefined' ? document.documentElement.dataset.theme || 'graywolf' : 'graywolf';
    return packetPalette(theme, contrastQuery?.matches || false);
  }

  function repaint() {
    if (!state.onDataRX) return;
    try {
      state.onDataRX(enc.encode('\x1b[0m\x1b[2J\x1b[H'));
      const colors = palette();
      for (const entry of history) state.onDataRX(enc.encode(formatMonitorPacket(entry,colorEnabled,colors)));
    } catch { /* viewport disposed */ }
  }

  function setPacketColors(enabled) {
    const next = enabled === true;
    if (colorEnabled === next) return;
    colorEnabled = next;
    repaint();
  }

  function themeChanged() { if (colorEnabled) repaint(); }
  if (typeof MutationObserver !== 'undefined' && typeof document !== 'undefined') {
    themeObserver = new MutationObserver(themeChanged);
    themeObserver.observe(document.documentElement,{attributes:true,attributeFilter:['data-theme']});
  }
  if (typeof window !== 'undefined' && typeof window.matchMedia === 'function') {
    contrastQuery = window.matchMedia('(prefers-contrast: more)');
    contrastQuery.addEventListener?.('change',themeChanged);
  }

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
        // Bound repaint history by both count and per-entry text size.
        const entry = { raw: String(env.raw_tail.raw ?? '').slice(0,1024),
          direction: String(env.raw_tail.direction ?? '').slice(0,16), source: String(env.raw_tail.source ?? '').slice(0,64), notes: String(env.raw_tail.notes ?? '').slice(0,64) };
        history.push(entry);
        if (history.length > HISTORY_LIMIT) history.shift();
        emit(formatMonitorPacket(entry,colorEnabled,palette()));
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
    history = [];
    if (!state.onDataRX) return;
    try { state.onDataRX(enc.encode('\x1b[2J\x1b[H')); } catch { /* ignore */ }
  }

  function close() {
    themeObserver?.disconnect();
    contrastQuery?.removeEventListener?.('change',themeChanged);
    history = [];
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
    setPacketColors,
    repaint,
    sendData,
    setFilter,
    clearScreen,
    close,
  };
}
