<script>
  import { onMount, onDestroy } from 'svelte';
  import { api } from '../lib/api.js';
  import PageHeader from '../components/PageHeader.svelte';

  // Mission-control layout for the Android GPS page.
  // Reads from the same /api/gps/state endpoint the desktop page uses;
  // poll every second so the operator gets near-live values without
  // adding a new SSE/WebSocket channel for this page alone. Polling is
  // paused when the page is hidden (screen off / app backgrounded) so
  // the device doesn't burn radio + CPU on values nobody is reading.
  let fix = $state(null);
  let gnss = $state(null);
  let lastUpdated = $state(null);
  let timer = null;

  async function poll() {
    try {
      const s = await api.get('/gps/state');
      fix = s.fix ?? null;
      gnss = s.gnss_status ?? null;
      lastUpdated = Date.now();
    } catch (_) { /* ignore — keep last values */ }
  }

  function startPolling() {
    if (timer) return;
    poll();
    timer = setInterval(poll, 1000);
  }
  function stopPolling() {
    if (timer) { clearInterval(timer); timer = null; }
  }
  function onVisibility() {
    if (document.visibilityState === 'hidden') stopPolling();
    else startPolling();
  }

  onMount(() => {
    startPolling();
    document.addEventListener('visibilitychange', onVisibility);
  });
  onDestroy(() => {
    stopPolling();
    document.removeEventListener('visibilitychange', onVisibility);
  });

  // Status pill derivation: lat/lon present and fix age < 5s → LOCKED;
  // recent fix object but no lat → SEARCHING; no fix object → NO FIX.
  const FIX_FRESH_MS = 5000;
  let status = $derived.by(() => {
    if (!fix) return { label: 'NO FIX', tone: 'bad' };
    const age = lastUpdated ? Date.now() - lastUpdated : Infinity;
    if (fix.lat != null && fix.lon != null && age < FIX_FRESH_MS) {
      return { label: 'FIX LOCKED', tone: 'good' };
    }
    return { label: 'SEARCHING', tone: 'warn' };
  });

  function fmtAccuracy(m) {
    if (m == null || m === 0) return '—';
    return `${m.toFixed(1)} m`;
  }
  function fmtAgeSec(ms) {
    if (ms == null) return '—';
    return `${Math.max(0, Math.floor((Date.now() - ms) / 1000))}s ago`;
  }
  function satLabel(s) {
    const cn0 = s.cn0_dbhz?.toFixed(1) ?? '—';
    return `SVID ${s.svid} (${s.constellation}), C/N0 ${cn0} dBHz, used in fix`;
  }
</script>

<PageHeader title="GPS" subtitle="Status, satellites, position" />

<!-- role=status makes the pill a polite live region: AT users hear
     "FIX LOCKED" when the state transitions, without having to navigate
     to it. Tone is purely decorative; the label carries the meaning. -->
<section class="status-row" data-tone={status.tone} role="status" aria-live="polite">
  <div class="status-pill">{status.label}</div>
  <div class="status-meta">
    accuracy {fmtAccuracy(fix?.accuracy_m)} · updated {fmtAgeSec(lastUpdated)}
  </div>
</section>

<section class="latlon" aria-label="Position">
  <div><span class="readout-label">LATITUDE</span><span>{fix?.lat?.toFixed(5) ?? '—'}</span></div>
  <div><span class="readout-label">LONGITUDE</span><span>{fix?.lon?.toFixed(5) ?? '—'}</span></div>
</section>

<section class="trio" aria-label="Movement">
  <div><span class="readout-label">ALT</span><span>{fix?.alt_m != null ? `${fix.alt_m.toFixed(0)} m` : '—'}</span></div>
  <div><span class="readout-label">SPEED</span><span>{fix?.speed_mps != null ? `${fix.speed_mps.toFixed(1)} m/s` : '—'}</span></div>
  <div><span class="readout-label">COURSE</span><span>{fix?.course_deg != null ? `${fix.course_deg.toFixed(0)}°` : '—'}</span></div>
</section>

<section class="sats" aria-labelledby="sats-heading">
  <h2 id="sats-heading">
    SATS USED IN FIX ({gnss?.sats_used ?? 0} / {gnss?.sats_in_view ?? 0} in view)
  </h2>
  <div class="bars" role="group" aria-label="Per-satellite C/N0">
    {#each (gnss?.sats ?? []).filter(s => s.used_in_fix) as s}
      <div
        class="bar"
        role="img"
        aria-label={satLabel(s)}
        title={satLabel(s)}
        style:height={`${Math.min(100, (s.cn0_dbhz ?? 0) * 2)}%`}
      ></div>
    {/each}
  </div>
</section>

<style>
  .status-row { display: flex; align-items: center; gap: 1rem; margin-bottom: 1rem; }
  .status-pill { padding: 0.25rem 0.75rem; border-radius: 999px; font-weight: 600; color: white; }
  .status-row[data-tone="good"] .status-pill { background: var(--color-success); }
  .status-row[data-tone="warn"] .status-pill { background: var(--color-warning); }
  .status-row[data-tone="bad"]  .status-pill { background: var(--color-danger); }
  .latlon { display: grid; grid-template-columns: 1fr 1fr; gap: 1rem; margin-bottom: 1rem; }
  .latlon .readout-label, .trio .readout-label { display: block; font-size: 0.75rem; opacity: 0.6; text-transform: uppercase; letter-spacing: 0.05em; }
  .latlon span, .trio span { font-size: 1.5rem; font-family: var(--font-mono); }
  .trio { display: grid; grid-template-columns: 1fr 1fr 1fr; gap: 1rem; margin-bottom: 1rem; }
  .sats h2 { font-size: 0.75rem; opacity: 0.6; text-transform: uppercase; letter-spacing: 0.05em; font-weight: 600; margin: 0 0 0.5rem; }
  .sats .bars {
    display: flex;
    align-items: flex-end;
    gap: 4px;
    height: 80px;
    padding: 8px 0;
    overflow-x: auto;
    scroll-snap-type: x proximity;
  }
  .sats .bar {
    width: 12px;
    min-height: 4px;
    background: var(--color-primary);
    border-radius: 2px 2px 0 0;
    flex-shrink: 0;
    scroll-snap-align: start;
    cursor: help;
    outline-offset: 2px;
  }
  .sats .bar:focus { outline: 2px solid var(--color-primary); }
  @media (max-width: 480px) {
    .sats .bar { width: 8px; }
    .sats .bars { gap: 3px; }
  }
  @media (orientation: landscape) and (max-height: 480px) {
    .latlon { grid-template-columns: 1fr 1fr 1fr 1fr; }
  }
</style>
