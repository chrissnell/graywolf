// Platform detection. Returns 'android' when running inside the
// Android WebView (signalled by the GraywolfWebInterface JS bridge),
// otherwise 'desktop'. Used by Svelte routes/components that gate
// surfaces on host platform.
//
// Read via Platform.kind. The kind getter is dynamic — it consults
// the bridge on every access — so test setups that toggle
// globalThis.GraywolfWebInterface between cases observe the change
// without re-importing.
//
// Companion modules: pkg/platform.Kind (Go) and Platform.KIND (Kotlin).

function detectKind() {
  // Bypass androidBridge's cache — Platform.kind is documented as dynamic
  // so test setups that toggle globalThis.GraywolfWebInterface observe
  // the change without re-importing.
  try {
    const v = globalThis.GraywolfWebInterface?.getBearerToken?.();
    return (typeof v === 'string' && v.length > 0) ? 'android' : 'desktop';
  } catch {
    return 'desktop';
  }
}

export const Platform = {
  get kind() { return detectKind(); },
};
