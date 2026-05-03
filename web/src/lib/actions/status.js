// Map an action invocation status (from pkg/actions/types.go) to a
// chonky-ui Badge variant. Shared so InvocationsPanel and the
// TestActionDialog result panel use identical color rules.
export function statusVariant(s) {
  switch (s) {
    case 'ok':
      return 'success';
    case 'bad_otp':
    case 'bad_arg':
    case 'denied':
    case 'no_credential':
    case 'error':
    case 'timeout':
      return 'danger';
    case 'rate_limited':
    case 'busy':
    case 'disabled':
      return 'warning';
    default:
      return 'default';
  }
}

// Pull the offending arg key out of a "bad arg: <key>" reply. Returns
// '' when the message doesn't match the format. The runner's reply
// uses the human-readable form; the audit row's status enum is the
// codename "bad_arg".
export function badArgKey(replyText) {
  if (!replyText || typeof replyText !== 'string') return '';
  const m = replyText.match(/^bad arg:\s*([A-Za-z0-9_-]+)\s*$/);
  return m ? m[1] : '';
}
