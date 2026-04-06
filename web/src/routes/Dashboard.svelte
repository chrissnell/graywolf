<script>
  import { onMount } from 'svelte';
  import { api } from '../lib/api.js';
  import { Box } from '@chrissnell/chonky-ui';
  import PageHeader from '../components/PageHeader.svelte';

  let packets = $state([]);
  let stats = $state({ packets_rx: 0, packets_tx: 0, igated: 0, uptime: 0 });
  let position = $state(null);
  let audioDevices = $state([]);
  let pollTimer = $state(null);

  let hasInput = $derived(audioDevices.some(d => d.direction === 'input'));
  let hasOutput = $derived(audioDevices.some(d => d.direction === 'output'));

  onMount(() => {
    loadData();
    loadAudioDevices();
    pollTimer = setInterval(loadData, 5000);
    return () => clearInterval(pollTimer);
  });

  async function loadData() {
    try {
      const [pkts, pos, st] = await Promise.all([
        api.get('/packets?limit=20'),
        api.get('/position'),
        api.get('/channels/1/stats'),
      ]);
      packets = pkts || [];
      position = pos;
      stats = st || stats;
    } catch (_) { /* mock fallback handles it */ }
  }

  async function loadAudioDevices() {
    try {
      audioDevices = await api.get('/audio-devices') || [];
    } catch (_) { /* ignore */ }
  }

  function formatUptime(s) {
    const h = Math.floor(s / 3600);
    const m = Math.floor((s % 3600) / 60);
    return `${h}h ${m}m`;
  }

  function formatTime(ts) {
    return new Date(ts).toLocaleTimeString();
  }
</script>

<PageHeader title="Dashboard" subtitle="Live station overview" />

<div class="readiness-row">
  <div class="ready-chip" class:ok={hasInput}>
    <span class="ready-dot">{hasInput ? '●' : '○'}</span>
    <span>RX {hasInput ? 'Ready' : 'No Input'}</span>
  </div>
  <div class="ready-chip" class:ok={hasOutput}>
    <span class="ready-dot">{hasOutput ? '●' : '○'}</span>
    <span>TX Audio {hasOutput ? 'Ready' : 'No Output'}</span>
  </div>
</div>

