// Local echo for the AX.25 terminal keystroke path.
//
// AX.25 connected-mode BBSes are half-duplex over radio and, by
// convention, do NOT echo the characters an operator types -- echoing
// would waste airtime. Classic hardware TNCs compensated with an
// `ECHO ON` default in converse mode so the operator could see what
// they typed. GrayWolf replaced the TNC with a software stack and the
// xterm canvas does no echo of its own, so keystrokes were invisible
// until (and unless) the remote host echoed them back. This restores
// the TNC's local-echo behavior.
//
// echoFor(s) maps a chunk emitted by xterm's onData into the string to
// write back to the same terminal for display. The chunk sent to the
// host is never altered -- only what the operator sees locally.

// localEcho returns the display string for a keystroke chunk `s`, or ''
// when nothing should be echoed.
//
//   CR / LF      -> CRLF      (advance a row, matching the inbound
//                              normalizer so typed Enter behaves like
//                              received line breaks)
//   BS / DEL     -> "\b \b"   (rub out the previous glyph)
//   TAB          -> TAB       (let xterm advance to the next tab stop)
//   printable    -> as-is     (>= 0x20, includes multi-byte UTF-8)
//   other ctrl   -> ''        (dropped)
//
// A chunk containing ESC (0x1b) is an editor/cursor key sequence
// (arrows, Home/End, function keys); echoing it would paint literal
// "[A" garbage, so the whole chunk is skipped. Pasted command text does
// not carry ESC, so paste still echoes.
export function localEcho(s) {
  if (!s) return '';
  if (s.indexOf('\x1b') !== -1) return '';

  let out = '';
  for (const ch of s) {
    const code = ch.codePointAt(0);
    if (ch === '\r' || ch === '\n') out += '\r\n';
    else if (ch === '\x7f' || ch === '\b') out += '\b \b';
    else if (ch === '\t') out += '\t';
    else if (code >= 0x20) out += ch;
    // else: drop other C0 control characters.
  }
  return out;
}
