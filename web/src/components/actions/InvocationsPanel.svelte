<script>
  import { onMount, onDestroy } from 'svelte';
  import { Table, Badge, Button, Input, Select } from '@chrissnell/chonky-ui';
  import ConfirmDialog from '../ConfirmDialog.svelte';
  import { actionsStore } from '../../lib/actions/store.svelte.js';
  import { invocationsApi } from '../../lib/actions/api.js';

  let clearOpen = $state(false);

  const STATUS_OPTIONS = [
    { value: '', label: 'Status: any' },
    { value: 'ok', label: 'ok' },
    { value: 'bad_otp', label: 'bad_otp' },
    { value: 'bad_arg', label: 'bad_arg' },
    { value: 'denied', label: 'denied' },
    { value: 'disabled', label: 'disabled' },
    { value: 'unknown', label: 'unknown' },
    { value: 'no_credential', label: 'no_credential' },
    { value: 'busy', label: 'busy' },
    { value: 'rate_limited', label: 'rate_limited' },
    { value: 'timeout', label: 'timeout' },
    { value: 'error', label: 'error' },
  ];

  const SOURCE_OPTIONS = [
    { value: '', label: 'Source: any' },
    { value: 'rf', label: 'RF' },
    { value: 'is', label: 'IS' },
  ];

  let pollTimer = $state(null);

  function timeAgo(isoStr) {
    if (!isoStr) return '—';
    const ms = Date.now() - new Date(isoStr).getTime();
    if (Number.isNaN(ms)) return '—';
    const sec = Math.floor(ms / 1000);
    if (sec < 60) return `${sec}s ago`;
    const min = Math.floor(sec / 60);
    if (min < 60) return `${min} min ago`;
    const hr = Math.floor(min / 60);
    if (hr < 24) return `${hr}h ${min % 60}m ago`;
    const day = Math.floor(hr / 24);
    return `${day}d ago`;
  }

  function statusVariant(s) {
    switch (s) {
      case 'ok':
        return 'success';
      case 'bad_otp':
      case 'denied':
      case 'error':
      case 'timeout':
        return 'danger';
      case 'rate_limited':
      case 'busy':
      case 'disabled':
        return 'warning';
      default:
        return 'default';
    }
  }

  function sourceLabel(s) {
    if (s === 'rf') return 'RF';
    if (s === 'is') return 'IS';
    return (s || '').toUpperCase() || '—';
  }

  function argSummary(args) {
    if (!args) return '';
    const entries = Object.entries(args);
    if (entries.length === 0) return '';
    return entries.map(([k, v]) => `${k}=${v}`).join(', ');
  }

  function credName(id) {
    if (!id) return '—';
    const c = actionsStore.creds.find((x) => x.id === id);
    return c?.name ?? `#${id}`;
  }

  function detailText(inv) {
    if (inv.reply_text) return inv.reply_text;
    if (inv.status_detail) return inv.status_detail;
    if (inv.output_capture) return inv.output_capture;
    return '';
  }

  async function copy(text) {
    try {
      await navigator.clipboard.writeText(text);
    } catch {
      // best-effort; older browsers will silently no-op
    }
  }

  let actionOptions = $derived([
    { value: '', label: 'Action: any' },
    ...actionsStore.actions.map((a) => ({ value: String(a.id), label: a.name })),
  ]);

  function onFilterChange() {
    actionsStore.refreshInvocations();
  }

  async function clearLog() {
    await invocationsApi.clear();
    await actionsStore.refreshInvocations();
  }

  function startPolling() {
    stopPolling();
    pollTimer = setInterval(() => {
      if (document.visibilityState === 'visible') {
        actionsStore.refreshInvocations();
      }
    }, 5000);
  }

  function stopPolling() {
    if (pollTimer) {
      clearInterval(pollTimer);
      pollTimer = null;
    }
  }

  onMount(startPolling);
  onDestroy(stopPolling);
</script>

