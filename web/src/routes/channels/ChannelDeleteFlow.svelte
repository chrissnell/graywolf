<script>
  import { untrack } from 'svelte';
  import { AlertDialog } from '@chrissnell/chonky-ui';
  import { api, ApiError } from '../../lib/api.js';
  import { toasts } from '../../lib/stores.js';
  import { groupReferrers, totalReferrers } from '../../lib/channelReferrers.js';

  // target is $bindable: parent sets it to a channel row to open the flow;
  // this component resets it to null on success or cancel.
  let { target = $bindable(), onDeleted } = $props();

  // Phase 5 two-step delete flow (D12).
  // Stage 1 ("impact") lists referrers grouped by type; the operator
  // chooses to cancel or proceed to stage 2. Stage 2 ("confirm")
  // requires typing the channel's exact name before the red button
  // enables. An unreferenced channel skips stage 1 and goes straight
  // to stage 2 with the same typed-name gate for consistency.
  let deleteImpactOpen = $state(false);
  let deleteConfirmOpen = $state(false);
  let deleteReferrers = $state([]);
  let deleteNameInput = $state('');
  let deleteInFlight = $state(false);
  let deleteGroups = $derived(groupReferrers(deleteReferrers));
  let deleteTotal = $derived(totalReferrers(deleteReferrers));
  let deleteNameMatches = $derived(
    target != null && deleteNameInput.trim() === target.name
  );

  // Edge-triggered open: fire openFlow ONCE per null→non-null transition of
  // target, mirroring the original one-shot imperative requestDelete call.
  // Only `target` is tracked; the row is read via untrack() so reassigning
  // target to another non-null ref mid-flow does NOT re-run openFlow.
  // Setting target=null (cancel/success) resets the edge guard so a later
  // reopen fires once again.
  let prevTargetNull = true;
  $effect(() => {
    const row = target; // subscribe to `target` only
    if (row != null && prevTargetNull) {
      prevTargetNull = false;
      untrack(() => { openFlow(row); });
    } else if (row == null) {
      prevTargetNull = true;
    }
  });

  // Phase 5 two-step delete flow (D12).
  //
  // Called when target becomes non-null:
  //   1. Fetch /api/channels/{id}/referrers.
  //   2. Empty list: skip the impact dialog; open the typed-name
  //      confirm dialog with cascade=false path.
  //   3. Non-empty list: open the impact dialog first. From there the
  //      operator clicks "Remove references…" to advance to the
  //      typed-name confirm dialog with cascade=true.
  //
  // Either way, the final Delete button is enabled only when the
  // operator types the channel's exact name. On confirm we call
  // DELETE with or without ?cascade=true depending on the path.
  async function openFlow(row) {
    deleteNameInput = '';
    deleteReferrers = [];
    try {
      const resp = await api.get(`/channels/${row.id}/referrers`);
      const refs = Array.isArray(resp?.referrers) ? resp.referrers : [];
      deleteReferrers = refs;
      if (refs.length === 0) {
        // Unreferenced — go straight to the typed-name confirm.
        deleteImpactOpen = false;
        deleteConfirmOpen = true;
      } else {
        deleteImpactOpen = true;
        deleteConfirmOpen = false;
      }
    } catch (err) {
      toasts.error(err.message);
      target = null;
    }
  }

  function proceedToConfirm() {
    deleteImpactOpen = false;
    deleteConfirmOpen = true;
    deleteNameInput = '';
  }

  function cancelDelete() {
    deleteImpactOpen = false;
    deleteConfirmOpen = false;
    target = null;
    deleteReferrers = [];
    deleteNameInput = '';
  }

  async function executeDelete() {
    if (!target || !deleteNameMatches) return;
    const cascade = deleteReferrers.length > 0;
    const id = target.id;
    deleteInFlight = true;
    try {
      const path = cascade ? `/channels/${id}?cascade=true` : `/channels/${id}`;
      await api.delete(path);
      toasts.success(cascade
        ? `Channel deleted along with ${deleteTotal} reference${deleteTotal === 1 ? '' : 's'}`
        : 'Channel deleted');
      await onDeleted?.();
      deleteImpactOpen = false;
      deleteConfirmOpen = false;
      target = null;
      deleteReferrers = [];
      deleteNameInput = '';
    } catch (err) {
      // A 409 here would mean a race (referrers appeared between our
      // GET and DELETE). Surface the same error channel; the impact
      // dialog route will naturally pick them up on the next click.
      if (err instanceof ApiError && err.status === 409 && Array.isArray(err.body?.referrers)) {
        deleteReferrers = err.body.referrers;
        deleteConfirmOpen = false;
        deleteImpactOpen = true;
        toasts.error('New references appeared — review and try again');
      } else {
        toasts.error(err.message);
      }
    } finally {
      deleteInFlight = false;
    }
  }
</script>

<!-- Phase 5 two-step delete: stage 1 = impact dialog (only when the
     channel has referrers). Lists what the cascade will do to each
     dependent row, grouped by type, so the operator has an informed
     sense of scope before hitting the typed-name gate. -->
