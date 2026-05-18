<script>
  import { AlertDialog } from '@chrissnell/chonky-ui';
  import { groupReferrers, totalReferrers } from '../../lib/channelReferrers.js';

  // open is $bindable so the parent can drive open/close via putConfirmOpen.
  // referrers is the raw 409 referrer array; groups/total are derived here.
  // serverError is the human-readable error string from the 409 body (may be '').
  // onConfirm: parent runs confirmForcePut (force-retry persistSave + putPendingContext).
  // onCancel: parent runs cancelForcePut (clears pending state, closes dialog).
  //
  // No $effect needed: this dialog is purely controlled by bind:open + props.
  // The parent owns all orchestration (persistSave, 409 detection, putPendingContext
  // for the force-retry); this component owns only the confirm dialog markup
  // and the in-flight display state that gates the Action button.
  let { open = $bindable(), referrers, serverError = '', onConfirm, onCancel } = $props();

  // Derived inside the component; parent passes raw referrers array.
  let groups = $derived(groupReferrers(referrers));
  let total = $derived(totalReferrers(referrers));

  // Local in-flight flag: set true while the force-retry round-trip is in
  // progress so the Action button is disabled and the operator can't
  // double-submit. Reset in the finally block of handleConfirm below; the
  // parent no longer tracks PUT in-flight state.
  let inFlight = $state(false);

  async function handleConfirm() {
    inFlight = true;
    try {
      await onConfirm?.();
    } finally {
      inFlight = false;
    }
  }
</script>

<!-- Phase 3 -- channel PUT 409 "force" confirmation. Mirrors the
     stage-1 delete impact dialog (same AlertDialog shape, same
     groupReferrers() rendering) but the Action retries the PUT with
     ?force=true instead of cascading a delete. No typed-name gate:
     a broken-referrer PUT is recoverable by editing again. -->
<AlertDialog bind:open>
  <AlertDialog.Content>
    <AlertDialog.Title>Update channel and break references?</AlertDialog.Title>
    <AlertDialog.Description>
      This channel update would break the following active config.
      {#if serverError}
        <span class="put-error-reason">Reason: {serverError}</span>
      {/if}
    </AlertDialog.Description>
    <ul class="referrer-groups">
      {#each groups as g (g.type)}
        <li>
          <strong>{g.items.length} {g.label}</strong>{#if g.items.some((i) => i.name)}:
            <span class="referrer-items">
              {#each g.items as item, idx (item.id)}{idx > 0 ? ', ' : ''}{item.name || `#${item.id}`}{/each}
            </span>
          {/if}
        </li>
      {/each}
    </ul>
    <p class="put-force-note">
      Saving will apply the change anyway. The referrers listed above
      will remain in the database but may fail to transmit until you
      fix them on their respective pages.
    </p>
    <div class="modal-footer">
      <AlertDialog.Cancel onclick={onCancel}>Cancel</AlertDialog.Cancel>
      <AlertDialog.Action
        class="danger-action"
        onclick={handleConfirm}
        disabled={inFlight}
      >
        Save channel and break {total} reference{total === 1 ? '' : 's'}
      </AlertDialog.Action>
    </div>
  </AlertDialog.Content>
</AlertDialog>

<style>
  /* Referrer list — scoped copy; Svelte styles are per-component.
     ChannelDeleteFlow.svelte carries its own copy for the same reason. */
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
  .referrer-items {
    color: var(--text-secondary);
  }

  .modal-footer {
    display: flex;
    gap: 8px;
    justify-content: flex-end;
    padding: 1.25rem 1.5rem 1.5rem;
  }

  /* :global(.danger-action) moved here from Channels.svelte; after B7 the
     parent has no more dialogs so this is the sole declaration. Being :global
     it still reaches the chonky-ui AlertDialog.Action slot regardless of
     component scope. */
  :global(.danger-action) {
    background: var(--color-danger) !important;
    color: white !important;
  }

  /* Phase 3 inline "Reason:" clause: shows the server's concrete explanation
     (e.g. "no output device configured") so the operator sees why the
     mutation breaks referrers without guessing. */
  .put-error-reason {
    display: block;
    margin-top: 6px;
    font-size: 13px;
    color: var(--color-danger, #f85149);
  }
  .put-force-note {
    margin: 12px 1.5rem 0 1.5rem;
    font-size: 13px;
    color: var(--text-secondary);
    line-height: 1.5;
  }
</style>
