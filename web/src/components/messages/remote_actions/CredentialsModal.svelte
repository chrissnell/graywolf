<script>
  // CredentialsModal -- top-level dialog over the Messages route. Lists
  // every RemoteOTPCredential with name, algorithm summary, last-used
  // timestamp, used-by count, and edit / delete buttons. Delete is
  // disabled when used_by.length > 0 with a tooltip explaining how to
  // unbind.
  //
  // Mirrors the inbound CredentialsTable.svelte pattern but the data
  // path is remoteCredsApi (separate table, separate URLs).
  import { Button, EmptyState, Modal, Tooltip, toast } from '@chrissnell/chonky-ui';
  import EditCredentialModal from './EditCredentialModal.svelte';
  import { remoteCredsApi } from '../../../lib/remote_actions/api.js';
  import { remoteActionsStore } from '../../../lib/remote_actions/store.svelte.js';
  import { timeAgo } from '../../../lib/actions/time.js';

  let { open = $bindable(false) } = $props();

  let editOpen = $state(false);
  let editTarget = $state(null);

  let prevOpen = false;
  $effect(() => {
    if (open && !prevOpen) remoteActionsStore.loadCreds();
    prevOpen = open;
  });

  function openNew() {
    editTarget = null;
    editOpen = true;
  }

  function openEdit(c) {
    editTarget = c;
    editOpen = true;
  }

  async function remove(c) {
    if ((c.used_by?.length ?? 0) > 0) return;
    const { error } = await remoteCredsApi.remove(c.id);
    if (error) {
      toast(`Delete failed: ${error.error ?? error.message ?? error}`, 'error');
      return;
    }
    toast(`Deleted "${c.name}".`, 'success');
    await remoteActionsStore.loadCreds();
  }
</script>

<Modal bind:open class="remote-creds-modal">
  <Modal.Header>
    <h3 class="modal-title">OTP Secrets</h3>
    <Modal.Close aria-label="Close">x</Modal.Close>
  </Modal.Header>
  <Modal.Body>
    <div class="header">
      <p class="hint">
        Secrets are reused by every macro that fires at the same remote station.
        Create one credential per station, named like <code>NW5W OTP</code>.
      </p>
      <Button variant="primary" onclick={openNew}>+ New Secret</Button>
    </div>

    {#if remoteActionsStore.creds.length === 0}
      <EmptyState>
        <h3>No secrets yet</h3>
        <p>Create a credential before binding it to a macro.</p>
        <Button variant="primary" onclick={openNew}>+ New Secret</Button>
      </EmptyState>
    {:else}
      <table class="creds-table">
        <thead>
          <tr>
            <th>Name</th>
            <th>Algo</th>
            <th>Last used</th>
            <th>Used by</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          {#each remoteActionsStore.creds as c (c.id)}
            {@const inUse = (c.used_by?.length ?? 0) > 0}
            <tr>
              <td><strong>{c.name}</strong></td>
              <td>TOTP / {(c.algorithm || 'sha1').toUpperCase()} / {c.digits} / {c.period}s</td>
              <td>{c.last_used_at ? timeAgo(c.last_used_at) : '--'}</td>
              <td>
                {#if inUse}
                  <Tooltip>
                    <Tooltip.Trigger>{c.used_by.length} macro{c.used_by.length === 1 ? '' : 's'}</Tooltip.Trigger>
                    <Tooltip.Content>{c.used_by.join(', ')}</Tooltip.Content>
                  </Tooltip>
                {:else}
                  --
                {/if}
              </td>
              <td class="row-actions">
                <Button variant="ghost" size="sm" onclick={() => openEdit(c)}>Edit</Button>
                {#if inUse}
                  <Tooltip>
                    <Tooltip.Trigger>
                      <Button variant="ghost" size="sm" disabled>Delete</Button>
                    </Tooltip.Trigger>
                    <Tooltip.Content>Unbind from {c.used_by.length} macro(s) first.</Tooltip.Content>
                  </Tooltip>
                {:else}
                  <Button variant="danger" size="sm" onclick={() => remove(c)}>Delete</Button>
                {/if}
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  </Modal.Body>
</Modal>

<EditCredentialModal bind:open={editOpen} cred={editTarget} onSaved={() => remoteActionsStore.loadCreds()} />

<style>
  .modal-title { margin: 0; font-size: 14px; font-weight: 600; }
  .header { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-bottom: 12px; }
  .hint { color: var(--color-text-muted); font-size: 0.875rem; max-width: 60ch; margin: 0; }
  .creds-table { width: 100%; border-collapse: collapse; font-size: 13px; }
  .creds-table th, .creds-table td {
    text-align: left;
    padding: 6px 8px;
    border-bottom: 1px solid var(--color-border);
  }
  .creds-table th {
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    color: var(--color-text-dim);
  }
  .row-actions { display: flex; gap: 6px; justify-content: flex-end; }
</style>
