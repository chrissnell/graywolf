<script>
  import { onMount } from 'svelte';
  import { Button } from '@chrissnell/chonky-ui';
  import { api, ApiError } from '../lib/api.js';
  import { toasts } from '../lib/stores.js';
  import { Platform } from '../lib/platform.js';
  import PageHeader from '../components/PageHeader.svelte';
  import { channelsStore, start as startChannelsStore, invalidate as refreshChannels } from '../lib/stores/channels.svelte.js';
  import ChannelRow from './channels/ChannelRow.svelte';
  import ChannelEditModal from './channels/ChannelEditModal.svelte';
  import ChannelDeleteFlow from './channels/ChannelDeleteFlow.svelte';
  import ChannelPutConfirm from './channels/ChannelPutConfirm.svelte';

  // The Channels page itself hydrates the shared store: this page is
  // the cheapest place for a first-visit operator to land, so it
  // starts the poller idempotently. Other picker pages do the same;
  // whoever mounts first wins.
  let channels = $derived(channelsStore.list);
  let audioDevices = $state([]);
  let txTimings = $state({});
  let modalOpen = $state(false);
  let editing = $state(null);

  // delete flow state — owned by ChannelDeleteFlow; parent only tracks the target.
  let deleteTarget = $state(null);

  // Phase 3 -- channel PUT 409 confirm-and-force flow. Mirrors the
  // stage-1 impact dialog above: show the list of referrers that
  // would break if the mutation proceeded, let the operator cancel
  // or confirm, and on confirm retry the PUT with ?force=true
  // (same wire convention as ?cascade=true on DELETE).
  //
  // No typed-name gate here. A PUT that breaks referrers is
  // recoverable (the operator can edit again). A DELETE is not --
  // that's why the delete flow carries the stronger gate. The
  // referrer list itself is the confirmation surface.
  let putConfirmOpen = $state(false);
  let putReferrers = $state([]);
  let putPendingPayload = $state(null);
  let putPendingId = $state(null);
  let putServerError = $state('');
  // Captured tx/ptt context from the 409 save attempt; re-used on force retry.
  let putPendingContext = $state(null);

  onMount(async () => {
    startChannelsStore();
    await Promise.all([loadChannels(), loadDevices(), loadTxTimings()]);
  });

  // Legacy name; delegates to the shared store so every caller gets
  // the same refresh semantics (including pickers on other tabs).
  async function loadChannels() {
    await refreshChannels();
  }

  async function loadDevices() {
    audioDevices = await api.get('/audio-devices') || [];
  }

  async function loadTxTimings() {
    const list = await api.get('/tx-timing') || [];
    const map = {};
    for (const t of list) map[t.channel] = t;
    txTimings = map;
  }

  function openCreate() {
    editing = null;
    modalOpen = true;
  }

  function openEdit(row) {
    editing = row;
    modalOpen = true;
  }

  function closeModal() {
    modalOpen = false;
  }

  // handleSave receives the payload + context built by ChannelEditModal
  // and delegates to persistSave (PUT/POST + referrer-confirm path).
  async function handleSave({ payload, isTxEnabled, txTiming, androidPttMethod }) {
    await persistSave(payload, { force: false, isTxEnabled, txTiming, androidPttMethod });
  }

  // persistSave runs the actual PUT/POST + follow-up tx-timing save.
  // Factored out of handleSave so the Phase 3 409-force retry path
  // can reuse it without duplicating the tx-timing / modal-close /
  // reload dance. `force` adds ?force=true to the PUT query when
  // true; backend treats that as "I know this breaks referrers,
  // proceed anyway" (Phase 1 handoff).
  // isTxEnabled, txTiming, androidPttMethod are passed from ChannelEditModal
  // via handleSave; the force-retry path (confirmForcePut) re-passes the
  // context it captured when the 409 landed.
  async function persistSave(data, { force, isTxEnabled = false, txTiming = null, androidPttMethod = null }) {
    try {
      let channelId;
      if (editing) {
        const path = force
          ? `/channels/${editing.id}?force=true`
          : `/channels/${editing.id}`;
        await api.put(path, data);
        channelId = editing.id;
        toasts.success('Channel updated');
      } else {
        const created = await api.post('/channels', data);
        channelId = created.id;
        toasts.success('Channel created');
      }

      // Save TX timing if this is a TX-capable channel
      if (isTxEnabled && channelId && txTiming) {
        const timingData = {
          channel: channelId,
          ...txTiming,
        };
        await api.put(`/tx-timing/${channelId}`, timingData);
      }

      // On Android, persist the PTT method via POST /api/ptt.
      // Data shape: method='android', gpio_pin=method_int (1–4 per Appendix B).
      // The Go modembridge reads gpio_pin to determine which USB transport
      // to invoke in UsbPttAdapter (T7). This reuses the existing PttConfig
      // row's gpio_pin field as a method-int carrier rather than adding a
      // new schema column — the semantics are different from the desktop
      // gpio-line use but the field is otherwise unused on Android channels.
      if (Platform.kind === 'android' && androidPttMethod != null && channelId) {
        await api.post('/ptt', {
          channel_id: channelId,
          method: 'android',
          gpio_pin: androidPttMethod,
        });
      }

      modalOpen = false;
      await Promise.all([loadChannels(), loadTxTimings()]);
    } catch (err) {
      // Phase 3 -- PUT 409 with referrers means the mutation would
      // break active config. Reuse the DELETE-cascade referrer-
      // grouping UI (channelReferrers.js) for consistency; the only
      // difference is the copy and the action (force vs cascade).
      // POST / non-409 paths fall through to the toast.
      if (
        editing &&
        !force &&
        err instanceof ApiError &&
        err.status === 409 &&
        Array.isArray(err.body?.referrers)
      ) {
        putReferrers = err.body.referrers;
        putPendingPayload = data;
        putPendingId = editing.id;
        putPendingContext = { isTxEnabled, txTiming, androidPttMethod };
        putServerError = err.body?.error || err.message || '';
        putConfirmOpen = true;
        return;
      }
      toasts.error(err.message);
    }
  }

  // Called from the confirm dialog's Action button when the
  // operator acknowledges the referrer list and chooses to proceed.
  async function confirmForcePut() {
    if (!putPendingPayload || !putPendingId) return;
    const data = putPendingPayload;
    try {
      // editing can get cleared by other code paths; re-affirm it
      // from the id we captured when the 409 landed so the retry
      // routes to the correct row.
      const targetId = putPendingId;
      if (editing?.id !== targetId) {
        editing = channels.find((c) => c.id === targetId) || editing;
      }
      const ctx = putPendingContext || {};
      await persistSave(data, { force: true, ...ctx });
    } finally {
      putConfirmOpen = false;
      putReferrers = [];
      putPendingPayload = null;
      putPendingId = null;
      putPendingContext = null;
      putServerError = '';
    }
  }

  // Cancel path: drop the pending payload and leave the edit modal
  // as-is. The operator's form state is preserved so they can
  // adjust the channel config and try again.
  function cancelForcePut() {
    putConfirmOpen = false;
    putReferrers = [];
    putPendingPayload = null;
    putPendingId = null;
    putPendingContext = null;
    putServerError = '';
  }

  // Opener: ChannelRow's onDelete sets deleteTarget to kick off the
  // delete flow owned entirely by ChannelDeleteFlow.
  function requestDelete(row) {
    deleteTarget = row;
  }
