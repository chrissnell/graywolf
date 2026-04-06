<script>
  import { onMount } from 'svelte';
  import { Button, Input, Select, Box } from '@chrissnell/chonky-ui';
  import { api } from '../lib/api.js';
  import PageHeader from '../components/PageHeader.svelte';

  let packets = $state([]);
  let filter = $state('');
  let dirFilter = $state('all');
  let limit = $state('100');
  let loading = $state(false);

  const dirOptions = [
    { value: 'all', label: 'All' },
    { value: 'rx', label: 'RX Only' },
    { value: 'tx', label: 'TX Only' },
  ];

  onMount(loadPackets);

  async function loadPackets() {
    loading = true;
    try {
      packets = await api.get(`/packets?limit=${limit}`) || [];
    } catch (_) { /* mock fallback */ }
    loading = false;
  }

  let filtered = $derived(() => {
    let list = packets;
    if (dirFilter !== 'all') list = list.filter((p) => p.direction === dirFilter);
    if (filter.trim()) {
      const q = filter.toLowerCase();
      list = list.filter((p) =>
        p.source.toLowerCase().includes(q) ||
        p.destination.toLowerCase().includes(q) ||
        p.raw.toLowerCase().includes(q)
      );
    }
    return list;
  });

  function formatTime(ts) {
    return new Date(ts).toLocaleString();
  }

  function exportCsv() {
    const rows = filtered().map((p) =>
      `"${p.timestamp}","${p.direction}","${p.source}","${p.destination}","${p.raw}"`
    );
    const csv = 'Timestamp,Direction,Source,Destination,Raw\n' + rows.join('\n');
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
              <th>Channel</th>
              <th>Source</th>
              <th>Destination</th>
              <th>Type</th>
              <th>Raw</th>
            </tr>
          </thead>
          <tbody>
            {#each filtered() as pkt}
              <tr>
                <td class="col-time">{formatTime(pkt.timestamp)}</td>
                <td>
                  <span class="dir-badge" class:rx={pkt.direction === 'rx'} class:tx={pkt.direction === 'tx'}>
                    {pkt.direction.toUpperCase()}
                  </span>
                </td>
                <td>{pkt.channel || '—'}</td>
                <td class="col-call">{pkt.source}</td>
                <td>{pkt.destination}</td>
                <td>{pkt.type || '—'}</td>
                <td class="col-raw">{pkt.raw}</td>
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
  .col-raw { color: var(--text-secondary); word-break: break-all; max-width: 300px; }
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
