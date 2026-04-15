<script>
  import { onMount } from 'svelte';
  import { Button, Box, Dot } from '@chrissnell/chonky-ui';
  import { api } from '../lib/api.js';
  import PageHeader from '../components/PageHeader.svelte';

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

  // --- Packet feed helpers (matching Logs page exactly) ---

  function originTag(pkt) {
    const src = pkt.source || '';
    const notes = pkt.notes || '';
    switch (src) {
      case 'digipeater': return { label: 'Digipeater', cls: 'digi' };
      case 'beacon':     return { label: 'Beacon',     cls: 'bcn' };
      case 'igate':
        if (notes === 'is2rf') return { label: 'iGate IS\u2192RF', cls: 'igate-is2rf' };
        if (notes === 'rf2is') return { label: 'iGate RF\u2192IS', cls: 'igate-rf2is' };
        return { label: 'iGate', cls: 'igate' };
      default: return null;
    }
  }

  function parseDisplay(pkt) {
    const d = pkt.decoded;
    if (d?.source) return { src: d.source, dst: d.dest || '' };
    const s = pkt.display || '';
    const gt = s.indexOf('>');
    if (gt < 0) return { src: '', dst: '' };
    const src = s.substring(0, gt);
    const rest = s.substring(gt + 1);
    const end = rest.search(/[,:]/);
    const dst = end >= 0 ? rest.substring(0, end) : rest;
    return { src, dst };
  }

  function deviceLabel(pkt) {
    const dev = pkt.device;
    if (!dev) return '';
    if (dev.vendor && dev.model) return `${dev.vendor} ${dev.model}`;
    return dev.model || dev.vendor || '';
  }

  function distanceLabel(pkt) {
    if (pkt.distance_mi == null) return '';
    const d = pkt.distance_mi;
    const label = d < 1 ? `${(d * 5280).toFixed(0)} ft` : `${d.toFixed(1)} mi`;
    return pkt.via ? `${label} via ${pkt.via}` : label;
  }

  function formatTime(ts) {
    const d = new Date(ts);
    const mo = d.getMonth() + 1;
    const day = d.getDate();
    const h = d.getHours().toString().padStart(2, '0');
    const m = d.getMinutes().toString().padStart(2, '0');
    const s = d.getSeconds().toString().padStart(2, '0');
    return `${mo}/${day} ${h}:${m}:${s}`;
  }

  // Auto-scroll for packet feed
  let feedEl = $state();
  let isAtBottom = $state(true);

  function checkAtBottom() {
    if (!feedEl) return;
    isAtBottom = feedEl.scrollHeight - feedEl.scrollTop - feedEl.clientHeight < 2;
  }

  function scrollToBottom() {
    if (feedEl) {
      feedEl.scrollTop = feedEl.scrollHeight;
      isAtBottom = true;
    }
  }

  $effect(() => {
    void packets;
    if (isAtBottom && feedEl) {
      requestAnimationFrame(() => {
        if (feedEl) feedEl.scrollTop = feedEl.scrollHeight;
      });
    }
  });
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
        {#if position.has_alt} &middot; {position.alt_m?.toFixed(0)}m{/if}
        {#if position.has_course} &middot; {position.heading_deg?.toFixed(0)}&deg; &middot; {position.speed_kt?.toFixed(1)}kt{/if}
      </span>
    {:else}
      <span class="stat-value" style="color: var(--color-text-dim);">&mdash;</span>
      <span class="stat-label">GPS &middot; No Fix</span>
    {/if}
  </div>
</div>

<!-- Live Packet Feed (Logs-style) -->
<div class="feed-section">
  {#if packets.length === 0}
    <Box><div class="empty">Waiting for packets...</div></Box>
  {:else}
    <div class="log-panel">
      <div class="log-toolbar">
        <Dot on={true} />
        <span class="tb-label">live</span>
        <span class="tb-count">{packets.length} entries</span>
      </div>

      <div class="log-scroll" bind:this={feedEl} onscroll={checkAtBottom}>
        <div class="pkt-hdr">
          <span class="c-time">Time</span>
          <span class="c-dir">Direction</span>
          <span class="c-origin">Origin</span>
          <span class="c-ch">Ch</span>
          <span class="c-src">Source</span>
          <span class="c-dst">Dest</span>
          <span class="c-type">Type</span>
          <span class="c-device">Device</span>
          <span class="c-dist">Distance</span>
        </div>

        {#each packets as pkt, i}
          {@const calls = parseDisplay(pkt)}
          {@const origin = originTag(pkt)}
          {@const device = deviceLabel(pkt)}
          {@const dist = distanceLabel(pkt)}
          {@const dir = (pkt.direction || '').toUpperCase()}
          <div class="pkt-entry" class:pkt-alt={i % 2 === 1}>
            <div class="pkt-row">
              <span class="c-time">{formatTime(pkt.timestamp)}</span>
              <span class="c-dir">
                <span class="badge" class:b-rx={dir === 'RX'} class:b-tx={dir === 'TX'} class:b-is={dir === 'IS'}>
                  {dir === 'RX' ? 'Received' : dir === 'TX' ? 'Transmitted' : dir === 'IS' ? 'APRS-IS' : dir}
                </span>
              </span>
              <span class="c-origin">
                {#if origin}
                  <span class="badge o-{origin.cls}">{origin.label}</span>
                {:else}
                  <span class="dim">&mdash;</span>
                {/if}
              </span>
              <span class="c-ch">{pkt.channel ?? '\u2014'}</span>
              <span class="c-src val-src">{calls.src}</span>
              <span class="c-dst val-dst">{calls.dst}</span>
              <span class="c-type">
                {#if pkt.type}<span class="badge b-type">{pkt.type}</span>{:else}<span class="dim">&mdash;</span>{/if}
              </span>
              <span class="c-device dim">{device || '\u2014'}</span>
              <span class="c-dist">
                {#if dist}<span class="val-dist">{dist}</span>{:else}<span class="dim">&mdash;</span>{/if}
              </span>
            </div>
            <div class="pkt-raw">{pkt.display || ''}</div>
          </div>
        {/each}
      </div>

      {#if !isAtBottom}
        <button class="jump-btn" onclick={scrollToBottom}>&darr; Jump to bottom</button>
      {/if}
    </div>
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

  /* ── packet feed (matching Logs page) ─────────── */
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

  .log-panel {
    position: relative;
    border: 1px solid var(--color-border);
    border-radius: var(--radius);
    background: var(--color-bg);
    overflow: hidden;
  }

  .log-toolbar {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 7px 14px;
    border-bottom: 1px solid var(--color-border);
    background: var(--color-surface);
  }
  .tb-label {
    font-size: var(--text-xs);
    color: var(--color-text-muted);
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }
  .tb-count {
    margin-left: auto;
    font-size: var(--text-xs);
    color: var(--color-text-dim);
  }

  .log-scroll {
    height: 400px;
    overflow-y: auto;
    overflow-x: auto;
  }

  .jump-btn {
    position: absolute;
    bottom: 10px;
    left: 50%;
    transform: translateX(-50%);
    padding: 5px 14px;
    font-size: var(--text-xs);
    border: 1px solid var(--color-border);
    border-radius: var(--radius);
    background: var(--color-surface-raised);
    color: var(--color-text-muted);
    cursor: pointer;
    z-index: 2;
    box-shadow: 0 2px 8px rgba(0,0,0,0.4);
  }
  .jump-btn:hover { background: var(--color-btn-bg-hover); }

  /* ── column widths (shared header + rows) ─────── */
  .c-time   { width: 100px; flex-shrink: 0; }
  .c-dir    { width: 100px; flex-shrink: 0; }
  .c-origin { width: 115px; flex-shrink: 0; }
  .c-ch     { width: 30px;  flex-shrink: 0; text-align: center; }
  .c-src    { width: 100px; flex-shrink: 0; }
  .c-dst    { width: 80px;  flex-shrink: 0; }
  .c-type   { width: 85px;  flex-shrink: 0; }
  .c-device { flex: 1 1 0;  min-width: 80px; }
  .c-dist   { width: 175px; flex-shrink: 0; text-align: right; }

  /* ── sticky header ────────────────────────────── */
  .pkt-hdr {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 12px;
    position: sticky;
    top: 0;
    z-index: 1;
    background: var(--color-bg);
    border-bottom: 1px solid var(--color-border);
    font-size: var(--text-xs);
    font-weight: 500;
    color: var(--color-text-dim);
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }

  /* ── entry block ──────────────────────────────── */
  .pkt-entry {
    border-bottom: 1px solid var(--color-border-subtle);
  }
  .pkt-alt {
    background: var(--color-surface);
  }

  .pkt-row {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 5px 12px 2px;
    font-size: var(--text-sm);
  }

  .pkt-raw {
    padding: 0 12px 5px 12px;
    font-size: 0.65rem;
    color: var(--color-text-dim);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    line-height: 1.5;
  }

  /* ── badges ───────────────────────────────────── */
  .badge {
    display: inline-block;
    font-weight: 700;
    font-size: 10px;
    padding: 2px 6px;
    border-radius: 3px;
    white-space: nowrap;
    text-align: center;
    line-height: 1.4;
  }

  /* Direction badges */
  .b-rx { background: var(--color-success-muted); color: var(--color-success); }
  .b-tx { background: var(--color-warning-muted); color: var(--color-warning); }
  .b-is { background: var(--color-info-muted);    color: var(--color-info); }

  /* Origin badges */
  .o-digi        { background: var(--color-info-muted);    color: var(--color-info); }
  .o-bcn         { background: var(--color-success-muted); color: var(--color-success); }
  .o-igate       { background: var(--color-info-muted);    color: var(--color-info); }
  .o-igate-is2rf { background: var(--color-warning-muted); color: var(--color-warning); }
  .o-igate-rf2is { background: var(--color-info-muted);    color: var(--color-info); }

  /* Type badge */
  .b-type {
    background: var(--color-surface-raised);
    color: var(--color-text-muted);
    font-weight: 500;
  }

  /* ── value colors ─────────────────────────────── */
  .val-src {
    color: var(--color-warning);
    font-weight: 600;
  }
  .val-dst {
    color: var(--color-info);
  }
  .val-dist {
    font-size: var(--text-xs);
    color: var(--color-success);
  }
  .dim {
    color: var(--color-text-dim);
  }
</style>
