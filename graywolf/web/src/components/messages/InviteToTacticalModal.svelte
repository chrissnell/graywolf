<script>
  // Invite-to-tactical modal.
  //
  // Two-axis interaction: the tactical is fixed (carried in the header,
  // not pinned to a pill in the body the way ComposeNewModal pins its
  // To: pill), and the variable is the recipient chip set. The body
  // wraps `CallsignAutocomplete` in a chip picker with paste-list
  // expansion, dedupe-on-commit, and self-invite filtering. Send fires
  // one `POST /api/messages` per chip concurrently; per-chip state
  // transitions idle → sending → sent | failed, driven by SSE ack
  // reconciliation through `messagesStore.messageById`, not the POST
  // response (202 returns before RF transmission completes).
  //
  // The modal deliberately stays open after the Send click: the user
  // watches each chip settle. Once every chip is terminal (sent or
  // failed), the Send button relabels to `Done` (or `Retry all (N)`
  // if all failed). Confirm-close on Escape/backdrop/X while anything
  // is still `sending`.

  import { onMount, tick } from 'svelte';
  import { Button, Icon, Modal } from '@chrissnell/chonky-ui';
  import { messages as store } from '../../lib/messagesStore.svelte.js';
  import { sendMessage } from '../../api/messages.js';
  import { api } from '../../lib/api.js';
  import { toasts } from '../../lib/stores.js';
  import {
    classifyCommit,
    classifyPasteList,
    normalizeCall,
    PASTE_SPLIT_RE,
  } from '../../lib/inviteValidation.js';
  import CallsignAutocomplete from './CallsignAutocomplete.svelte';

  /** @type {{
   *    tactical: string,
   *    open: boolean,
   *    onClose: () => void,
   *  }}
   */
  let {
    tactical,
    open = $bindable(false),
    onClose,
  } = $props();

  // --- Chip state ---------------------------------------------------
  // Paste-list + chip validation rules live in lib/inviteValidation.js
  // (pure logic, unit-tested).
  //
  // Per-chip record:
  //   {call, state, messageId?, error?, flashing?}
  //   state: 'idle' | 'sending' | 'sent' | 'failed'
  let chips = $state([]);
  let autoInput = $state('');
  let hintText = $state('');
  let isOnline = $state(true);
  let ourCall = $state('');
  let sendBtnWrap = $state(null);
  let confirmCloseOpen = $state(false);

  // Fetch operator callsign from /igate (same pattern as TacticalSettings).
  onMount(async () => {
    try {
      const ig = await api.get('/igate');
      ourCall = (ig?.callsign || '').toUpperCase();
    } catch {
      // Non-fatal — self-filter just won't work if this fails.
    }
    if (typeof navigator !== 'undefined') {
      isOnline = !!navigator.onLine;
    }
    const onOnline = () => { isOnline = true; };
    const onOffline = () => { isOnline = false; };
    if (typeof window !== 'undefined') {
      window.addEventListener('online', onOnline);
      window.addEventListener('offline', onOffline);
    }
    return () => {
      if (typeof window !== 'undefined') {
        window.removeEventListener('online', onOnline);
        window.removeEventListener('offline', onOffline);
      }
    };
  });

  // Clear state whenever the modal opens so reopening after a previous
  // batch doesn't leak chips.
  $effect(() => {
    if (open) {
      chips = [];
      autoInput = '';
      hintText = '';
    }
  });

  // --- SSE reconciliation -------------------------------------------
  // Watch `messageById` for acks; flip per-chip `sending → sent`.
  // `failed` is set directly on POST rejection (covered in send loop).
  $effect(() => {
    // Touch the map so Svelte re-runs when anything changes.
    const map = store.messageById;
    let mutated = false;
    const next = chips.map((c) => {
      if (c.state !== 'sending' || !c.messageId) return c;
      const m = map.get(c.messageId);
      if (!m) return c;
      // Terminal success states:
      //   - acked (APRS ack received)
      //   - sent / sent_rf / sent_is (transmitted; ack may or may not follow)
      //   - timeout (retry budget exhausted; plan treats it as "sent")
      // Terminal failure states:
      //   - rejected, failed
      if (['acked', 'sent', 'sent_rf', 'sent_is', 'timeout'].includes(m.status)) {
        mutated = true;
        return { ...c, state: 'sent' };
      }
      if (['rejected', 'failed'].includes(m.status)) {
        mutated = true;
        return { ...c, state: 'failed', error: m.failure_reason || m.status };
      }
      return c;
    });
    if (mutated) chips = next;
  });

  // --- Chip state derivations ---------------------------------------
  const chipCount = $derived(chips.length);
  const anySending = $derived(chips.some((c) => c.state === 'sending'));
  const allTerminal = $derived(
    chipCount > 0 && chips.every((c) => c.state === 'sent' || c.state === 'failed'),
  );
  const allFailed = $derived(
    chipCount > 0 && chips.every((c) => c.state === 'failed'),
  );

  const sendLabel = $derived.by(() => {
    if (chipCount === 0) return 'Send';
    if (allTerminal) {
      if (allFailed) return `Retry all (${chipCount})`;
      return 'Done';
    }
    const noun = chipCount === 1 ? 'invitation' : 'invitations';
    return `Send ${chipCount} ${noun}`;
  });

  const sendDisabled = $derived(
    chipCount === 0 || anySending || (!isOnline && !allTerminal),
  );

  // --- Chip mutation helpers ----------------------------------------
  function indexOfChip(call) {
    const norm = normalizeCall(call);
    return chips.findIndex((c) => c.call === norm);
  }

  function flashChip(idx) {
    if (idx < 0 || idx >= chips.length) return;
    chips = chips.map((c, i) => (i === idx ? { ...c, flashing: true } : c));
    setTimeout(() => {
      chips = chips.map((c, i) => (i === idx ? { ...c, flashing: false } : c));
    }, 600);
  }

  /**
   * Commit a single callsign as a chip. Returns the same outcome set as
   * classifyCommit: 'ok' | 'duplicate' | 'invalid' | 'self'. Caller
   * decides whether to show a toast / clear the input.
   */
  function commitChip(raw) {
    const existingCalls = chips.map((c) => c.call);
    const outcome = classifyCommit(raw, existingCalls, ourCall);
    if (outcome === 'ok') {
      chips = [...chips, { call: normalizeCall(raw), state: 'idle' }];
    } else if (outcome === 'duplicate') {
      const idx = indexOfChip(raw);
      flashChip(idx);
    }
    return outcome;
  }

  function removeChip(idx) {
    const chip = chips[idx];
    if (!chip) return;
    // Locked chips (sent / sending) cannot be removed via ×.
    if (chip.state === 'sent' || chip.state === 'sending') return;
    chips = chips.filter((_, i) => i !== idx);
  }

  function onAutocompleteCommit(call) {
    const result = commitChip(call);
    // Reset hint only when something new happens; don't stomp a paste-list hint.
    if (result === 'ok') hintText = '';
    if (result === 'self') {
      toasts.error("You can't invite yourself");
    }
    // Dedupe on commit: DO NOT clear the input. The plan explicitly
    // says the user backspaces to clear it so they know the autocomplete
    // didn't add anything.
    if (result === 'duplicate') return;
    autoInput = '';
  }

  function handlePaste(e) {
    const pasted = e?.clipboardData?.getData?.('text') || '';
    if (!pasted || !PASTE_SPLIT_RE.test(pasted)) {
      // Single-token paste: let the autocomplete handle it normally.
      return;
    }
    e.preventDefault();
    const existingCalls = chips.map((c) => c.call);
    const { added, duplicate, invalid, self } = classifyPasteList(pasted, existingCalls, ourCall);
    // Commit the valid tokens as chips (classifyPasteList already
    // verified them; commitChip repeats the check but is idempotent on
    // already-passed tokens).
    for (const call of added) {
      chips = [...chips, { call, state: 'idle' }];
    }
    if (self.length > 0) {
      toasts.error("You can't invite yourself");
    }
    // Hint: "N added · M invalid [· K duplicate]" — omit zero counts.
    const invalidTotal = invalid.length + self.length;
    const parts = [];
    if (added.length > 0) parts.push(`${added.length} added`);
    if (duplicate.length > 0) parts.push(`${duplicate.length} duplicate`);
    if (invalidTotal > 0) parts.push(`${invalidTotal} invalid`);
    hintText = parts.length > 0 ? parts.join(' · ') : '';
    // Leave invalid tokens in the input so the operator can fix them;
    // self + malformed are both "not added".
    autoInput = [...invalid, ...self].join(' ');
  }

  function handleChipKeydown(e, idx) {
    if (e.key === 'Backspace' || e.key === 'Delete') {
      e.preventDefault();
      removeChip(idx);
    }
  }

  // Capture-phase keyboard on the Recipients container:
  //   - Backspace on empty autocomplete input → remove last chip.
  //   - Enter on empty input → focus Send.
  //   - Cmd/Ctrl+Enter → send from anywhere inside the modal.
  function onRecipientsKeydown(e) {
    if ((e.ctrlKey || e.metaKey) && e.key === 'Enter') {
      e.preventDefault();
      e.stopPropagation();
      handleSend();
      return;
    }
    // Only the input's raw keydown can tell us "input is empty".
    const isAutocompleteInput = e.target?.getAttribute?.('role') === 'combobox';
    if (!isAutocompleteInput) return;
    const val = e.target.value || '';
    if (e.key === 'Backspace' && val === '' && chips.length > 0) {
      e.preventDefault();
      // Find the last non-locked chip to remove.
      for (let i = chips.length - 1; i >= 0; i--) {
        const c = chips[i];
        if (c.state !== 'sent' && c.state !== 'sending') {
          removeChip(i);
          return;
        }
      }
      return;
    }
    if (e.key === 'Enter' && val === '') {
      e.preventDefault();
      e.stopPropagation();
      if (chips.length > 0 && sendBtnWrap) {
        const btn = sendBtnWrap.querySelector('button');
        btn?.focus();
      }
    }
  }

  // --- Send flow ----------------------------------------------------
  async function sendOne(idx) {
    const chip = chips[idx];
    if (!chip) return;
    // Transition to sending
    chips = chips.map((c, i) => (i === idx ? { ...c, state: 'sending', error: undefined } : c));
    try {
      const res = await sendMessage({
        to: chip.call,
        text: '',
        kind: 'invite',
        invite_tactical: tactical,
      });
      // Feed the freshly-persisted row into the shared store so the
      // messageById effect above can reconcile acks arriving via SSE /
      // poll without a cursor round-trip. This is the same pattern the
      // parent Messages.svelte uses via `reconcilePending`, minus the
      // pending clientId bookkeeping (invite sends don't go through the
      // optimistic-bubble pipeline).
      if (res) store.upsertMessage(res);

      // Capture id for SSE reconciliation. If the row is already in a
      // terminal state (status arrived with the POST response, which
      // can happen for IS-only sends or immediate-fail paths), mirror
      // it immediately.
      chips = chips.map((c, i) => {
        if (i !== idx) return c;
        const next = { ...c, messageId: res?.id };
        if (res && ['acked', 'sent', 'sent_rf', 'sent_is', 'timeout'].includes(res.status)) {
          next.state = 'sent';
        } else if (res && ['rejected', 'failed'].includes(res.status)) {
          next.state = 'failed';
          next.error = res.failure_reason || res.status;
        }
        return next;
      });
    } catch (err) {
      chips = chips.map((c, i) => (
        i === idx
          ? { ...c, state: 'failed', error: err?.message || 'Send failed' }
          : c
      ));
    }
  }

  async function handleSend() {
    if (chipCount === 0) return;
    // If everyone is terminal and this is the Done button, close.
    if (allTerminal && !allFailed) {
      doClose();
      return;
    }
    // Retry-all: flip every failed chip back to idle and continue.
    if (allTerminal && allFailed) {
      chips = chips.map((c) => (
        c.state === 'failed' ? { ...c, state: 'idle', error: undefined } : c
      ));
      await tick();
    }
    // Otherwise, fire every `idle` chip in parallel. `sending` chips
    // stay as-is (shouldn't happen — send is disabled while any are
    // sending).
    const jobs = [];
    chips.forEach((c, i) => {
      if (c.state === 'idle') jobs.push(sendOne(i));
    });
    await Promise.allSettled(jobs);
  }

  async function retryChip(idx) {
    const chip = chips[idx];
    if (!chip || chip.state !== 'failed') return;
    chips = chips.map((c, i) => (i === idx ? { ...c, state: 'idle', error: undefined } : c));
    await tick();
    await sendOne(idx);
  }

  // --- Close lifecycle ----------------------------------------------
  function requestClose() {
    if (anySending) {
      confirmCloseOpen = true;
      return false;
    }
    doClose();
    return true;
  }

  function doClose() {
    confirmCloseOpen = false;
    open = false;
    onClose?.();
  }

  function cancelClose() {
    confirmCloseOpen = false;
  }

  // Chonky's Modal toggles `open` via bits-ui Dialog on Escape + backdrop
  // click. We intercept via `onOpenChange` below: if the close is not
  // allowed, we re-open the dialog in the next tick. Not pretty, but
  // bits-ui's Dialog doesn't expose a `preventDefault`-style close hook.
  function onOpenChange(nextOpen) {
    if (nextOpen) return;
    if (!requestClose()) {
      // Snap back open.
      tick().then(() => { open = true; });
    }
  }
