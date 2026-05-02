// One AX.25 terminal session bound to a single WebSocket. The
// multi-session store (./sessions.svelte.js) owns a Map of these.
//
// State is a Svelte 5 $state object so route components can read it
// reactively. Bytes from the BBS arrive on the `onDataRX` callback
// the viewport sets after construction; everything else flows through
// reactive state fields.

import { b64ToBytes, encodeData } from './envelope.js';

const READY_STATE_OPEN = 1; // WebSocket.OPEN constant, hoisted for clarity.

function newID() {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID();
  }
  return 'sess-' + Math.random().toString(36).slice(2, 10);
}

// createSession opens a WebSocket for one operator-initiated link.
// `initial` carries the connect arguments (channelId, localCall,
// localSSID, destCall, destSSID, via, advanced timer knobs). The
// returned object exposes reactive state plus imperative controls.
export function createSession(initial, opts = {}) {
  const url = opts.url ?? `${wsScheme()}//${location.host}/api/ax25/terminal`;

  const state = $state({
    id: newID(),
    channelId: initial.channelId,
    peer: addrLabel(initial.destCall, initial.destSSID),
    local: addrLabel(initial.localCall, initial.localSSID),
    via: Array.isArray(initial.via) ? initial.via : [],
    status: 'connecting', // connecting | connected | disconnected | error
    stateName: 'DISCONNECTED',
    unreadBytes: 0,
    stats: null,
    errorMessage: null,
    suspended: false,
    focused: false,
    transcriptEnabled: false,
    // Configured retry budget (N2). Falls back to the kernel default
    // when the operator left the advanced field at zero.
    n2: pickN2(initial),
    // Viewport sets these after construction.
    onDataRX: null,
    onStateChange: null,
  });

  let ws = null;

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
      try {
        ws.send(JSON.stringify({ kind: 'connect', connect: initial }));
      } catch (err) {
        state.errorMessage = `connect send: ${String(err)}`;
        state.status = 'error';
      }
    };
    ws.onmessage = (ev) => {
      let env;
      try {
        env = JSON.parse(typeof ev.data === 'string' ? ev.data : new TextDecoder().decode(ev.data));
      } catch (err) {
        state.errorMessage = `bad envelope: ${String(err)}`;
        return;
      }
      handle(env);
    };
    ws.onerror = () => {
      state.errorMessage = state.errorMessage ?? 'WebSocket error';
    };
    ws.onclose = () => {
      // Only flip to disconnected if the link wasn't already in a
      // terminal failure state.
      if (state.status !== 'error') state.status = 'disconnected';
    };
  }

  function handle(env) {
    switch (env.kind) {
      case 'state':
        state.stateName = env.state?.name ?? state.stateName;
        if (state.stateName === 'CONNECTED') state.status = 'connected';
        else if (state.stateName === 'DISCONNECTED') state.status = 'disconnected';
        else state.status = 'connecting';
        state.onStateChange?.(state.stateName, env.state?.reason);
        break;
      case 'data_rx': {
        const bytes = b64ToBytes(env.data);
        state.onDataRX?.(bytes);
        if (!state.focused) state.unreadBytes += bytes.length;
        break;
      }
      case 'link_stats':
        state.stats = env.stats ?? null;
        break;
      case 'error':
        state.errorMessage = env.error?.message ?? 'unknown error';
        break;
      // Default: unknown kind from a future server -- ignore.
    }
  }

  function send(envObj) {
    if (!ws || ws.readyState !== READY_STATE_OPEN) return false;
    try {
      ws.send(JSON.stringify(envObj));
      return true;
    } catch (err) {
      state.errorMessage = `send: ${String(err)}`;
      return false;
    }
  }

  function sendData(bytes) {
    return send(encodeData(bytes));
  }

  function disconnect() {
    send({ kind: 'disconnect' });
  }

  function setTranscript(enabled) {
    state.transcriptEnabled = !!enabled;
    return send({ kind: 'transcript_set', transcript: { enabled: !!enabled } });
  }

  function abort() {
    send({ kind: 'abort' });
  }

  function close() {
    if (ws) {
      try { ws.close(1000, 'closed by client'); } catch { /* ignore */ }
      ws = null;
    }
  }

  function clearUnread() {
    state.unreadBytes = 0;
  }

  open();

  return { state, sendData, disconnect, abort, close, clearUnread, setTranscript };
}

// pickN2 returns the configured retry budget from `initial`, falling
// back to the kernel default (10). Accepts both snake_case (wire shape
// emitted by PreConnectForm) and camelCase to be tolerant of callers.
function pickN2(initial) {
  const v = Number(initial?.n2 ?? initial?.N2 ?? 0);
  return v > 0 ? v : 10;
}

function wsScheme() {
  return location.protocol === 'https:' ? 'wss:' : 'ws:';
}

function addrLabel(call, ssid) {
  const c = (call ?? '').toUpperCase();
  if (!c) return '';
  if (!ssid || ssid === 0) return c;
  return `${c}-${ssid}`;
}
