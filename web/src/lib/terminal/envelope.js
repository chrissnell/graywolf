// Envelope helpers shared between the WebSocket client and the
// xterm.js viewport.
//
// Wire format mirrors pkg/ax25termws/envelope.go: { kind, ... } where
// the optional `data` field is base64 because encoding/json marshals
// []byte that way. Browsers only have atob/btoa for binary strings,
// so we bridge to Uint8Array byte arrays here so callers never see
// string-as-bytes (which silently corrupts non-ASCII).

// b64ToBytes decodes a base64 string into a Uint8Array.
export function b64ToBytes(b64) {
  if (!b64) return new Uint8Array(0);
  const bin = atob(b64);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

// bytesToB64 encodes a Uint8Array (or array of byte numbers) as base64.
export function bytesToB64(bytes) {
  if (!bytes || bytes.length === 0) return '';
  let bin = '';
  for (let i = 0; i < bytes.length; i++) bin += String.fromCharCode(bytes[i]);
  return btoa(bin);
}

// encodeData wraps a Uint8Array in a KindData envelope. Returns the
// envelope object; the caller JSON-stringifies before WebSocket send.
export function encodeData(bytes) {
  return { kind: 'data', data: bytesToB64(bytes) };
}
