// Preserve API flags while exposing complete transmit/receive levels.
export function serialValue(method, invert, dtrInvert) {
  return `${invert ? 1 : 0}${method === 'serial_rts_dtr' && dtrInvert ? 1 : 0}`;
}
export function serialFlags(method, value) {
  return { invert: value[0] === '1', dtr_invert: method === 'serial_rts_dtr' && value[1] === '1' };
}
export function serialOptions(method) {
  const levels = inverted => inverted ? 'TX LOW / RX HIGH' : 'TX HIGH / RX LOW';
  if (method === 'serial_rts_dtr') {
    return ['00', '01', '10', '11'].map(value => ({
      value, label: `RTS: ${levels(value[0] === '1')}; DTR: ${levels(value[1] === '1')}`,
    }));
  }
  const line = method === 'serial_dtr' ? 'DTR' : 'RTS';
  return ['00', '10'].map(value => ({ value, label: `${line}: ${levels(value[0] === '1')}` }));
}
