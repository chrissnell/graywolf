// Pure form helpers for the channel create/edit modal. No Svelte
// imports; no component state. Inputs in, values out.
//
// Extracted from Channels.svelte (inline logic in openCreate, openEdit,
// validate, buildPayload) so the round-trip logic can be unit-tested
// outside a browser.

// Default blank form values, matching the $state initializer and the
// txTimingDefaults constant in Channels.svelte.
export function blankForm() {
  return {
    name: '',
    mode: 'aprs',
    channel_type: 'modem',
    input_device_id: '0', input_channel: '0',
    output_device_id: '0', output_channel: '0',
    modem_type: 'afsk', bit_rate: '1200', mark_freq: '1200', space_freq: '2200',
    tx_delay_ms: '300', tx_tail_ms: '100', slot_ms: '100', persist: '63', full_dup: false,
    enabled: true,
    fx25_encode: false,
    fx25_mode: 'auto',
  };
}

// Map a channel row (from GET /api/channels) plus a tx-timing record
// (from GET /api/tx-timing, keyed by channel id) into a form object
// ready to bind in the edit modal.
//
// timing may be undefined/null — the ?? fallbacks match the original
// txTimings[row.id] || {} pattern plus the per-field ?? defaults.
export function rowToForm(row, timing) {
  const t = timing || {};
  const channelType = row.input_device_id == null ? 'kiss-tnc' : 'modem';
  return {
    ...row,
    fx25_encode: row.fx25_encode ?? false,
    fx25_mode: row.fx25_mode || 'auto',
    mode: row.mode || 'aprs',
    channel_type: channelType,
    input_device_id: row.input_device_id == null ? '0' : String(row.input_device_id),
    input_channel: String(row.input_channel),
    output_device_id: String(row.output_device_id),
    output_channel: String(row.output_channel),
    bit_rate: String(row.bit_rate),
    mark_freq: String(row.mark_freq),
    space_freq: String(row.space_freq),
    tx_delay_ms: String(t.tx_delay_ms ?? 300),
    tx_tail_ms: String(t.tx_tail_ms ?? 100),
    slot_ms: String(t.slot_ms ?? 100),
    persist: String(t.persist ?? 63),
    full_dup: t.full_dup ?? false,
    // A row that predates the enabled flag (or any partial payload) reads
    // as enabled so an edit never silently disables a running channel.
    enabled: row.enabled ?? true,
  };
}

// Shape a bound form object into the ChannelRequest DTO for PUT/POST.
// KISS-TNC channels send input_device_id=null and zero the audio
// fields; the backend validator enforces the same invariant.
export function formToPayload(form) {
  const base = {
    name: form.name,
    mode: form.mode,
    enabled: form.enabled,
    fx25_encode: form.fx25_encode ?? false,
    fx25_mode: form.fx25_mode || 'auto',
    modem_type: form.modem_type,
    bit_rate: parseInt(form.bit_rate, 10),
    mark_freq: parseInt(form.mark_freq, 10),
    space_freq: parseInt(form.space_freq, 10),
    input_channel: parseInt(form.input_channel, 10),
    output_channel: parseInt(form.output_channel, 10),
  };
  // 'modem' / 'kiss-tnc' are the UI-only channel_type form enum, not imported
  // from channelBacking.js — kept separate to avoid conflating form state with
  // the backing.summary wire value (SUMMARY_MODEM / SUMMARY_KISS_TNC).
  if (form.channel_type === 'kiss-tnc') {
    return {
      ...base,
      input_device_id: null,
      input_channel: 0,
      output_device_id: 0,
      output_channel: 0,
    };
  }
  return {
    ...base,
    input_device_id: parseInt(form.input_device_id, 10),
    output_device_id: parseInt(form.output_device_id, 10),
  };
}

// Validate a bound form object. Returns an errors object: empty means
// valid. Mirrors the inline validate() logic in Channels.svelte.
//
// isModemType / isTxEnabled are re-derived here from form fields so
// this function is pure (no component $derived state needed).
export function validateForm(form) {
  const e = {};
  if (!form.name.trim()) e.name = 'Required';
  const isModemType = form.channel_type === 'modem';
  if (isModemType) {
    if (!form.input_device_id || form.input_device_id === '0') {
      e.input_device_id = 'Required';
    }
  }
  if (!['auto','16','32','64'].includes(form.fx25_mode || 'auto')) e.fx25_encode = 'Invalid FX.25 parity selection.';
  const fx25Supported = (Number(form.bit_rate) === 1200 && Number(form.mark_freq) === 1200 && Number(form.space_freq) === 2200) ||
    (Number(form.bit_rate) === 300 && Number(form.mark_freq) === 1600 && Number(form.space_freq) === 1800);
  if (form.fx25_encode && (!isModemType || form.modem_type !== 'afsk' ||
      !fx25Supported || form.il2p_encode)) {
    e.fx25_encode = 'FX.25 requires native AFSK: 1200 baud (1200/2200 Hz) or experimental 300 baud (1600/1800 Hz), with IL2P disabled.';
  }
  const isTxEnabled = isModemType && form.output_device_id !== '0';
  if (isTxEnabled) {
    const p = parseInt(form.persist, 10);
    if (isNaN(p) || p < 0 || p > 255) e.persist = 'Must be 0–255';
  }
  return e;
}


// Confirmation is UI-only; API callers explicitly opt in with fx25_encode=true.
export const FX25_300_WARNING = 'Experimental 300-baud FX.25 TX: testers only; not hardware/on-air validated. At 300 baud the 8-byte tag plus 16/32/64 parity bytes alone occupy about 0.64/1.07/1.92 seconds per frame. Block padding and extra framing add more; the total depends on packet length and mode. For example, a 60-byte test frame with Auto and 300/100 ms key-up/tail takes about 4.91 seconds versus 2.09 seconds as ordinary AX.25. Longer transmissions consume shared-channel capacity and may increase collisions. Prefer coordinated trials on quiet channels; leave TX off on busy channels unless the tradeoff is justified. Automatic RX itself adds no transmit airtime.';

export function experimentalFx25TxNeedsConfirmation(form, original) {
  if (!form.fx25_encode || form.channel_type !== 'modem' || Number(form.bit_rate) !== 300) return false;
  return !original?.fx25_encode || Number(original.bit_rate) !== 300 ||
    Number(original.mark_freq) !== 1600 || Number(original.space_freq) !== 1800 ||
    (original.fx25_mode || 'auto') !== (form.fx25_mode || 'auto') ||
    Number(original.output_device_id || 0) !== Number(form.output_device_id || 0);
}
