// Local echo for the AX.25 terminal.
//
// xterm forwards every keystroke through term.onData and we ship those
// bytes straight to the BBS, but nothing renders them on our own canvas.
// Packet BBSes work in line mode and do not echo characters back, so
// without local echo the operator types blind -- the link-establish
// fix in #397 made this visible (before it, piled-up lines masked the
// missing echo). This is the terminal-side equivalent of a TNC in
// CONVERS mode echoing what you type.
//
// echoForDisplay maps an onData string to what should be written back
// to xterm so typing is visible and editable:
//   - CR / LF        -> CRLF so Enter both returns to column 0 and
//                       advances a row (matches the inbound normalizer)
//   - BS / DEL       -> "\b \b" to rub out the cell under the cursor
//   - other printables (incl. non-ASCII) -> echoed verbatim
//   - everything else (control bytes) is dropped
//
// A leading ESC means an escape sequence (arrow keys, function keys,
// bracketed paste markers): echoing its printable tail would spray
// "[C", "OP", etc. onto the canvas, so the whole string is dropped.
export function echoForDisplay(s) {
  if (!s) return '';
  if (s.charCodeAt(0) === 0x1b) return '';

  let out = '';
  for (let i = 0; i < s.length; i++) {
    const c = s.charCodeAt(i);
    if (c === 0x0d || c === 0x0a) {
      out += '\r\n';
    } else if (c === 0x08 || c === 0x7f) {
      out += '\b \b';
    } else if (c === 0x09) {
      out += '\t';
    } else if (c >= 0x20) {
      out += s[i];
    }
    // remaining control bytes (e.g. lone Ctrl-C) are not echoed
  }
  return out;
}
