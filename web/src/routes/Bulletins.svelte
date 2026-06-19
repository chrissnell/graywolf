<script>
  import { onMount } from 'svelte';
  import { Button, Input } from '@chrissnell/chonky-ui';
  import PageHeader from '../components/PageHeader.svelte';
  import {
    listBulletins,
    sendBulletin,
    deleteBulletin,
    markBulletinRead,
    markAllBulletinsRead,
  } from '../api/bulletins.js';
  import { toasts } from '../lib/stores.js';

  // --- view state ---
  let tab = $state('inbound');           // 'inbound' | 'outbound'
  let inbound = $state([]);
  let outbound = $state([]);
  let loading = $state(true);

  // --- compose form ---
  let slot = $state('BLN0');
  let text = $state('');
  let intervalMins = $state(20); // per-bulletin retransmit interval
  let sending = $state(false);

  const BULLETIN_SLOTS = ['BLN0','BLN1','BLN2','BLN3','BLN4',
                          'BLN5','BLN6','BLN7','BLN8','BLN9'];
  const ANNOUNCEMENT_SLOTS = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ'
    .split('').map(l => 'BLN' + l);
  const ALL_SLOTS = [...BULLETIN_SLOTS, ...ANNOUNCEMENT_SLOTS];

  const MAX_TEXT = 67;
  let textLen = $derived(text.length);
  let textOver = $derived(textLen > MAX_TEXT);
  let isAnnouncement = $derived(!BULLETIN_SLOTS.includes(slot));

  async function load() {
    try {
      const [inRows, outRows] = await Promise.all([
        listBulletins({ direction: 'in' }),
        listBulletins({ direction: 'out' }),
      ]);
      inbound = Array.isArray(inRows) ? inRows : [];
      outbound = Array.isArray(outRows) ? outRows : [];
    } catch (e) {
      toasts.add({ kind: 'error', message: 'Failed to load bulletins.' });
    } finally {
      loading = false;
    }
  }

  async function handleSend() {
    if (textOver || !text.trim()) return;
    sending = true;
    try {
      await sendBulletin({ slot, text: text.trim(), interval_mins: isAnnouncement ? 0 : intervalMins });
      text = '';
      toasts.add({ kind: 'success', message: `Bulletin ${slot} queued for transmission.` });
      await load();
      tab = 'outbound';
    } catch (e) {
      toasts.add({ kind: 'error', message: e?.message || 'Send failed.' });
    } finally {
      sending = false;
    }
  }

  async function handleDelete(id) {
    try {
      await deleteBulletin(id);
      await load();
    } catch (e) {
      toasts.add({ kind: 'error', message: 'Delete failed.' });
    }
  }

  async function handleMarkRead(id) {
    try {
      await markBulletinRead(id);
      inbound = inbound.map(b => b.id === id ? { ...b, unread: false } : b);
    } catch (_) {}
  }

  async function handleMarkAllRead() {
    try {
      await markAllBulletinsRead();
      inbound = inbound.map(b => ({ ...b, unread: false }));
    } catch (_) {}
  }

  function slotLabel(b) {
    return b.is_announcement ? `${b.slot} (Announcement)` : b.slot;
  }

  function sendStatus(b) {
    if (b.send_count >= b.max_sends) return 'Complete';
    if (b.is_announcement) {
      return `${b.send_count} / ${b.max_sends} sent · every 1 hr`;
    }
    if (!b.interval_mins) {
      return `${b.send_count} / ${b.max_sends} sent · burst only`;
    }
    return `${b.send_count} / ${b.max_sends} sent · every ${b.interval_mins} min`;
  }

  function fmtTime(iso) {
    if (!iso) return '—';
    return new Date(iso).toLocaleString();
  }

  onMount(() => {
    load();
    const t = setInterval(load, 30_000);
    return () => clearInterval(t);
  });

  let unreadCount = $derived(inbound.filter(b => b.unread).length);
  let shown = $derived(tab === 'inbound' ? inbound : outbound);
</script>

