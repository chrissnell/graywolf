<script>
  import { onMount } from 'svelte';
  import { Button, Input, Select, Box } from '@chrissnell/chonky-ui';
  import { api } from '../lib/api.js';
  import PageHeader from '../components/PageHeader.svelte';

  let packets = $state([]);
  let filter = $state('');
  let dirFilter = $state('all');
  let limit = $state('100');
  let loading = $state(true);

  const dirOptions = [
    { value: 'all', label: 'All' },
    { value: 'rx', label: 'RX Only' },
    { value: 'tx', label: 'TX Only' },
  ];

  let pollTimer;

  onMount(() => {
    loadPackets();
    pollTimer = setInterval(loadPackets, 2000);
    return () => clearInterval(pollTimer);
  });

  async function loadPackets() {
    try {
      packets = await api.get(`/packets?limit=${limit}`) || [];
    } catch (_) { /* mock fallback */ }
    loading = false;
  }

  // Extract callsign from the display string "SRC>DEST,DIGI*:info"
  function parseDisplay(pkt) {
    const d = pkt.decoded;
    if (d) return { src: d.source || '', dst: d.dest || '' };
    const s = pkt.display || '';
    const gt = s.indexOf('>');
    if (gt < 0) return { src: '', dst: '' };
    const src = s.substring(0, gt);
    const rest = s.substring(gt + 1);
    // dest is up to the first comma or colon
    const end = rest.search(/[,:]/);
    const dst = end >= 0 ? rest.substring(0, end) : rest;
    return { src, dst };
  }

  let filtered = $derived(() => {
    let list = packets;
    if (dirFilter !== 'all') {
      const want = dirFilter.toUpperCase();
      list = list.filter((p) => (p.direction || '').toUpperCase() === want);
    }
    if (filter.trim()) {
      const q = filter.toLowerCase();
      list = list.filter((p) => {
        const { src, dst } = parseDisplay(p);
        return src.toLowerCase().includes(q) ||
          dst.toLowerCase().includes(q) ||
          (p.display || '').toLowerCase().includes(q);
      });
    }
    return list;
  });

  function formatTime(ts) {
    return new Date(ts).toLocaleString();
  }

  function exportCsv() {
    const rows = filtered().map((p) => {
      const { src, dst } = parseDisplay(p);
      return `"${p.timestamp}","${p.direction}","${src}","${dst}","${p.display || ''}"`;
    });
    const csv = 'Timestamp,Direction,Source,Destination,Display\n' + rows.join('\n');
    const blob = new Blob([csv], { type: 'text/csv' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = 'packets.csv';
    a.click();
    URL.revokeObjectURL(url);
  }
</script>

<PageHeader title="Packet Logs" subtitle="Packet log viewer with filter/search">
  <Button onclick={loadPackets} disabled={loading}>Refresh</Button>
  <Button onclick={exportCsv}>Export CSV</Button>
</PageHeader>

<Box>
  <div class="filter-bar">
    <div class="filter-input">
      <Input bind:value={filter} placeholder="Search callsign, destination, raw..." />
    </div>
    <div class="filter-select">
      <Select bind:value={dirFilter} options={dirOptions} />
    </div>
    <div class="filter-select">
      <Select bind:value={limit} options={[
        { value: '50', label: '50 packets' },
        { value: '100', label: '100 packets' },
        { value: '500', label: '500 packets' },
        { value: '1000', label: '1000 packets' },
      ]} />
    </div>
  </div>
</Box>

<div class="log-table" style="margin-top: 12px;">
  <Box>
    <div class="log-container">
      {#if loading}
        <div class="empty">Loading...</div>
      {:else if filtered().length === 0}
        <div class="empty">No packets match filter</div>
      {:else}
        <table class="pkt-table">
          <thead>
            <tr>
              <th>Time</th>
              <th>Dir</th>
              <th>Ch</th>
              <th>Source</th>
              <th>Destination</th>
              <th>Type</th>
              <th>Info</th>
            </tr>
          </thead>
          <tbody>
            {#each filtered() as pkt}
              {@const calls = parseDisplay(pkt)}
              <tr>
                <td class="col-time">{formatTime(pkt.timestamp)}</td>
                <td>
                  <span class="dir-badge" class:rx={pkt.direction === 'RX'} class:tx={pkt.direction === 'TX'}>
                    {pkt.direction}
                  </span>
                </td>
                <td>{pkt.channel || '—'}</td>
                <td class="col-call">{calls.src}</td>
                <td>{calls.dst}</td>
                <td>{pkt.type || '—'}</td>
                <td class="col-info">{pkt.display || ''}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
    </div>
    <div class="log-footer">
      Showing {filtered().length} of {packets.length} packets
    </div>
  </Box>
</div>

<style>
  .filter-bar {
    display: flex;
    gap: 10px;
    flex-wrap: wrap;
  }
  .filter-input { flex: 1; min-width: 200px; }
  .filter-select { width: 140px; }
  .log-container {
    max-height: 500px;
    overflow: auto;
    font-size: 12px;
  }
  .pkt-table {
    width: 100%;
    border-collapse: collapse;
  }
  .pkt-table th {
    background: var(--bg-tertiary);
    color: var(--text-secondary);
    text-align: left;
    padding: 8px 10px;
    font-size: 10px;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    position: sticky;
    top: 0;
    border-bottom: 1px solid var(--border-color);
  }
  .pkt-table td {
    padding: 6px 10px;
    border-bottom: 1px solid var(--border-light);
  }
  .pkt-table tr:hover td {
    background: var(--bg-secondary);
  }
  .col-time { color: var(--text-muted); white-space: nowrap; }
  .col-call { color: var(--accent); font-weight: 500; }
  .col-info { color: var(--text-secondary); word-break: break-all; max-width: 400px; font-family: var(--font-mono); }
  .dir-badge {
    font-weight: 700;
    font-size: 10px;
    padding: 1px 4px;
    border-radius: 3px;
  }
  .rx { background: rgba(63, 185, 80, 0.2); color: var(--success); }
  .tx { background: rgba(88, 166, 255, 0.2); color: var(--accent); }
  .empty { color: var(--text-muted); text-align: center; padding: 24px; }
  .log-footer {
    padding: 8px;
    font-size: 11px;
    color: var(--text-muted);
    text-align: right;
    border-top: 1px solid var(--border-light);
  }
</style>
