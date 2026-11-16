export const PACKET_COLORS_KEY = 'graywolf-packet-colors';
export function readPacketColors(storage) {
  try { return (storage ?? globalThis.localStorage)?.getItem(PACKET_COLORS_KEY) === 'true'; }
  catch { return false; }
}
export function savePacketColors(enabled, storage) {
  try { (storage ?? globalThis.localStorage)?.setItem(PACKET_COLORS_KEY, String(enabled === true)); }
  catch { /* Device-local storage may be disabled. */ }
}
