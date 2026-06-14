// Resolve root-relative style URLs to absolute.
//
// The backend (pkg/mapsstyle) rewrites upstream maps.nw5w.com URLs to
// root-relative /api/maps/style/... paths. MapLibre only resolves relative
// URLs when a style is loaded from a URL string; we hand it the style as an
// *object* (so federated mode can swap sources), and in that mode v5 rejects
// relative sprite/glyphs ("Invalid sprite URL ... must be absolute"). Resolve
// the root-relative paths to absolute against the current origin (the same
// host that serves /api/maps/style) before MapLibre sees the spec.
//
// Mutates `style` in place and returns it. Callers must pass a style they own
// (a fresh clone or freshly built object), never a shared/cached spec.
//
// The backend always emits root-absolute paths ("/api/..."), so prefixing the
// origin is enough -- and unlike `new URL()` it leaves the {fontstack}/{range}
// glyph placeholders intact instead of percent-encoding them.
export function absolutizeStyleUrls(style) {
  if (typeof window === 'undefined' || !style) return style;
  const abs = (u) =>
    typeof u === 'string' && u.startsWith('/')
      ? window.location.origin + u
      : u;
  if (typeof style.sprite === 'string') {
    style.sprite = abs(style.sprite);
  } else if (Array.isArray(style.sprite)) {
    style.sprite = style.sprite.map((s) =>
      s && typeof s.url === 'string' ? { ...s, url: abs(s.url) } : s,
    );
  }
  if (typeof style.glyphs === 'string') style.glyphs = abs(style.glyphs);
  if (style.sources) {
    for (const src of Object.values(style.sources)) {
      if (src && typeof src.url === 'string') src.url = abs(src.url);
    }
  }
  return style;
}
