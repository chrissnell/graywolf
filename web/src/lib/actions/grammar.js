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
