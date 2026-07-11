<script>
  import { onMount } from 'svelte';
  import { Toggle, Box, Select, Input, Button, Icon } from '@chrissnell/chonky-ui';
  import { messagesPreferencesState } from '../lib/settings/messages-preferences-store.svelte.js';
  import { channelsStore, start as startChannels } from '../lib/stores/channels.svelte.js';
  import {
    getMessagesConfig, putMessagesConfig,
    listBlocklist, createBlocklistEntry, updateBlocklistEntry, deleteBlocklistEntry,
  } from '../api/messages.js';
  import { toasts } from '../lib/stores.js';
  import PageHeader from '../components/PageHeader.svelte';

  const fallbackPolicyOptions = [
    { value: 'is_fallback', label: 'Try RF first, fall back to APRS-IS' },
    { value: 'is_only', label: 'APRS-IS only' },
    { value: 'rf_only', label: 'RF only' },
    { value: 'both', label: 'Send on RF and APRS-IS' },
  ];

  let txChannel = $state(0);

  // --- Blocked call signs ---
  /** @type {Array<any>} */
  let blocklist = $state([]);
  let newCall = $state('');
  let newNote = $state('');
  let blockError = $state('');
  let addingBlock = $state(false);

  onMount(async () => {
    messagesPreferencesState.fetchPreferences();
    startChannels();
    const cfg = await getMessagesConfig().catch(() => null);
    txChannel = cfg?.tx_channel ?? 0;
    await refreshBlocklist();
  });

  let txChannelOptions = $derived([
    { value: 0, label: 'Auto (first APRS-eligible channel)' },
    ...channelsStore.list
      .filter((c) => c.mode !== 'packet')
      .map((c) => ({ value: c.id, label: c.name })),
  ]);

  async function handleTxChannelChange(v) {
    const next = Number(v);
    txChannel = next;
    try {
      await putMessagesConfig({ tx_channel: next });
    } catch {
      const cfg = await getMessagesConfig().catch(() => null);
      txChannel = cfg?.tx_channel ?? 0;
    }
  }

  async function refreshBlocklist() {
    try {
      const rows = await listBlocklist();
      blocklist = rows || [];
    } catch {
      // non-fatal
    }
  }

  function onNewCallInput(e) {
    newCall = (e.target.value || '').toUpperCase();
    if (e.target.value !== newCall) e.target.value = newCall;
    blockError = '';
  }

  function validateCall(call) {
    if (!call) return 'Call sign is required.';
    if (!/^[A-Z0-9-]{1,9}$/.test(call)) return 'Invalid format — up to 9 characters: A-Z, 0-9, -.';
    const dup = blocklist.find((r) => (r.callsign || '').toUpperCase() === call);
    if (dup) return `${call} is already blocked.`;
    return '';
  }

  async function addBlock() {
    const call = (newCall || '').trim().toUpperCase();
    const err = validateCall(call);
    if (err) { blockError = err; return; }
    addingBlock = true;
    try {
      await createBlocklistEntry({ callsign: call, note: (newNote || '').trim(), enabled: true });
      toasts.success(`Blocked ${call}`);
      newCall = '';
      newNote = '';
      await refreshBlocklist();
    } catch (e) {
      blockError = e?.message || 'Could not block call sign.';
    } finally {
      addingBlock = false;
    }
  }

  async function toggleBlock(row, next) {
    try {
      await updateBlocklistEntry(row.id, { callsign: row.callsign, note: row.note || '', enabled: next });
      await refreshBlocklist();
    } catch (e) {
      toasts.error(e?.message || 'Update failed');
    }
  }

  async function removeBlock(row) {
    try {
      await deleteBlocklistEntry(row.id);
      toasts.success(`Unblocked ${row.callsign}`);
      await refreshBlocklist();
    } catch (e) {
      toasts.error(e?.message || 'Delete failed');
    }
  }
</script>

<PageHeader title="Messaging" subtitle="APRS message sending options" />

