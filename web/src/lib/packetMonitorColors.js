import { packetFlow, packetHeaderSegments } from './packetTraffic.js';

const MAX_LINE = 1024;
function rgb(hex) {
  if (!/^#[0-9a-f]{6}$/i.test(hex)) throw new Error('Invalid packet palette color');
  return [1,3,5].map(i => parseInt(hex.slice(i,i+2),16)).join(';');
}
const fg = color => `\x1b[38;2;${rgb(color)}m`;
const bg = color => `\x1b[48;2;${rgb(color)}m`;

export function formatMonitorPacket(entry, enabled, palette) {
  const raw = String(entry.raw ?? '').slice(0,MAX_LINE);
  // Exact legacy path: no prefixes, escapes, extra whitespace or recoloring.
  if (!enabled) return raw + '\r\n';
  // Server sanitizes raw_tail. Defend the ANSI renderer too: only the escapes
  // generated here may control the terminal; packet data is never trusted ANSI.
  const safe = raw.replace(/[\x00-\x1f\x7f-\x9f]/g,'?');
  const flow = packetFlow(entry), colors = palette.flows[flow.id];
  const parts = packetHeaderSegments(safe).map(p => fg(palette[p.role] || palette.foreground) + p.text).join('');
  return bg(colors.background) + fg(colors.edge) + '▌' + fg(palette.foreground) + ` ${flow.label} ` + parts + fg(palette.foreground) + '\x1b[K\x1b[0m\r\n';
}