</script>

<PageHeader title="Channels" subtitle="Radio channel configuration">
  <Button variant="primary" onclick={openCreate}>+ Add Channel</Button>
</PageHeader>

{#if channels.length === 0}
  <div class="empty-state">
    No channels configured. Add a channel to start decoding RF packets.
    <br />
    <span class="empty-state-hint">
      Running an APRS-IS-only station? You don't need a channel — set your
      <a href="#/callsign">station callsign</a>, then enable the
      <a href="#/igate">iGate</a>. Messages will route over APRS-IS automatically.
    </span>
  </div>
{:else}
  <div class="channel-grid">
    {#each channels as ch}
      <ChannelRow
        channel={ch}
        txTiming={txTimings[ch.id]}
        {audioDevices}
        onEdit={openEdit}
        onDelete={requestDelete}
      />
    {/each}
  </div>
{/if}

<!-- Add/Edit modal (extracted to ChannelEditModal) -->
<ChannelEditModal
  bind:open={modalOpen}
  {editing}
  {audioDevices}
  {txTimings}
  onSave={handleSave}
  onCancel={closeModal}
/>

<!-- Phase 5 two-step delete flow (extracted to ChannelDeleteFlow) -->
<ChannelDeleteFlow
  bind:target={deleteTarget}
  onDeleted={async () => { await Promise.all([loadChannels(), loadTxTimings()]); }}
/>

<!-- Phase 3 -- PUT 409 force-confirm dialog (extracted to ChannelPutConfirm). -->
<ChannelPutConfirm
  bind:open={putConfirmOpen}
  referrers={putReferrers}
  serverError={putServerError}
  onConfirm={confirmForcePut}
  onCancel={cancelForcePut}
/>

<style>
  .empty-state {
    text-align: center;
    color: var(--text-muted);
    padding: 32px;
    border: 1px dashed var(--border-color);
    border-radius: var(--radius);
  }
  .empty-state-hint {
    display: inline-block;
    margin-top: 8px;
    font-size: 13px;
    color: var(--text-muted);
  }
  .empty-state-hint a {
    color: var(--color-primary);
    text-decoration: underline;
  }

  .channel-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(340px, 1fr));
    gap: 12px;
  }

  /* .modal-footer, :global(.danger-action), .referrer-groups, .referrer-items,
     .put-error-reason, .put-force-note moved to ChannelPutConfirm.svelte. */
</style>
