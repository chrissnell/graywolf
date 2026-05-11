import { test, beforeEach, afterEach } from 'node:test';
import assert from 'node:assert/strict';
import { installSecureFetch, installSecureWebSocket } from './secureFetch.js';
import { _resetForTests as resetBridge } from './androidBridge.js';

// node:test runs on bare node; isSameOrigin defaults to
// http://127.0.0.1/ when location is absent so same-origin checks
// reproduce the Android WebView's runtime behavior.

let originalFetch;
let originalWS;

beforeEach(() => {
  resetBridge();
  delete globalThis.GraywolfWebInterface;
  originalFetch = globalThis.fetch;
  originalWS = globalThis.WebSocket;
});
afterEach(() => {
  resetBridge();
  delete globalThis.GraywolfWebInterface;
  globalThis.fetch = originalFetch;
  globalThis.WebSocket = originalWS;
});

// ---------- installSecureFetch ----------

test('installSecureFetch wraps fetch even when bridge absent (transparent pass-through)', async () => {
  // Bridge attaches AFTER bootstrap on some WebViews; wrapping
  // unconditionally + reading the token per-call lets late-arriving
  // bridge state still inject the Bearer header. With no bridge,
  // the wrapped fetch must behave as a pass-through (no header set).
  let capturedHeaders;
  globalThis.fetch = (_input, opts) => {
    capturedHeaders = new Headers(opts?.headers);
    return Promise.resolve(new Response('{}'));
  };
  installSecureFetch();
  await globalThis.fetch('/api/version');
  assert.equal(capturedHeaders.get('Authorization'), null);
});

test('installSecureFetch picks up a late-arriving bridge', async () => {
  // Bridge absent at install time; attaches before first fetch.
  // Stub the originalFetch BEFORE installing so the wrapper closes
  // over the stub. Then attach the bridge and call the wrapped fetch.
  let capturedHeaders;
  globalThis.fetch = (_input, opts) => {
    capturedHeaders = new Headers(opts?.headers);
    return Promise.resolve(new Response('{}'));
  };
  installSecureFetch();
  globalThis.GraywolfWebInterface = { getBearerToken: () => 'tok-late' };
  await globalThis.fetch('/api/version');
  assert.equal(capturedHeaders.get('Authorization'), 'Bearer tok-late');
});

test('installSecureFetch wraps fetch and adds Authorization header', async () => {
  globalThis.GraywolfWebInterface = { getBearerToken: () => 'tok-abc' };
  let capturedHeaders;
  globalThis.fetch = (input, opts) => {
    capturedHeaders = new Headers(opts?.headers);
    return Promise.resolve(new Response('{}'));
  };
  installSecureFetch();
  await globalThis.fetch('/api/version');
  assert.equal(capturedHeaders.get('Authorization'), 'Bearer tok-abc');
});

test('installSecureFetch does not add header to cross-origin URLs', async () => {
  globalThis.GraywolfWebInterface = { getBearerToken: () => 'tok-abc' };
  let capturedHeaders;
  globalThis.fetch = (input, opts) => {
    capturedHeaders = new Headers(opts?.headers);
    return Promise.resolve(new Response('{}'));
  };
  installSecureFetch();
  await globalThis.fetch('https://example.com/x');
  assert.equal(capturedHeaders.get('Authorization'), null);
});

test('installSecureFetch handles fetch(new Request(...)) by cloning with merged headers', async () => {
  globalThis.GraywolfWebInterface = { getBearerToken: () => 'tok-abc' };
  let capturedRequest;
  globalThis.fetch = (input) => {
    capturedRequest = input;
    return Promise.resolve(new Response('{}'));
  };
  installSecureFetch();
  const req = new Request('http://127.0.0.1/api/version', { method: 'POST' });
  await globalThis.fetch(req);
  assert.ok(capturedRequest instanceof Request);
  assert.equal(capturedRequest.headers.get('Authorization'), 'Bearer tok-abc');
  assert.equal(capturedRequest.method, 'POST');
});

