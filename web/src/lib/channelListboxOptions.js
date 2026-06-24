// Pure option-model helpers for ChannelListbox. Extracted from the
// component so the none-row / selection logic is unit-testable: the
// web test runner is plain `node --test` with no Svelte render
// harness, so anything worth asserting on lives here.

// Build the listbox's unified option list: an opt-in leading "none"
// row followed by one row per channel. Channel ids start at 1, so the
// none row's sentinel (0) never collides with a real channel.
export function buildOptions(channels, allowNone) {
  const rows = (channels || []).map((c) => ({ none: false, channel: c }));
  return allowNone ? [{ none: true }, ...rows] : rows;
}

// Value emitted when the none row is committed: the unset sentinel in
// the parent's value space (0 for numeric pages like iGate, '' for
// string pages).
export function noneValueFor(valueType) {
  return valueType === 'number' ? 0 : '';
}

// Coerce a listbox value (string or number) to a comparable number,
// or null for the unset case ('' / null / undefined / non-finite).
export function asChannelNumber(v) {
  if (v === '' || v == null) return null;
  const n = typeof v === 'string' ? parseInt(v, 10) : v;
  return Number.isFinite(n) ? n : null;
}

// Index of the option matching `value` within `options`, or -1. The
// none row matches the unset value (null / 0); channel rows match on
// numeric id equality.
export function resolveCurrentIdx(options, value) {
  const num = asChannelNumber(value);
  return options.findIndex((o) =>
    o.none ? num == null || num === 0 : o.channel.id === num,
  );
}
