// Build an example "@@<otp>#<action> k=v" message string for help banners
// and modal previews. Argument insertion order follows Object.entries(),
// which is fine because operators read the example, not parse it.
export function exampleMessage({
  otp = '482910',
  action = 'TurnOnGarageLights',
  args = { state: 'on' },
} = {}) {
  const argsStr = Object.entries(args)
    .map(([k, v]) => `${k}=${v}`)
    .join(' ');
  return `@@${otp}#${action}${argsStr ? ' ' + argsStr : ''}`;
}

// Split the comma/whitespace-separated callsign allowlist string the
// backend stores into an array of trimmed callsigns. Empty input → [].
// Phase H's modal validates the same field; keep parsing in one place.
export function parseAllowlist(s) {
  if (!s) return [];
  return s
    .split(/[,\s]+/)
    .map((x) => x.trim())
    .filter(Boolean);
}
