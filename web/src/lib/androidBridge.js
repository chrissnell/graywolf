// Single source of truth for the per-launch bearer token injected by
// the Android Service via WebView.addJavascriptInterface(...,
// "GraywolfWebInterface"). Returns null on desktop builds so the
// bearer wiring becomes a no-op there.
//
// The token is cached at first read because the injected JS bridge
// object is stable for the WebView's lifetime; calling the JNI
// getBearerToken on every fetch would cross the JS<->Java boundary
// needlessly.

let cached; // sentinel: undefined = not read; null = absent; string = token

export function getBearerToken() {
  if (cached !== undefined) return cached;
  try {
    const v = globalThis.GraywolfWebInterface?.getBearerToken?.();
    cached = (typeof v === 'string' && v.length > 0) ? v : null;
  } catch {
    cached = null;
  }
  return cached;
}

// Test-only: reset the cache between unit tests. Not part of the
// public surface; do not call from app code.
export function _resetForTests() {
  cached = undefined;
}