<AlertDialog bind:open={deleteImpactOpen}>
  <AlertDialog.Content>
    <AlertDialog.Title>Delete channel {target?.name ?? ''}?</AlertDialog.Title>
    <AlertDialog.Description>
      This channel has {deleteTotal} reference{deleteTotal === 1 ? '' : 's'}. Deleting it will affect:
    </AlertDialog.Description>
    <ul class="referrer-groups">
      {#each deleteGroups as g (g.type)}
        <li>
          <strong>{g.items.length} {g.label}</strong>{#if g.action}<span class="referrer-action"> — {g.action}</span>{/if}{#if g.items.some((i) => i.name)}:
            <span class="referrer-items">
              {#each g.items as item, idx (item.id)}{idx > 0 ? ', ' : ''}{item.name || `#${item.id}`}{/each}
            </span>
          {/if}
        </li>
      {/each}
    </ul>
    <div class="modal-footer">
      <AlertDialog.Cancel onclick={cancelDelete}>Cancel</AlertDialog.Cancel>
      <AlertDialog.Action class="secondary-action" onclick={proceedToConfirm}>
        Remove references…
      </AlertDialog.Action>
    </div>
  </AlertDialog.Content>
</AlertDialog>

<!-- Phase 5 two-step delete: stage 2 = typed-name confirm. Fires for
     unreferenced channels directly (no stage 1) and for referenced
     channels after the operator clicks through the impact dialog.
     The delete button only enables when the typed name matches exactly. -->
<AlertDialog bind:open={deleteConfirmOpen}>
  <AlertDialog.Content>
    <AlertDialog.Title>
      {#if deleteReferrers.length > 0}
        Delete channel and {deleteTotal} reference{deleteTotal === 1 ? '' : 's'}
      {:else}
        Delete channel {target?.name ?? ''}?
      {/if}
    </AlertDialog.Title>
    <AlertDialog.Description>
      This cannot be undone. To confirm, type the channel name exactly:
      <strong>{target?.name ?? ''}</strong>
    </AlertDialog.Description>
    <label class="confirm-label">
      Channel name
      <input
        type="text"
        class="confirm-input"
        bind:value={deleteNameInput}
        autocomplete="off"
        aria-label={`Type ${target?.name ?? ''} to confirm delete`}
      />
    </label>
    <div class="modal-footer">
      <AlertDialog.Cancel onclick={cancelDelete}>Cancel</AlertDialog.Cancel>
      <AlertDialog.Action
        class="danger-action"
        onclick={executeDelete}
        disabled={!deleteNameMatches || deleteInFlight}
      >
        {#if deleteReferrers.length > 0}
          Delete channel and {deleteTotal} reference{deleteTotal === 1 ? '' : 's'}
        {:else}
          Delete channel
        {/if}
      </AlertDialog.Action>
    </div>
  </AlertDialog.Content>
</AlertDialog>

<style>
  /* Referrer list — scoped copy; Svelte styles are per-component.
     ChannelPutConfirm.svelte carries its own copy for the same reason. */
  .referrer-groups {
    margin: 12px 1.5rem 0 1.5rem;
    padding: 10px 12px;
    background: var(--bg-tertiary);
    border-radius: var(--radius);
    list-style: disc inside;
    font-size: 13px;
    color: var(--text-primary);
    line-height: 1.6;
  }
  .referrer-groups li + li {
    margin-top: 2px;
  }
  .referrer-action {
    color: var(--text-secondary);
    font-style: italic;
  }
  .referrer-items {
    color: var(--text-secondary);
  }

  /* modal-footer: used by both delete stages here. */
  .modal-footer {
    display: flex;
    gap: 8px;
    justify-content: flex-end;
    padding: 1.25rem 1.5rem 1.5rem;
  }

  /* :global(.danger-action) is declared in ChannelPutConfirm.svelte; being
     :global it is document-wide and reaches this component's
     AlertDialog.Action slot too. */

  /* :global(.secondary-action) — used only by the delete impact stage here;
     moved out of Channels.svelte (no remaining use in parent). Declared
     :global so it reaches the chonky-ui AlertDialog.Action slot. */
  :global(.secondary-action) {
    background: var(--bg-tertiary) !important;
    color: var(--text-primary) !important;
  }

  /* confirm-label / confirm-input used only by delete stage 2; moved here. */
  .confirm-label {
    display: block;
    margin: 12px 1.5rem 0 1.5rem;
    font-size: 13px;
    color: var(--text-secondary);
  }
  .confirm-input {
    display: block;
    width: 100%;
    margin-top: 4px;
    padding: 8px 10px;
    min-height: 40px;
    background: var(--bg-secondary);
    border: 1px solid var(--border-color);
    border-radius: var(--radius);
    color: var(--text-primary);
    font: inherit;
  }
  .confirm-input:focus-visible {
    outline: 2px solid var(--color-info, #388bfd);
    outline-offset: -2px;
  }
</style>