<div class="dashboard-grid">
  <Box title="Station Stats">
    <div class="stats-grid">
      <div class="stat">
        <span class="stat-value">{stats.packets_rx}</span>
        <span class="stat-label">Packets RX</span>
      </div>
      <div class="stat">
        <span class="stat-value">{stats.packets_tx}</span>
        <span class="stat-label">Packets TX</span>
      </div>
      <div class="stat">
        <span class="stat-value">{stats.igated}</span>
        <span class="stat-label">iGated</span>
      </div>
      <div class="stat">
        <span class="stat-value">{formatUptime(stats.uptime)}</span>
        <span class="stat-label">Uptime</span>
      </div>
    </div>
  </Box>

  <Box title="DCD Status">
    <div class="dcd-row">
      <div class="dcd-indicator">
        <span class="dcd-dot dcd-idle"></span>
        <span>CH1 — VHF APRS</span>
      </div>
      <div class="dcd-indicator">
        <span class="dcd-dot dcd-idle"></span>
        <span>CH2 — 9600 Data</span>
      </div>
    </div>
  </Box>

  <Box title="Audio Levels">
    <div class="levels">
      <div class="level-row">
        <span class="level-label">CH1 RX</span>
        <div class="level-bar">
          <div class="level-fill" style="width: 35%"></div>
        </div>
        <span class="level-value">-12 dB</span>
      </div>
      <div class="level-row">
        <span class="level-label">CH1 TX</span>
        <div class="level-bar">
          <div class="level-fill level-tx" style="width: 0%"></div>
        </div>
        <span class="level-value">— dB</span>
      </div>
    </div>
  </Box>

  <Box title="GPS Position">
    {#if position}
      <div class="position-info">
        <span>{position.latitude.toFixed(4)}°N, {position.longitude.toFixed(4)}°W</span>
        <span class="pos-detail">Alt: {position.altitude}m | Fix: {position.fix} | Sats: {position.satellites}</span>
      </div>
    {:else}
      <span class="text-muted">No GPS fix</span>
    {/if}
  </Box>
</div>

<Box title="Live Packet Feed">
  <div class="packet-feed">
    {#if packets.length === 0}
      <div class="empty-feed">Waiting for packets...</div>
    {:else}
      {#each packets as pkt}
        <div class="packet-row">
          <span class="pkt-time">{formatTime(pkt.timestamp)}</span>
          <span class="pkt-dir" class:pkt-rx={pkt.direction === 'rx'} class:pkt-tx={pkt.direction === 'tx'}>
            {pkt.direction.toUpperCase()}
          </span>
          <span class="pkt-call">{pkt.source}</span>
          <span class="pkt-arrow">→</span>
          <span class="pkt-dest">{pkt.destination}</span>
          <span class="pkt-raw">{pkt.raw}</span>
        </div>
      {/each}
    {/if}
  </div>
</Box>

<style>
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
    background: var(--bg-secondary);
    border: 1px solid var(--border-color);
    color: var(--text-muted);
  }
  .ready-chip.ok {
    border-color: var(--success, #3fb950);
    color: var(--success, #3fb950);
  }
  .ready-dot {
    font-size: 10px;
  }
  .dashboard-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
    gap: 16px;
    margin-bottom: 16px;
  }
  .stats-grid {
    display: grid;
    grid-template-columns: repeat(2, 1fr);
    gap: 16px;
  }
  .stat {
    display: flex;
    flex-direction: column;
  }
  .stat-value {
    font-size: 24px;
    font-weight: 700;
    color: var(--accent);
  }
  .stat-label {
    font-size: 11px;
    color: var(--text-muted);
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }
  .dcd-row {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  .dcd-indicator {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 13px;
  }
  .dcd-dot {
    width: 10px;
    height: 10px;
    border-radius: 50%;
    display: inline-block;
  }
  .dcd-idle {
    background: var(--text-muted);
  }
  .levels {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  .level-row {
    display: flex;
    align-items: center;
    gap: 10px;
    font-size: 12px;
  }
  .level-label {
    width: 50px;
    color: var(--text-secondary);
  }
  .level-bar {
    flex: 1;
    height: 8px;
    background: var(--bg-primary);
    border-radius: 4px;
    overflow: hidden;
  }
  .level-fill {
    height: 100%;
    background: var(--success);
    border-radius: 4px;
    transition: width 0.3s;
  }
  .level-tx {
    background: var(--accent);
  }
  .level-value {
    width: 50px;
    text-align: right;
    color: var(--text-muted);
    font-size: 11px;
  }
  .position-info {
    display: flex;
    flex-direction: column;
    gap: 4px;
    font-size: 14px;
  }
  .pos-detail {
    font-size: 12px;
    color: var(--text-muted);
  }
  .packet-feed {
    max-height: 400px;
    overflow-y: auto;
    font-size: 12px;
  }
  .packet-row {
    display: flex;
    gap: 8px;
    padding: 4px 0;
    border-bottom: 1px solid var(--border-light);
    align-items: baseline;
    flex-wrap: wrap;
  }
  .pkt-time {
    color: var(--text-muted);
    min-width: 70px;
  }
  .pkt-dir {
    font-weight: 700;
    font-size: 10px;
    padding: 1px 4px;
    border-radius: 3px;
    min-width: 24px;
    text-align: center;
  }
  .pkt-rx { background: rgba(63, 185, 80, 0.2); color: var(--success); }
  .pkt-tx { background: rgba(88, 166, 255, 0.2); color: var(--accent); }
  .pkt-call { color: var(--accent); font-weight: 500; }
  .pkt-arrow { color: var(--text-muted); }
  .pkt-dest { color: var(--text-secondary); }
  .pkt-raw {
    color: var(--text-muted);
    word-break: break-all;
    flex: 1;
    min-width: 200px;
  }
  .empty-feed {
    color: var(--text-muted);
    text-align: center;
    padding: 24px;
  }
  .text-muted {
    color: var(--text-muted);
    font-size: 13px;
  }
</style>
