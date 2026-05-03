<script>
  import { Table, Badge, Button, EmptyState, toast } from '@chrissnell/chonky-ui';
  import ConfirmDialog from '../ConfirmDialog.svelte';
  import { actionsStore } from '../../lib/actions/store.svelte.js';
  import { actionsApi } from '../../lib/actions/api.js';
  import { timeAgo } from '../../lib/actions/time.js';
  import { parseAllowlist } from '../../lib/actions/grammar.js';

  // Callbacks from parent. onEdit(null) opens the modal in "create" mode;
  // passing an action opens it for editing.
  let {
    onEdit = (_) => {},
    onTest = (_) => {},
  } = $props();

  let confirmOpen = $state(false);
  let pendingDelete = $state(null);

  function askDelete(action) {
    pendingDelete = action;
    confirmOpen = true;
  }

  async function confirmDelete() {
    if (!pendingDelete?.id) return;
    const { error } = await actionsApi.remove(pendingDelete.id);
    if (error) {
      toast(`Delete failed: ${error.error ?? error.message ?? error}`, 'error');
    } else {
      toast(`Deleted action "${pendingDelete.name}".`, 'success');
    }
    pendingDelete = null;
    await actionsStore.loadAll();
  }
</script>

<section class="actions-section">
  <h2 class="section-title">Actions</h2>

  {#if actionsStore.actions.length === 0}
    <EmptyState class="actions-empty">
      <h3>No actions yet</h3>
      <p>Define your first one to start triggering commands or webhooks from APRS messages.</p>
      <Button variant="primary" onclick={() => onEdit(null)}>+ New Action</Button>
    </EmptyState>
  {:else}
    <div class="table-wrapper">
      <Table>
        <thead>
          <tr>
            <th>Name</th>
            <th>Type</th>
            <th>OTP</th>
            <th>Sender allowlist</th>
            <th>Last fired</th>
            <th class="actions-col">Actions</th>
          </tr>
        </thead>
        <tbody>
          {#each actionsStore.actions as a (a.id)}
            {@const allowlist = parseAllowlist(a.sender_allowlist)}
            <tr>
              <td>
                <div class="name-cell">
                  <span class="action-name">{a.name}</span>
                  {#if a.description}
                    <span class="action-desc">{a.description}</span>
                  {/if}
                </div>
              </td>
              <td>
                {#if a.type === 'webhook'}
                  <Badge variant="info">webhook</Badge>
                {:else}
                  <Badge variant="default">command</Badge>
                {/if}
              </td>
              <td>
                {#if a.otp_required}
                  <Badge variant="success">REQUIRED</Badge>
                {:else}
                  <Badge variant="warning">NOT REQUIRED</Badge>
                {/if}
              </td>
              <td>
                <div class="chips">
                  {#each allowlist.slice(0, 3) as call}
                    <span class="chip">{call}</span>
                  {/each}
                  {#if allowlist.length > 3}
                    <span class="chip overflow" title={allowlist.slice(3).join(', ')}
                      >+{allowlist.length - 3}</span
                    >
                  {/if}
                  {#if allowlist.length === 0}
                    <span class="muted">any</span>
                  {/if}
                </div>
              </td>
              <td>
                {#if a.last_invoked_at}
                  <div class="last-fired">
                    <span>{timeAgo(a.last_invoked_at)}</span>
                    {#if a.last_invoked_by}
                      <span class="muted">by {a.last_invoked_by}</span>
                    {/if}
                  </div>
                {:else}
                  <span class="muted">never</span>
                {/if}
              </td>
              <td class="actions-cell">
                <Button size="sm" variant="ghost" onclick={() => onEdit(a)}>Edit</Button>
                <Button size="sm" variant="ghost" onclick={() => onTest(a)}>Test</Button>
                <Button size="sm" variant="danger" onclick={() => askDelete(a)}>Delete</Button>
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
  title="Delete action?"
  message={pendingDelete
    ? `Permanently delete "${pendingDelete.name}"? This cannot be undone.`
    : ''}
  confirmLabel="Delete"
  onConfirm={confirmDelete}
/>

<style>
  .actions-section {
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
  }
  .section-title {
    font-size: 16px;
    font-weight: 600;
    margin: 0;
  }
  .table-wrapper {
    overflow-x: auto;
  }
  .name-cell {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .action-name {
    font-weight: 600;
  }
  .action-desc {
    color: var(--text-muted);
    font-size: 12px;
  }
  .chips {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
  }
  .chip {
    background: var(--surface-2, rgba(0, 0, 0, 0.06));
    border-radius: 10px;
    padding: 1px 8px;
    font-size: 12px;
    font-family: ui-monospace, monospace;
  }
  .chip.overflow {
    cursor: help;
  }
  .last-fired {
    display: flex;
    flex-direction: column;
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
  .actions-cell :global(button) {
    margin-left: 4px;
  }
</style>
