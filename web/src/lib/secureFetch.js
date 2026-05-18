// Boot-time wrappers that inject the Android per-launch bearer token
// into every same-origin fetch and WebSocket call. Both functions are
// no-ops when the JS bridge is absent (desktop builds).
//
// We patch globalThis.fetch and globalThis.WebSocket once at boot
// rather than refactoring every call site because the SPA has 30+
// fetch sites (some via api.js, many direct fetch('/api/...')) and
// 6+ WebSocket sites scattered across stores and components. The
// wrappers only activate when androidBridge.getBearerToken() is
// non-null so desktop behavior is unchanged.
//
// Caller-supplied Authorization headers win over the auto-injected
// bearer token; this lets a deliberate per-call override (e.g. a
// rotated token during testing) bypass the wrapper without disabling
// it globally.

import { getBearerToken } from './androidBridge.js';

// Read the bridge directly each call (not the cached androidBridge.getBearerToken)
// so a renderer process that hadn't seen `GraywolfWebInterface` at install
// time still picks up the token once the bridge attaches. The bridge is
// stable for the WebView's lifetime, so a single read per fetch is cheap.
function readBearerNow() {
  try {
    const v = globalThis.GraywolfWebInterface?.getBearerToken?.();
    return (typeof v === 'string' && v.length > 0) ? v : null;
  } catch {
    return null;
  }
}

export function installSecureFetch() {
  // Wrap unconditionally. If we're on desktop (bridge never attaches) the
  // per-call readBearerNow stays null and the wrapper is transparent.
  const originalFetch = globalThis.fetch.bind(globalThis);

  globalThis.fetch = function (input, init) {
    const url = inputUrl(input);
    if (!isSameOrigin(url)) {
      return originalFetch(input, init);
    }
    const token = readBearerNow();
    if (!token) {
      return originalFetch(input, init);
    }

    // fetch(Request) -- clone the Request so we can merge headers
    // without mutating the caller's object.
    if (typeof Request !== 'undefined' && input instanceof Request) {
      if (input.headers.has('Authorization')) {
        return originalFetch(input, init);
      }
      const merged = new Headers(input.headers);
      merged.set('Authorization', `Bearer ${token}`);
      const cloned = new Request(input, { headers: merged });
      return originalFetch(cloned, init);
    }

    // fetch(string|URL, init?)
    const opts = { ...(init || {}) };
    const headers = new Headers(opts.headers || {});
    if (!headers.has('Authorization')) {
      headers.set('Authorization', `Bearer ${token}`);
    }
    opts.headers = headers;
    return originalFetch(input, opts);
  };
  // Keep the named import live so the test harness's resetBridge still
  // reaches the same module without bundlers tree-shaking it.
  void getBearerToken;
}

export function installSecureWebSocket() {
  // Same per-call bridge read pattern as installSecureFetch.
  const Original = globalThis.WebSocket;

  // Subclass via `class extends` so:
  //   - `instanceof Original` works on instances
  //   - third-party libs that subclass WebSocket continue to inherit
  //     prototype methods correctly
  //   - new.target chain is preserved across the boundary
  // Static readyState constants (CONNECTING/OPEN/CLOSING/CLOSED) are
  // inherited via the static prototype chain; do NOT re-assign them
  // -- WebView's WebSocket exposes them as read-only and assignment
  // throws "Cannot assign to read only property" in strict mode.
  class SecureWS extends Original {
    constructor(url, protocols) {
      const token = readBearerNow();
      const u = (token && isSameOrigin(url)) ? appendToken(url, token) : url;
      if (protocols !== undefined) {
        super(u, protocols);
      } else {
        super(u);
      }
    }
  }
  globalThis.WebSocket = SecureWS;
}

function inputUrl(input) {
  if (typeof input === 'string') return input;
  if (typeof URL !== 'undefined' && input instanceof URL) return input.href;
  if (input && typeof input.url === 'string') return input.url;
  return '';
}

function isSameOrigin(url) {
  if (!url) return true; // relative => same-origin
  if (typeof url === 'string' && url.startsWith('/')) return true;
  try {
    const base = (typeof location !== 'undefined' && location.href) || 'http://127.0.0.1/';
    const u = new URL(url, base);
    const origin = (typeof location !== 'undefined' && location.origin) || 'http://127.0.0.1';
    // ws:// and wss:// share an origin with http:// / https:// of the
    // same host:port for our purposes (the Go HTTP server hosts both
    // surfaces on 127.0.0.1:8080).
    if (u.protocol === 'ws:' || u.protocol === 'wss:') {
      const httpProto = u.protocol === 'wss:' ? 'https:' : 'http:';
      return `${httpProto}//${u.host}` === origin;
    }
    return u.origin === origin;
  } catch {
    return false;
  }
}

function appendToken(url, token) {
  const sep = url.includes('?') ? '&' : '?';
  return `${url}${sep}token=${encodeURIComponent(token)}`;
}
