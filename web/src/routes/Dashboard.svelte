<script>
  import { onMount } from 'svelte';
  import { api } from '../lib/api.js';
  import { Box, LogViewer } from '@chrissnell/chonky-ui';
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
    const [pkts, pos, st] = await Promise.allSettled([
      api.get('/packets?limit=20'),
      api.get('/position'),
      api.get('/channels/1/stats'),
    ]);
    if (pkts.status === 'fulfilled') packets = pkts.value || [];
    if (pos.status === 'fulfilled') position = pos.value;
    if (st.status === 'fulfilled' && st.value) stats = st.value;
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

  let feedEntries = $derived(packets.map(pkt => {
    const calls = parseDisplay(pkt);
    return {
      time: formatTime(pkt.timestamp),
      direction: pkt.direction || '',
      channel: pkt.channel || '—',
      source: calls.src,
      dest: calls.dst,
      info: pkt.display || '',
    };
  }));

</script>

{#snippet dirBadge(value)}
  <span class="dir-badge" class:rx={value === 'RX'} class:tx={value === 'TX'}>{value}</span>
{/snippet}

{#snippet chBadge(value)}
  <span class="ch-badge">{value}</span>
{/snippet}

{#snippet srcCall(value)}
  <span class="col-src">{value}</span>
{/snippet}

{#snippet dstCall(value)}
  <span class="col-dst">{value}</span>
{/snippet}

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
    {#if position?.latitude != null}
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
  {#if packets.length === 0}
    <div class="empty-feed">Waiting for packets...</div>
  {:else}
    <LogViewer
      entries={feedEntries}
      columns={[
        { key: 'time', label: 'Time', width: '90px' },
        { key: 'direction', label: 'Dir', width: '50px', render: dirBadge },
        { key: 'channel', label: 'Ch', width: '50px', render: chBadge },
        { key: 'source', label: 'Source', width: '100px', render: srcCall },
        { key: 'dest', label: 'Dest', width: '100px', render: dstCall },
        { key: 'info', label: 'Info', width: '1fr' },
      ]}
      live={true}
      autoscroll={true}
      height="400px"
      showHeader={true}
      class="feed-viewer"
    />
  {/if}
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
  :global(.feed-viewer) {
    font-size: 13px;
  }
  :global(.feed-viewer .log-grid-cell) {
    padding-top: 5px;
    padding-bottom: 5px;
    font-size: 13px;
  }
  :global(.feed-viewer .log-grid-header) {
    font-size: 11px;
  }
  :global(.feed-viewer .dir-badge),
  :global(.feed-viewer .ch-badge) {
    font-weight: 700;
    font-size: 10px;
    padding: 2px 5px;
    border-radius: 3px;
    display: inline-block;
    min-width: 24px;
    text-align: center;
  }
  :global(.feed-viewer .dir-badge.rx) { background: rgba(63, 185, 80, 0.2); color: var(--success); }
  :global(.feed-viewer .dir-badge.tx) { background: rgba(88, 166, 255, 0.2); color: var(--accent); }
  :global(.feed-viewer .ch-badge) {
    background: var(--bg-tertiary);
    color: var(--text-secondary);
  }
  :global(.feed-viewer .col-src) {
    color: #d4a040;
    font-weight: 500;
  }
  :global(.feed-viewer .col-dst) {
    color: #58a6ff;
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