<Box title="Messages">
  <Toggle
    checked={messagesPreferencesState.allowLong}
    onCheckedChange={(v) => messagesPreferencesState.setAllowLong(v)}
    label="Allow long APRS messages"
    disabled={!messagesPreferencesState.loaded || messagesPreferencesState.saving}
  />
  <p class="messages-hint">
    Lets you send messages up to 200 characters. Some receivers cannot
    decode longer messages and will truncate or drop them. Leave off
    unless you know your contacts support it.
  </p>
  <p class="tx-channel-label">Transmit channel</p>
  <Select
    value={txChannel}
    onValueChange={handleTxChannelChange}
    options={txChannelOptions}
    aria-label="Messages transmit channel"
  />
  <p class="messages-hint">
    Where graywolf sends outbound APRS messages. Auto picks the first
    APRS-eligible channel at send time.
  </p>
  <p class="tx-channel-label">Send path</p>
  <Select
    value={messagesPreferencesState.fallbackPolicy}
    onValueChange={(v) => messagesPreferencesState.setFallbackPolicy(v)}
    options={fallbackPolicyOptions}
    aria-label="Message send path"
    disabled={!messagesPreferencesState.loaded || messagesPreferencesState.saving}
  />
  <p class="messages-hint">
    Choose APRS-IS only if you have no radio channel configured. The
    default tries RF first and silently falls back to APRS-IS when no
    modem is available.
  </p>
</Box>

<Box title="Blocked call signs">
  <p class="messages-hint block-intro">
    Messages from a blocked call sign are dropped before they reach your
    inbox, and graywolf never acknowledges them. Enter a bare call sign
    (like <code>N0CALL</code>) to block every SSID, or an SSID-qualified
    call sign (like <code>N0CALL-7</code>) to block just that station.
  </p>

  <form class="block-add" onsubmit={(e) => { e.preventDefault(); addBlock(); }}>
    <Input
      type="text"
      value={newCall}
      oninput={onNewCallInput}
      placeholder="N0CALL"
      aria-label="Call sign to block"
    />
    <Input
      type="text"
      bind:value={newNote}
      placeholder="Reason (optional)"
      aria-label="Reason"
    />
    <Button variant="primary" type="submit" disabled={addingBlock}>
      <Icon name="user-x" size="sm" />
      Block
    </Button>
  </form>
  {#if blockError}
    <p class="err" role="alert">{blockError}</p>
  {/if}

  {#if blocklist.length === 0}
    <p class="messages-hint block-empty">No call signs are blocked.</p>
  {:else}
    <ul class="block-rows">
      {#each blocklist as row (row.id)}
        <li class="block-row" class:disabled={row.enabled === false}>
          <div class="block-text">
            <span class="block-call">{row.callsign}</span>
            {#if row.note}<span class="block-note">{row.note}</span>{/if}
          </div>
          <div class="block-actions">
            <Toggle
              checked={row.enabled !== false}
              onCheckedChange={(v) => toggleBlock(row, v)}
              label="Blocking"
              aria-label={`Toggle blocking for ${row.callsign}`}
            />
            <Button variant="ghost" size="sm" onclick={() => removeBlock(row)} aria-label={`Unblock ${row.callsign}`}>
              <Icon name="trash-2" size="sm" />
            </Button>
          </div>
        </li>
      {/each}
    </ul>
  {/if}
</Box>

<style>
  .messages-hint {
    margin-top: 12px;
    font-size: 13px;
    color: var(--text-muted);
  }
  .tx-channel-label {
    display: block;
    margin-top: 16px;
    margin-bottom: 6px;
    font-size: 13px;
    font-weight: 500;
    color: var(--text-default);
  }
  .block-intro { margin-top: 0; margin-bottom: 16px; }
  .block-intro code {
    font-family: var(--font-mono);
    font-size: 12px;
    padding: 1px 4px;
    border-radius: 4px;
    background: var(--color-bg-subtle, rgba(127, 127, 127, 0.12));
  }
  .block-add {
    display: flex;
    gap: 8px;
    align-items: center;
    flex-wrap: wrap;
  }
  /* chonky inputs carry a 1rem bottom margin (stacked-form default); in
     this single row it inflates the wrapper so align-items:center drops
     the Block button below the fields. Zero it so input and button align. */
  .block-add :global(input) { min-width: 0; margin-bottom: 0; }
  .err {
    margin-top: 8px;
    color: var(--color-danger, #d33);
    font-size: 13px;
  }
  .block-empty { font-style: italic; }
  .block-rows {
    list-style: none;
    margin: 16px 0 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .block-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: 8px 10px;
    border-radius: 6px;
    background: var(--color-bg-subtle, rgba(127, 127, 127, 0.06));
  }
  .block-row.disabled { opacity: 0.55; }
  .block-text {
    display: flex;
    align-items: baseline;
    gap: 10px;
    min-width: 0;
  }
  .block-call {
    font-family: var(--font-mono);
    font-weight: 600;
    font-size: 14px;
  }
  .block-note {
    font-size: 12px;
    color: var(--text-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .block-actions {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-shrink: 0;
  }
</style>
