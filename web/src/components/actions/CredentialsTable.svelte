<script>
  import { Table, Button, EmptyState, toast } from '@chrissnell/chonky-ui';
  import ConfirmDialog from '../ConfirmDialog.svelte';
  import { actionsStore } from '../../lib/actions/store.svelte.js';
  import { credsApi } from '../../lib/actions/api.js';
  import { timeAgo } from '../../lib/actions/time.js';

  let { onNew = () => {} } = $props();

  let confirmOpen = $state(false);
  let pendingDelete = $state(null);

  function algoSummary(c) {
    const parts = [];
    parts.push('TOTP');
    if (c.algorithm) parts.push(c.algorithm.toUpperCase());
    if (c.digits) parts.push(String(c.digits));
    if (c.period) parts.push(`${c.period}s`);
    return parts.join(' / ');
  }
  // Issuer/account intentionally omitted from the table per
  // single-user-station design: the issuer is always Graywolf and the
  // account is always the operator's own callsign, so the column would
  // be visual noise.

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
    const { error } = await credsApi.remove(pendingDelete.id);
    if (error) {
      toast(`Delete failed: ${error.error ?? error.message ?? error}`, 'error');
    } else {
      toast(`Deleted credential "${pendingDelete.name}".`, 'success');
    }
    pendingDelete = null;
    await actionsStore.loadAll();
  }
</script>

<section class="creds-section">
  <div class="section-header">
    <h2 class="section-title">OTP Credentials</h2>
    <Button variant="primary" class="actions-solid" onclick={onNew}>+ New Credential</Button>
  </div>

  {#if actionsStore.creds.length === 0}
    <EmptyState class="creds-empty">
      <h3>No credentials yet</h3>
      <p>Add an authenticator-app credential, then bind it to an action that requires OTP.</p>
      <Button variant="primary" class="actions-solid" onclick={onNew}>+ New Credential</Button>
    </EmptyState>
  {:else}
    <div class="table-wrapper">
      <Table>
        <thead>
          <tr>
            <th>Name</th>
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
