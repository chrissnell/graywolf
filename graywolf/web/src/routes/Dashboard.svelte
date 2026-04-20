<script>
  import { onMount } from 'svelte';
  import { Button, Box } from '@chrissnell/chonky-ui';
  import { api } from '../lib/api.js';
  import { formatAltitude, formatSpeed } from '../lib/settings/units.js';
  import PageHeader from '../components/PageHeader.svelte';
  import PacketLogViewer from '../components/PacketLogViewer.svelte';

  let packets = $state([]);
  let status = $state(null);
  let position = $state(null);
  let beacons = $state([]);
  let audioDevices = $state([]);
  let pollTimer = $state(null);

  let hasInput = $derived(audioDevices.some(d => d.direction === 'input'));
  let hasOutput = $derived(audioDevices.some(d => d.direction === 'output'));

  let totalRx = $derived(status?.channels?.reduce((sum, ch) => sum + (ch.rx_frames || 0), 0) ?? 0);
  let totalTx = $derived(status?.channels?.reduce((sum, ch) => sum + (ch.tx_frames || 0), 0) ?? 0);
  let igated = $derived(status?.igate?.rf_to_is_gated ?? 0);

  // Activity tracking for RX/TX flash indicators
  let prevStats = {};
  let rxActive = $state({});
  let txActive = $state({});
  let sendingBeacon = $state({});

  // Group enabled beacons by channel
  let beaconsByChannel = $derived(
    beacons.reduce((acc, b) => {
      if (b.enabled) {
        if (!acc[b.channel]) acc[b.channel] = [];
        acc[b.channel].push(b);
      }
      return acc;
    }, {})
  );

  onMount(() => {
    loadData();
    loadBeacons();
    loadAudioDevices();
    pollTimer = setInterval(loadData, 5000);
    return () => clearInterval(pollTimer);
  });

  async function loadData() {
    const [pkts, pos, st] = await Promise.allSettled([
      api.get('/packets?limit=20'),
      api.get('/position'),
      api.get('/status'),
    ]);
    if (pkts.status === 'fulfilled') packets = pkts.value || [];
    if (pos.status === 'fulfilled') position = pos.value;
    if (st.status === 'fulfilled' && st.value) {
      // Track RX/TX activity changes for flash indicators
      if (st.value.channels) {
        for (const ch of st.value.channels) {
          const prev = prevStats[ch.id];
          if (prev) {
            if (ch.rx_frames > prev.rx_frames) {
              rxActive = { ...rxActive, [ch.id]: true };
              const id = ch.id;
              setTimeout(() => { rxActive = { ...rxActive, [id]: false }; }, 3000);
            }
            if (ch.tx_frames > prev.tx_frames) {
              txActive = { ...txActive, [ch.id]: true };
              const id = ch.id;
              setTimeout(() => { txActive = { ...txActive, [id]: false }; }, 3000);
            }
          }
          prevStats[ch.id] = { rx_frames: ch.rx_frames, tx_frames: ch.tx_frames };
        }
      }
      status = st.value;
    }
  }

  async function loadBeacons() {
    try { beacons = await api.get('/beacons') || []; } catch (_) {}
  }

  async function loadAudioDevices() {
    try { audioDevices = await api.get('/audio-devices') || []; } catch (_) {}
  }

  async function sendBeaconNow(beaconId) {
    sendingBeacon[beaconId] = true;
    try {
      await api.post(`/beacons/${beaconId}/send`);
      // Flash TX indicator immediately for the beacon's channel
      const bcn = beacons.find(b => b.id === beaconId);
      if (bcn) {
        txActive = { ...txActive, [bcn.channel]: true };
        setTimeout(() => { txActive = { ...txActive, [bcn.channel]: false }; }, 3000);
      }
      // Re-poll status after a short delay to catch the tx_frames increment
      setTimeout(loadData, 1500);
    } catch (_) {}
    setTimeout(() => { sendingBeacon = { ...sendingBeacon, [beaconId]: false }; }, 2000);
  }

  function formatUptime(s) {
    if (!s && s !== 0) return '\u2014';
    const h = Math.floor(s / 3600);
    const m = Math.floor((s % 3600) / 60);
    return `${h}h ${m}m`;
  }

  function peakToPercent(peak) {
    if (peak == null) return 0;
    const clamped = Math.max(-60, Math.min(0, peak));
    return ((clamped + 60) / 60) * 100;
  }

  function levelColor(dbfs) {
    if (dbfs == null) return 'var(--color-text-dim)';
    if (dbfs > -6) return 'var(--color-danger, #f85149)';
    if (dbfs > -20) return 'var(--color-warning, #d29922)';
    return 'var(--color-success, #3fb950)';
  }

  function formatPeak(peak) {
    if (peak == null) return '\u2014';
    return `${peak.toFixed(0)} dBFS`;
  }

  function formatCoord(val, posChar, negChar) {
    if (val == null) return '\u2014';
    const abs = Math.abs(val);
    const dir = val >= 0 ? posChar : negChar;
    return `${abs.toFixed(4)}\u00B0${dir}`;
  }

  // Packet-feed helpers and auto-scroll live inside PacketLogViewer / Chonky
  // LogViewer now — nothing to wire up here beyond passing `packets` through.