<div class="page bulletins-page">
  <PageHeader title="Bulletins" />

  <!-- compose panel -->
  <div class="compose-panel">
    <h2 class="compose-title">Send a Bulletin</h2>
    <div class="compose-row">
      <label for="bln-slot" class="compose-label">Slot</label>
      <select id="bln-slot" class="slot-select" bind:value={slot}>
        <optgroup label="Bulletins (BLN0–9, 4 hours)">
          {#each BULLETIN_SLOTS as s}
            <option value={s}>{s}</option>
          {/each}
        </optgroup>
        <optgroup label="Announcements (BLNA–Z, 4 days)">
          {#each ANNOUNCEMENT_SLOTS as s}
            <option value={s}>{s}</option>
          {/each}
        </optgroup>
      </select>
    </div>
    <div class="compose-row">
      <label for="bln-text" class="compose-label">Text</label>
      <div class="text-wrap">
        <Input
          id="bln-text"
          bind:value={text}
          placeholder="Up to 67 characters"
          maxlength={MAX_TEXT}
          class="text-input{textOver ? ' over' : ''}"
        />
        <span class="char-count" class:over={textOver}>{textLen} / {MAX_TEXT}</span>
      </div>
    </div>
    {#if !isAnnouncement}
    <div class="compose-row">
      <label for="bln-interval" class="compose-label">Every</label>
      <div class="interval-wrap">
        <input
          id="bln-interval"
          class="interval-input"
          type="number"
          min="0"
          max="20"
          bind:value={intervalMins}
        />
        <span class="interval-unit">min</span>
        <span class="interval-hint">(0 = burst only, 20 = APRS standard)</span>
      </div>
    </div>
    {/if}
    <div class="compose-actions">
      <Button
        variant="primary"
        disabled={sending || textOver || !text.trim()}
        onclick={handleSend}
      >
        {sending ? 'Sending…' : 'Send Bulletin'}
      </Button>
    </div>
  </div>

  <!-- board tabs -->
  <div class="tabs">
    <button
      class="tab-btn"
      class:active={tab === 'inbound'}
      onclick={() => { tab = 'inbound'; }}
    >
      Received
      {#if unreadCount > 0}
        <span class="tab-badge">{unreadCount}</span>
      {/if}
    </button>
    <button
      class="tab-btn"
      class:active={tab === 'outbound'}
      onclick={() => { tab = 'outbound'; }}
    >
      Sent
    </button>
    {#if tab === 'inbound' && unreadCount > 0}
      <button class="mark-all-read" onclick={handleMarkAllRead}>
        Mark all read
      </button>
    {/if}
  </div>

  {#if loading}
    <p class="status-msg">Loading…</p>
  {:else if shown.length === 0}
    <p class="status-msg empty">
      {tab === 'inbound' ? 'No bulletins received yet.' : 'No outbound bulletins.'}
    </p>
  {:else}
    <div class="bulletin-list">
      {#each shown as b (b.id)}
        <div class="bulletin-row" class:unread={b.unread}>
          <div class="bulletin-header">
            <span class="slot-tag">{slotLabel(b)}</span>
            {#if tab === 'inbound'}
              <span class="from-call">{b.from_call}</span>
              {#if b.unread}
                <button class="read-btn" onclick={() => handleMarkRead(b.id)} title="Mark read">
                  ●
                </button>
              {/if}
            {:else}
              <span class="send-status">{sendStatus(b)}</span>
            {/if}
            <span class="ts">{fmtTime(b.updated_at)}</span>
            <button
              class="del-btn"
              onclick={() => handleDelete(b.id)}
              title="Delete"
              aria-label="Delete bulletin"
            >✕</button>
          </div>
          <p class="bulletin-text">{b.text}</p>
          {#if tab === 'outbound' && b.next_send_at && b.send_count < b.max_sends}
            <p class="next-send">Next send: {fmtTime(b.next_send_at)}</p>
          {/if}
        </div>
      {/each}
    </div>
  {/if}
</div>

<style>
  .bulletins-page {
    padding: 1rem 1.5rem;
    max-width: 800px;
  }

  .compose-panel {
    margin-bottom: 1.5rem;
    padding: 1rem;
  }

  .compose-title {
    font-size: 0.9rem;
    font-weight: 600;
    margin: 0 0 0.75rem;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    opacity: 0.7;
  }

  .compose-row {
    display: flex;
    align-items: center;
    gap: 0.75rem;
    margin-bottom: 0.5rem;
  }

  .compose-label {
    width: 3rem;
    font-size: 0.8rem;
    font-weight: 500;
    opacity: 0.8;
    flex-shrink: 0;
  }

  .slot-select {
    padding: 0.3rem 0.5rem;
    border-radius: 4px;
    border: 1px solid var(--color-border, #444);
    background: var(--color-input-bg, #1e1e1e);
    color: inherit;
    font-size: 0.85rem;
    cursor: pointer;
  }

  .text-wrap {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    flex: 1;
  }

  .char-count {
    font-size: 0.75rem;
    opacity: 0.6;
    white-space: nowrap;
  }

  .char-count.over {
    color: var(--color-error, #e05252);
    opacity: 1;
  }

  .interval-wrap {
    display: flex;
    align-items: center;
    gap: 0.4rem;
  }
  .interval-input {
    width: 4rem;
    padding: 0.25rem 0.4rem;
    border-radius: 4px;
    border: 1px solid var(--color-border, #444);
    background: var(--color-input-bg, #1e1e1e);
    color: inherit;
    font-size: 0.85rem;
    text-align: center;
  }
  .interval-unit {
    font-size: 0.8rem;
    opacity: 0.7;
  }
  .interval-hint {
    font-size: 0.72rem;
    opacity: 0.5;
  }
  .compose-actions {
    margin-top: 0.75rem;
  }

  /* tabs */
  .tabs {
    display: flex;
    align-items: center;
    gap: 0.25rem;
    border-bottom: 1px solid var(--color-border, #444);
    margin-bottom: 1rem;
  }

  .tab-btn {
    padding: 0.4rem 0.9rem;
    background: none;
    border: none;
    border-bottom: 2px solid transparent;
    font-size: 0.85rem;
    cursor: pointer;
    color: inherit;
    opacity: 0.6;
    transition: opacity 0.15s;
    display: flex;
    align-items: center;
    gap: 0.4rem;
  }

  .tab-btn.active {
    opacity: 1;
    border-bottom-color: var(--color-accent, #7bb8e8);
  }

  .tab-badge {
    background: var(--color-accent, #7bb8e8);
    color: #000;
    border-radius: 9999px;
    padding: 0 0.4rem;
    font-size: 0.7rem;
    font-weight: 700;
    min-width: 1.2rem;
    text-align: center;
  }

  .mark-all-read {
    margin-left: auto;
    background: none;
    border: none;
    font-size: 0.75rem;
    cursor: pointer;
    color: var(--color-accent, #7bb8e8);
    padding: 0.25rem 0.5rem;
  }

  /* bulletin rows */
  .status-msg {
    font-size: 0.85rem;
    opacity: 0.6;
    padding: 1rem 0;
  }

  .status-msg.empty {
    text-align: center;
    padding: 2rem 0;
  }

  .bulletin-list {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }

  .bulletin-row {
    background: var(--color-surface, #1e1e1e);
    border: 1px solid var(--color-border, #333);
    border-radius: 6px;
    padding: 0.6rem 0.8rem;
    transition: border-color 0.15s;
  }

  .bulletin-row.unread {
    border-left: 3px solid var(--color-accent, #7bb8e8);
  }

  .bulletin-header {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    margin-bottom: 0.35rem;
    flex-wrap: wrap;
  }

  .slot-tag {
    font-weight: 700;
    font-size: 0.75rem;
    background: var(--color-tag-bg, #2d3748);
    border-radius: 3px;
    padding: 0.1rem 0.35rem;
    letter-spacing: 0.03em;
  }

  .from-call {
    font-weight: 600;
    font-size: 0.8rem;
  }

  .send-status {
    font-size: 0.75rem;
    opacity: 0.7;
  }

  .ts {
    font-size: 0.72rem;
    opacity: 0.5;
    margin-left: auto;
  }

  .del-btn {
    background: none;
    border: none;
    cursor: pointer;
    font-size: 0.8rem;
    opacity: 0.45;
    padding: 0 0.25rem;
    color: inherit;
    line-height: 1;
  }

  .del-btn:hover {
    opacity: 0.9;
    color: var(--color-error, #e05252);
  }

  .read-btn {
    background: none;
    border: none;
    cursor: pointer;
    font-size: 0.6rem;
    color: var(--color-accent, #7bb8e8);
    padding: 0;
    line-height: 1;
  }

  .bulletin-text {
    margin: 0;
    font-size: 0.85rem;
    line-height: 1.4;
  }

  .next-send {
    margin: 0.25rem 0 0;
    font-size: 0.72rem;
    opacity: 0.55;
  }
</style>
