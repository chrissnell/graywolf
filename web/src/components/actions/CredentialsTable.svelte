<script>
  import { Table, Button, EmptyState } from '@chrissnell/chonky-ui';
  import ConfirmDialog from '../ConfirmDialog.svelte';
  import { actionsStore } from '../../lib/actions/store.svelte.js';
  import { credsApi } from '../../lib/actions/api.js';

  let { newCredOpen = $bindable(false) } = $props();

  let confirmOpen = $state(false);
  let pendingDelete = $state(null);

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

  function algoSummary(c) {
    const parts = [];
    parts.push('TOTP');
    if (c.algorithm) parts.push(c.algorithm.toUpperCase());
    if (c.digits) parts.push(String(c.digits));
    if (c.period) parts.push(`${c.period}s`);
    return parts.join(' / ');
  }

  function usedBySummary(used) {
    if (!used || used.length === 0) return { count: 0, label: '—', tooltip: '' };
    const label = used.slice(0, 3).join(', ') + (used.length > 3 ? `, +${used.length - 3}` : '');
    return { count: used.length, label, tooltip: used.join(', ') };
  }

  function askDelete(cred) {
    pendingDelete = cred;
    confirmOpen = true;
  }

  async function confirmDelete() {
    if (!pendingDelete?.id) return;
    await credsApi.remove(pendingDelete.id);
    pendingDelete = null;
    await actionsStore.loadAll();
  }
</script>

<section class="creds-section">
  <div class="section-header">
    <h2 class="section-title">OTP Credentials</h2>
    <Button variant="primary" onclick={() => (newCredOpen = true)}>+ New Credential</Button>
  </div>

  {#if actionsStore.creds.length === 0}
    <EmptyState class="creds-empty">
      <h3>No credentials yet</h3>
      <p>Add an authenticator-app credential, then bind it to an action that requires OTP.</p>
      <Button variant="primary" onclick={() => (newCredOpen = true)}>+ New Credential</Button>
    </EmptyState>
  {:else}
    <div class="table-wrapper">
      <Table>
        <thead>
          <tr>
            <th>Name</th>
            <th>Issuer / account</th>
            <th>Algorithm</th>
            <th>Created</th>
            <th>Last used</th>
            <th>Used by</th>
            <th class="actions-col">Action</th>
          </tr>
        </thead>
        <tbody>
          {#each actionsStore.creds as c (c.id)}
            {@const used = usedBySummary(c.used_by)}
            <tr>
              <td><span class="cred-name">{c.name}</span></td>
              <td>
                <div class="issuer-cell">
                  {#if c.issuer}<span>{c.issuer}</span>{/if}
                  {#if c.account}<span class="muted">{c.account}</span>{/if}
                  {#if !c.issuer && !c.account}<span class="muted">—</span>{/if}
                </div>
              </td>
              <td><span class="algo">{algoSummary(c)}</span></td>
              <td>{timeAgo(c.created_at)}</td>
              <td>{c.last_used_at ? timeAgo(c.last_used_at) : '—'}</td>
              <td>
                {#if used.count > 0}
                  <span title={used.tooltip}>{used.count} ({used.label})</span>
                {:else}
                  <span class="muted">unused</span>
                {/if}
              </td>
              <td class="actions-cell">
                <Button
                  size="sm"
                  variant="danger"
                  onclick={() => askDelete(c)}
                  disabled={used.count > 0}
                  title={used.count > 0 ? 'In use by an action; unbind first' : ''}
                >Delete</Button>
              </td>
            </tr>
          {/each}
        </tbody>
      </Table>
    </div>
  {/if}
</section>

<ConfirmDialog
  bind:open={confirmOpen}
  title="Delete credential?"
  message={pendingDelete
    ? `Permanently delete credential "${pendingDelete.name}"? This cannot be undone.`
    : ''}
  confirmLabel="Delete"
  onConfirm={confirmDelete}
/>

<style>
  .creds-section {
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
  }
  .section-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.5rem;
  }
  .section-title {
    font-size: 16px;
    font-weight: 600;
    margin: 0;
  }
  .table-wrapper {
    overflow-x: auto;
  }
  .cred-name {
    font-weight: 600;
  }
  .issuer-cell {
    display: flex;
    flex-direction: column;
  }
  .algo {
    font-family: ui-monospace, monospace;
    font-size: 12px;
  }
  .muted {
    color: var(--text-muted);
    font-size: 12px;
  }
  .actions-col,
  .actions-cell {
    text-align: right;
    white-space: nowrap;
  }
</style>
