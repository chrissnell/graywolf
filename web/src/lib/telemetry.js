// Slot order is shared with the native reporter and its on-air PARM definition.
export const telemetryLabels = ['RF Rx', 'Digi', 'RF Tx', 'RF → IS', 'IS → RF'];
export function telemetryState(status) {
  if (!status) return 'Status unavailable';
  if (status.state === 'warming_up') return `Collecting initial data — ${Math.ceil((status.warmup_seconds || 0) / 60)} min remaining`;
  return ({ disabled: 'Disabled', waiting_for_rf_input: 'Waiting for an enabled RF input',
    waiting_for_counters: 'Waiting for valid counters', waiting_for_identity: 'Waiting for a station identity',
    waiting_for_configuration: 'Waiting for configuration', invalid_configuration: 'Invalid configuration',
    waiting_for_delivery: 'Waiting for a delivery route', persistence_error: 'Unable to save report state',
    submission_failed: 'Report submission failed', definitions_failed: 'Definition submission failed',
    definitions_submitted: 'Definitions submitted locally', submitted: 'Report submitted locally', sampling: 'Collecting station activity',
  })[status.state] || 'Status unavailable';
}
export function telemetryRows(status) {
  return telemetryLabels.map((label, i) => ({ label,
    count: status?.ready ? (status.counts?.[i] ?? '—') : '—',
    encoded: status?.ready ? (status.encoded?.[i] ?? '—') : '—',
  }));
}

export function telemetryDeliveryError(delivery, channel) {
  return delivery !== 'aprsis' && !(Number(channel) > 0)
    ? 'Select an RF delivery channel for RF-only or APRS-IS preferred delivery.' : '';
}
