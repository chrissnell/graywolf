// Matches pkg/igate.ParseRFToISBlocklist. The configured source list
// accepts individual callsigns separated by any whitespace, comma or
// semicolon, including the newlines of the multi-line textarea.
export function splitRFToISBlocklist(value) {
  return (value ?? '').split(/[\s,;]+/).filter(Boolean);
}

// This is UI preflight only; backend validation remains authoritative.
export function validateRFToISBlocklist(value) {
  const calls = splitRFToISBlocklist(value);
  if (calls.length > 100) return 'Maximum 100 callsigns.';
  for (const call of calls) {
    const m = /^([A-Za-z0-9]{1,6})(?:-([0-9]{1,2}))?$/.exec(call);
    if (!m || (m[2] !== undefined && Number(m[2]) > 15)) {
      return 'Invalid callsign: ' + call;
    }
  }
  return '';
}