<section class="invocations-section">
  <div class="section-header">
    <h2 class="section-title">Recent Invocations</h2>
  </div>

  <div class="filter-bar">
    <Input
      type="text"
      placeholder="Search messages, callsigns..."
      bind:value={actionsStore.invocationFilter.q}
      oninput={onFilterChange}
    />
    <Select
      bind:value={actionsStore.invocationFilter.actionId}
      options={actionOptions}
      onValueChange={(v) => {
        actionsStore.invocationFilter.actionId = v;
        onFilterChange();
      }}
    />
    <Select
      bind:value={actionsStore.invocationFilter.status}
      options={STATUS_OPTIONS}
      onValueChange={(v) => {
        actionsStore.invocationFilter.status = v;
        onFilterChange();
      }}
    />
    <Select
      bind:value={actionsStore.invocationFilter.source}
      options={SOURCE_OPTIONS}
      onValueChange={(v) => {
        actionsStore.invocationFilter.source = v;
        onFilterChange();
      }}
    />
    <Button variant="danger" onclick={() => (clearOpen = true)}>Clear log</Button>
  </div>

  <div class="table-wrapper">
    <Table>
      <thead>
        <tr>
          <th>Time</th>
          <th>Sender</th>
          <th>Src</th>
          <th>Action</th>
          <th>Cred</th>
          <th>Result</th>
          <th>Reply / detail</th>
        </tr>
      </thead>
      <tbody>
        {#if actionsStore.invocations.length === 0}
          <tr>
            <td colspan="7" class="empty-row">No invocations match the current filters.</td>
          </tr>
        {:else}
          {#each actionsStore.invocations as inv (inv.id)}
            {@const detail = detailText(inv)}
            {@const args = argSummary(inv.args)}
            <tr>
              <td title={inv.created_at}>{timeAgo(inv.created_at)}</td>
              <td>{inv.sender_call ?? '—'}</td>
              <td><Badge variant="default">{sourceLabel(inv.source)}</Badge></td>
              <td>
                <div class="action-cell">
                  <span class="name">{inv.action_name ?? '—'}</span>
                  {#if args}<span class="args">{args}</span>{/if}
                </div>
              </td>
              <td>{credName(inv.otp_credential_id)}</td>
              <td>
                <Badge variant={statusVariant(inv.status)}>{inv.status ?? 'unknown'}</Badge>
              </td>
              <td>
                {#if detail}
                  <button
                    type="button"
                    class="detail"
                    title="Copy"
                    onclick={() => copy(detail)}
                  >
                    {detail.length > 80 ? detail.slice(0, 80) + '…' : detail}
                  </button>
                {:else}
                  <span class="muted">—</span>
                {/if}
              </td>
            </tr>
          {/each}
        {/if}
      </tbody>
    </Table>
  </div>
</section>

<ConfirmDialog
  bind:open={clearOpen}
  title="Clear invocation log?"
  message="Permanently delete every recorded invocation. This cannot be undone."
  confirmLabel="Clear log"
  onConfirm={clearLog}
/>

<style>
  .invocations-section {
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
  }
  .section-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }
  .section-title {
    font-size: 16px;
    font-weight: 600;
    margin: 0;
  }
  .filter-bar {
    display: flex;
    flex-wrap: wrap;
    gap: 0.5rem;
    align-items: center;
  }
  .filter-bar :global(input),
  .filter-bar :global(select) {
    min-width: 0;
  }
  .table-wrapper {
    overflow-x: auto;
  }
  .empty-row {
    text-align: center;
    color: var(--text-muted);
    padding: 1.25rem;
  }
  .action-cell {
    display: flex;
    flex-direction: column;
  }
  .action-cell .name {
    font-weight: 600;
  }
  .action-cell .args {
    color: var(--text-muted);
    font-size: 12px;
    font-family: ui-monospace, monospace;
  }
  .detail {
    background: none;
    border: 0;
    padding: 0;
    color: inherit;
    font: inherit;
    text-align: left;
    cursor: pointer;
    max-width: 28ch;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .detail:hover {
    text-decoration: underline;
  }
  .muted {
    color: var(--text-muted);
    font-size: 12px;
  }
</style>
