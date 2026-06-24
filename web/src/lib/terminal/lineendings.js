// Inbound line-ending normalizer for the AX.25 terminal.
//
// Packet-radio BBSes are wildly inconsistent about line terminators:
// FBB and many TNCs emit a bare CR, JNOS/Linux hosts emit bare LF, and
// some emit proper CRLF. xterm runs with convertEol:false (it only ever
// rewrites bare LF, never bare CR), so a bare-CR stream returns the
// cursor to column 0 without advancing a row and every line overwrites
// the last -- the "lines pile up" bug operators see.
//
// This is the browser-terminal equivalent of a cooked tty line
// discipline (stty onlcr/icrnl): collapse CR, LF, and CRLF all to CRLF
// so each line both returns to column 0 and advances a row, regardless
// of what the host sent.

const CR = 13;
const LF = 10;

// createEolNormalizer returns a stateful normalize(bytes) function.
// State is required because a CRLF pair can straddle two WebSocket
// chunks (CR ends one data_rx, LF begins the next); without carrying
// that across calls a split CRLF would render as a doubled blank line.
// Use one normalizer per session.
export function createEolNormalizer() {
  // True when the previous chunk ended on a CR we already expanded to
  // CRLF: if this chunk opens with the partner LF, swallow it so a
  // split CRLF stays a single line break.
  let swallowLeadingLF = false;

  return function normalize(bytes) {
    if (!bytes || bytes.length === 0) return bytes ?? new Uint8Array(0);

    const out = [];
    let i = 0;

    if (swallowLeadingLF) {
      swallowLeadingLF = false;
      if (bytes[0] === LF) i = 1;
    }

    for (; i < bytes.length; i++) {
      const b = bytes[i];
      if (b === CR) {
        if (i + 1 < bytes.length) {
          // In-chunk CR: skip a following LF so CRLF stays CRLF, and
          // promote a bare CR to CRLF. Either way we emit one CRLF.
          out.push(CR, LF);
          if (bytes[i + 1] === LF) i++;
        } else {
          // CR is the last byte: emit CRLF now (no rendering lag) and
          // arm the swallow so a split partner LF doesn't double up.
          out.push(CR, LF);
          swallowLeadingLF = true;
        }
      } else if (b === LF) {
        // Bare LF (any LF partnered with a CR was consumed above).
        out.push(CR, LF);
      } else {
        out.push(b);
      }
    }

    return new Uint8Array(out);
  };
}