test('installSecureFetch preserves caller-supplied Authorization header (caller wins)', async () => {
  globalThis.GraywolfWebInterface = { getBearerToken: () => 'tok-abc' };
  let capturedHeaders;
  globalThis.fetch = (input, opts) => {
    capturedHeaders = new Headers(opts?.headers);
    return Promise.resolve(new Response('{}'));
  };
  installSecureFetch();
  await globalThis.fetch('/api/version', { headers: { Authorization: 'Bearer caller-set' } });
  assert.equal(capturedHeaders.get('Authorization'), 'Bearer caller-set');
});

// ---------- installSecureWebSocket ----------

test('installSecureWebSocket wraps WebSocket even when bridge absent (pass-through)', () => {
  // Same rationale as installSecureFetch: wrap unconditionally so a
  // late-arriving bridge can still drive the per-call token read.
  const captured = [];
  class FakeWS {
    constructor(url, protocols) { captured.push({ url, protocols }); }
  }
  globalThis.WebSocket = FakeWS;
  installSecureWebSocket();
  /* eslint-disable-next-line no-new */
  new globalThis.WebSocket('/ws/x');
  assert.equal(captured.length, 1);
  // No token in the URL when bridge absent.
  assert.equal(captured[0].url, '/ws/x');
});

test('installSecureWebSocket appends ?token to same-origin URL', () => {
  globalThis.GraywolfWebInterface = { getBearerToken: () => 'tok-abc' };
  const captured = [];
  // Stub WebSocket as a class so `class extends WebSocket` works.
  class FakeWS {
    constructor(url, protocols) {
      captured.push({ url, protocols });
      this.readyState = 1;
    }
    close() {}
  }
  FakeWS.CONNECTING = 0; FakeWS.OPEN = 1; FakeWS.CLOSING = 2; FakeWS.CLOSED = 3;
  globalThis.WebSocket = FakeWS;
  installSecureWebSocket();
  const ws = new globalThis.WebSocket('ws://127.0.0.1/ws/foo');
  assert.match(captured[0].url, /token=tok-abc/);
  assert.ok(ws instanceof FakeWS);
});

test('installSecureWebSocket preserves an existing query string', () => {
  globalThis.GraywolfWebInterface = { getBearerToken: () => 'tok-abc' };
  const captured = [];
  class FakeWS {
    constructor(url) { captured.push(url); }
    close() {}
  }
  FakeWS.CONNECTING = 0; FakeWS.OPEN = 1; FakeWS.CLOSING = 2; FakeWS.CLOSED = 3;
  globalThis.WebSocket = FakeWS;
  installSecureWebSocket();
  new globalThis.WebSocket('ws://127.0.0.1/ws/foo?bar=1');
  assert.match(captured[0], /[?&]bar=1/);
  assert.match(captured[0], /[?&]token=tok-abc/);
});

test('installSecureWebSocket passes protocols through unchanged', () => {
  globalThis.GraywolfWebInterface = { getBearerToken: () => 'tok-abc' };
  const captured = [];
  class FakeWS {
    constructor(url, protocols) { captured.push({ url, protocols }); }
    close() {}
  }
  FakeWS.CONNECTING = 0; FakeWS.OPEN = 1; FakeWS.CLOSING = 2; FakeWS.CLOSED = 3;
  globalThis.WebSocket = FakeWS;
  installSecureWebSocket();
  new globalThis.WebSocket('ws://127.0.0.1/ws/foo', ['graywolf.v1']);
  assert.deepEqual(captured[0].protocols, ['graywolf.v1']);
});

test('installSecureWebSocket does not append token to cross-origin WS URLs', () => {
  globalThis.GraywolfWebInterface = { getBearerToken: () => 'tok-abc' };
  const captured = [];
  class FakeWS {
    constructor(url) { captured.push(url); }
    close() {}
  }
  FakeWS.CONNECTING = 0; FakeWS.OPEN = 1; FakeWS.CLOSING = 2; FakeWS.CLOSED = 3;
  globalThis.WebSocket = FakeWS;
  installSecureWebSocket();
  new globalThis.WebSocket('ws://example.com/ws/foo');
  assert.doesNotMatch(captured[0], /token=/);
});