</script>

<PageHeader title="Dashboard" subtitle="Live station overview" />

<div class="readiness-row">
  <div class="ready-chip" class:ok={hasInput}>
    <span class="ready-dot">{hasInput ? '\u25CF' : '\u25CB'}</span>
    <span>RX {hasInput ? 'Ready' : 'No Input'}</span>
  </div>
  <div class="ready-chip" class:ok={hasOutput}>
    <span class="ready-dot">{hasOutput ? '\u25CF' : '\u25CB'}</span>
    <span>TX Audio {hasOutput ? 'Ready' : 'No Output'}</span>
  </div>
</div>

<!-- Channel Cards -->
<div class="channel-grid">
  {#if status?.channels?.length}
    {#each status.channels as ch}
      {@const channelBeacons = beaconsByChannel[ch.id] || []}
      {@const audioPeak = ch.device_peak_dbfs || ch.audio_peak}
      <div class="ch-card">
        <div class="ch-header">
          <span class="ch-title">CH{ch.id}: {ch.name}</span>
          <span class="ch-modem">{ch.modem_type.toUpperCase()} {ch.bit_rate} bd</span>
        </div>

        <div class="ch-indicators">
          <span class="indicator" class:active={ch.dcd_state}>
            <span class="ind-dot dcd"></span> DCD
          </span>
          <span class="indicator" class:active={rxActive[ch.id]}>
            <span class="ind-dot rx"></span> RX
          </span>
          <span class="indicator" class:active={txActive[ch.id]}>
            <span class="ind-dot tx"></span> TX
          </span>
        </div>

        <div class="ch-audio">
          <div class="level-bar">
            <div class="level-fill" style="width: {peakToPercent(audioPeak)}%; background: {levelColor(audioPeak)}"></div>
          </div>
          <span class="level-value">{formatPeak(audioPeak)}</span>
        </div>

        <div class="ch-stats">
          <span>RX: <strong>{ch.rx_frames || 0}</strong></span>
          <span>TX: <strong>{ch.tx_frames || 0}</strong></span>
        </div>

        {#if channelBeacons.length > 0}
          <div class="ch-beacons">
            {#each channelBeacons as bcn}
              <Button
                variant="primary"
                onclick={() => sendBeaconNow(bcn.id)}
                disabled={sendingBeacon[bcn.id]}
              >
                {sendingBeacon[bcn.id] ? 'Sent!' : `Beacon Now: ${bcn.callsign}`}
              </Button>
            {/each}
          </div>
        {/if}
      </div>
    {/each}
  {:else}
    <div class="ch-card"><span class="text-muted">No channels configured</span></div>
  {/if}
</div>

<!-- Station Summary -->
<div class="stats-grid">
  <div class="stat-card">
    <span class="stat-value">{totalRx}</span>
    <span class="stat-label">Packets RX</span>
  </div>
  <div class="stat-card">
    <span class="stat-value">{totalTx}</span>
    <span class="stat-label">Packets TX</span>
  </div>
  <div class="stat-card">
    <span class="stat-value">{igated}</span>
    <span class="stat-label">iGated</span>
  </div>
  <div class="stat-card">
    <span class="stat-value">{formatUptime(status?.uptime_seconds)}</span>
    <span class="stat-label">Uptime</span>
  </div>
  <div class="stat-card gps-card">
    {#if position?.valid}
      <span class="stat-value gps-value">{formatCoord(position.lat, 'N', 'S')}, {formatCoord(position.lon, 'E', 'W')}</span>
      <span class="stat-label">
        {position.source === 'gps' ? 'GPS' : 'Fixed Position'}
        {#if position.has_alt} &middot; {formatAltitude(position.alt_m)}{/if}
        {#if position.has_course} &middot; {position.heading_deg?.toFixed(0)}&deg; &middot; {formatSpeed(position.speed_kt)}{/if}
      </span>
    {:else}
      <span class="stat-value" style="color: var(--color-text-dim);">&mdash;</span>
      <span class="stat-label">GPS &middot; No Fix</span>
    {/if}
  </div>
</div>

<!-- Live Packet Feed -->
<div class="feed-section">
  {#if packets.length === 0}
    <Box><div class="empty">Waiting for packets...</div></Box>
  {:else}
    <PacketLogViewer
      {packets}
      height="400px"
      live
      showHeader
      mobileBreakpoint="768px"
    />
  {/if}
</div>

<style>
  /* ── readiness row ────────────────────────────── */
  .readiness-row {
    display: flex;
    gap: 10px;
    margin-bottom: 16px;
    flex-wrap: wrap;
  }
  .ready-chip {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 6px 14px;
    font-size: 12px;
    font-weight: 600;
    border-radius: 999px;
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    color: var(--color-text-muted);
  }
  .ready-chip.ok {
    border-color: var(--color-success);
    color: var(--color-success);
  }
  .ready-dot { font-size: 10px; }

  /* ── channel cards ────────────────────────────── */
  .channel-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(300px, 1fr));
    gap: 16px;
    margin-bottom: 16px;
  }
  .ch-card {
    border: 1px solid var(--color-border);
    border-radius: var(--radius);
    background: var(--color-bg);
    padding: 16px;
    display: flex;
    flex-direction: column;
    gap: 12px;
  }
  .ch-header {
    display: flex;
    justify-content: space-between;
    align-items: baseline;
  }
  .ch-title {
    font-size: 15px;
    font-weight: 700;
    color: var(--color-text);
  }
  .ch-modem {
    font-size: var(--text-xs);
    color: var(--color-text-dim);
    letter-spacing: 0.03em;
  }

  /* ── DCD / RX / TX indicators ─────────────────── */
  .ch-indicators {
    display: flex;
    gap: 16px;
  }
  .indicator {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: var(--text-xs);
    font-weight: 600;
    color: var(--color-text-dim);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
  .ind-dot {
    width: 10px;
    height: 10px;
    border-radius: 50%;
    background: var(--color-text-dim);
    transition: background 0.2s, box-shadow 0.2s;
  }
  .indicator.active .ind-dot.dcd {
    background: var(--color-success);
    box-shadow: 0 0 8px var(--color-success);
  }
  .indicator.active .ind-dot.rx {
    background: var(--color-success);
    box-shadow: 0 0 8px var(--color-success);
  }
  .indicator.active .ind-dot.tx {
    background: var(--color-warning);
    box-shadow: 0 0 8px var(--color-warning);
  }
  .indicator.active {
    color: var(--color-text);
  }

  /* ── audio level bar ──────────────────────────── */
  .ch-audio {
    display: flex;
    align-items: center;
    gap: 10px;
  }
  .level-bar {
    flex: 1;
    height: 8px;
    background: var(--color-surface);
    border-radius: 4px;
    overflow: hidden;
  }
  .level-fill {
    height: 100%;
    border-radius: 4px;
    transition: width 0.15s ease-out, background 0.15s;
  }
  .level-value {
    font-size: var(--text-xs);
    color: var(--color-text-dim);
    white-space: nowrap;
    min-width: 55px;
    text-align: right;
  }

  /* ── channel stats ────────────────────────────── */
  .ch-stats {
    display: flex;
    gap: 20px;
    font-size: var(--text-sm);
    color: var(--color-text-muted);
  }
  .ch-stats strong {
    color: var(--color-text);
  }

  /* ── beacon buttons ───────────────────────────── */
  .ch-beacons {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
  }

  /* ── station stats cards ────────────────────────── */
  .stats-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(160px, 1fr));
    gap: 12px;
    margin-bottom: 16px;
  }
  .stat-card {
    border: 1px solid var(--color-border);
    border-radius: var(--radius);
    background: var(--color-bg);
    padding: 16px;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 4px;
  }
  .stat-card.gps-card {
    grid-column: span 2;
  }
  .stat-value {
    font-size: 28px;
    font-weight: 700;
    color: var(--color-text);
  }
  .stat-value.gps-value {
    font-size: 18px;
  }
  .stat-label {
    font-size: var(--text-xs);
    color: var(--color-text-dim);
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }

  /* ── packet feed wrapper ───────────────────────── */
  .feed-section {
    margin-top: 16px;
  }
  .empty {
    color: var(--color-text-dim);
    text-align: center;
    padding: 24px;
  }
  .text-muted {
    color: var(--color-text-dim);
    font-size: 13px;
  }
</style>