</script>

<Modal bind:open {onOpenChange}>
  <Modal.Header>
    <h3 class="title">Invite to {tactical}</h3>
    <Modal.Close aria-label="Close">
      <Icon name="x" size="sm" />
    </Modal.Close>
  </Modal.Header>
  <Modal.Body>
    {#if !isOnline}
      <div class="offline-banner" role="status">
        <Icon name="wifi-off" size="sm" />
        <span>You're offline — invitations will queue.</span>
      </div>
    {/if}

    <div
      class="recipients"
      role="group"
      aria-label="Recipients"
    >
      <label class="label" for="invite-recipients-input">Recipients</label>
      <!-- keydown lives on the chip-area because its direct descendants
           include the autocomplete input; an outer role="group" + keydown
           trips a11y rules. Cmd/Ctrl+Enter, Backspace-on-empty, and
           Enter-on-empty are all handled there. This element is a
           container for an actually-interactive combobox child; the
           outer keydown is a delegation shortcut, not an interactive
           role in its own right. -->
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
      <div
        class="chip-area"
        onpaste={handlePaste}
        onkeydown={onRecipientsKeydown}
      >
        {#each chips as chip, i (chip.call)}
          <span
            class="chip"
            class:sending={chip.state === 'sending'}
            class:sent={chip.state === 'sent'}
            class:failed={chip.state === 'failed'}
            class:flashing={chip.flashing}
            data-testid="invite-chip"
            data-call={chip.call}
            data-state={chip.state}
          >
            <span class="chip-call">{chip.call}</span>
            {#if chip.state === 'sending'}
              <span class="chip-status" aria-label="Sending">
                <Icon name="refresh-cw" size="xs" />
              </span>
            {:else if chip.state === 'sent'}
              <span class="chip-status" aria-label="Sent">
                <Icon name="check" size="xs" />
              </span>
            {:else if chip.state === 'failed'}
              <button
                type="button"
                class="chip-retry"
                onclick={() => retryChip(i)}
                aria-label={`Retry invitation to ${chip.call}`}
                data-testid="chip-retry"
              >
                <Icon name="refresh-cw" size="xs" />
              </button>
            {/if}
            {#if chip.state !== 'sent' && chip.state !== 'sending'}
              <button
                type="button"
                class="chip-remove"
                onclick={() => removeChip(i)}
                onkeydown={(e) => handleChipKeydown(e, i)}
                aria-label={`Remove ${chip.call}`}
              >
                <Icon name="x" size="xs" />
              </button>
            {/if}
          </span>
        {/each}

        <div class="autocomplete-wrap">
          <CallsignAutocomplete
            bind:value={autoInput}
            placeholder={chips.length === 0 ? 'Callsign, tactical, or paste a list' : 'Add another…'}
            onCommit={onAutocompleteCommit}
            autofocus={true}
          />
        </div>
      </div>
      {#if hintText}
        <div class="hint" data-testid="paste-hint">{hintText}</div>
      {:else if chipCount === 0}
        <div class="helper">Add at least one recipient.</div>
      {/if}
    </div>

    <div class="actions" bind:this={sendBtnWrap}>
      <Button
        variant="primary"
        onclick={handleSend}
        disabled={sendDisabled}
        data-testid="invite-send"
      >
        {sendLabel}
      </Button>
    </div>
  </Modal.Body>
</Modal>

{#if confirmCloseOpen}
  <div
    class="confirm-close"
    role="alertdialog"
    aria-modal="true"
    aria-labelledby="invite-confirm-title"
  >
    <div class="confirm-card">
      <h4 id="invite-confirm-title">Close while sending?</h4>
      <p>Some invitations are still transmitting. They'll continue, but you won't see status updates.</p>
      <div class="confirm-actions">
        <Button variant="ghost" onclick={cancelClose}>Keep open</Button>
        <Button variant="primary" onclick={doClose}>Close anyway</Button>
      </div>
    </div>
  </div>
{/if}

<style>
  .title {
    margin: 0;
    font-size: 14px;
    font-weight: 600;
    font-family: var(--font-mono);
  }
  .offline-banner {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 6px 10px;
    margin-bottom: 10px;
    background: var(--color-warning-muted, rgba(250, 175, 75, 0.15));
    border: 1px solid var(--color-warning, #eab308);
    border-radius: var(--radius);
    font-size: 12px;
    color: var(--color-text);
  }
  .recipients {
    display: flex;
    flex-direction: column;
    gap: 4px;
    margin-bottom: 16px;
  }
  .label {
    font-size: 11px;
    font-weight: 700;
    letter-spacing: 1px;
    text-transform: uppercase;
    color: var(--color-text-dim);
  }
  .chip-area {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    align-items: center;
    padding: 6px;
    background: var(--color-bg);
    border: 1px solid var(--color-border);
    border-radius: var(--radius);
    min-height: 44px;
  }
  .chip-area:focus-within {
    border-color: var(--color-primary);
    box-shadow: 0 0 0 2px var(--color-primary-muted);
  }
  .autocomplete-wrap {
    flex: 1 1 180px;
    min-width: 180px;
  }
  /* The autocomplete renders its own bordered input; flatten it so it
     blends into the chip-area surface. */
  :global(.chip-area .wrap .input) {
    border: none !important;
    box-shadow: none !important;
    background: transparent !important;
    padding: 4px 2px !important;
  }
  .chip {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    min-height: 24px;
    padding: 0 4px 0 10px;
    background: var(--color-surface-raised);
    border: 1px solid var(--color-border);
    border-radius: 999px;
    font-family: var(--font-mono);
    font-size: 12px;
    letter-spacing: 0.5px;
    color: var(--color-text);
  }
  .chip.sending {
    border-color: var(--color-primary);
    color: var(--color-primary);
  }
  .chip.sent {
    border-color: var(--color-success, #22c55e);
    color: var(--color-success, #22c55e);
    background: var(--color-success-muted, rgba(34, 197, 94, 0.12));
  }
  .chip.failed {
    border-color: var(--color-danger);
    color: var(--color-danger);
    background: var(--color-danger-muted, rgba(239, 68, 68, 0.12));
  }
  @keyframes chipflash {
    0%, 100% { box-shadow: 0 0 0 0 var(--color-primary-muted); }
    40%      { box-shadow: 0 0 0 4px var(--color-primary-muted); }
  }
  .chip.flashing {
    animation: chipflash 600ms ease-out;
    border-color: var(--color-primary);
  }
  .chip-call {
    display: inline-flex;
    align-items: center;
    line-height: 1;
    font-weight: 600;
  }
  .chip-remove,
  .chip-retry,
  .chip-status {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 18px;
    height: 18px;
    border-radius: 999px;
    background: transparent;
    border: none;
    color: inherit;
    cursor: pointer;
    padding: 0;
  }
  .chip-status {
    cursor: default;
  }
  .chip.sending .chip-status :global(svg) {
    animation: spin 1s linear infinite;
  }
  @keyframes spin {
    from { transform: rotate(0deg); }
    to   { transform: rotate(360deg); }
  }
  .chip-remove:hover,
  .chip-retry:hover {
    background: var(--color-surface);
  }
  .chip-remove:focus-visible,
  .chip-retry:focus-visible {
    outline: 2px solid var(--color-primary);
    outline-offset: 1px;
  }
  .hint {
    font-size: 11px;
    color: var(--color-text-muted);
    margin-top: 2px;
  }
  .helper {
    font-size: 11px;
    color: var(--color-text-dim);
    margin-top: 2px;
  }
  .actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    margin-top: 4px;
  }

  /* Lightweight inline confirm sheet. The chonky <AlertDialog> is
     rendered in a portal and conflicts with the parent Modal's focus
     trap; this in-flow card sits on top of the Modal content and
     keeps focus contained. */
  .confirm-close {
    position: fixed;
    inset: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    background: rgba(0, 0, 0, 0.45);
    z-index: 2000;
  }
  .confirm-card {
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: var(--radius);
    padding: 16px;
    max-width: 360px;
    width: calc(100% - 32px);
    box-shadow: 0 8px 32px rgba(0, 0, 0, 0.5);
  }
  .confirm-card h4 {
    margin: 0 0 8px 0;
    font-size: 14px;
  }
  .confirm-card p {
    margin: 0 0 12px 0;
    font-size: 13px;
    color: var(--color-text-muted);
  }
  .confirm-actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
  }
</style>
