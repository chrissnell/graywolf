<script>
  // Per-conversation routing control (issue #453). A single quiet
  // gear button in the thread header that opens a small popover with:
  //   - Send path: Use default / RF only / APRS-IS only (tactical + DM)
  //   - Automatic Resend until ACK: on by default; off = send once,
  //     no re-sends, for no-ACK handhelds like the TIDRadio TD-H9 (DM
  //     only — tactical rows never enroll in the retry ladder).
  //
  // The button stays visually silent (muted, no dot) while the thread
  // inherits the global defaults, so the compose area and header are
  // uncluttered until the operator deliberately customizes a contact.
  //
  // Persistence is lazy: prefs are fetched the first time the popover
  // opens, and every change PUTs the full state. A reset back to the
  // defaults deletes the row server-side (the response echoes the
  // inherited defaults), keeping the overrides table sparse.

  import { Icon, Radio, RadioGroup, Checkbox, Tooltip } from '@chrissnell/chonky-ui';
  import { getConversationPrefs, putConversationPrefs } from '../../api/messages.js';
  import { toasts } from '../../lib/stores.js';

  /** @type {{ kind: 'dm' | 'tactical', threadKey: string }} */
  let { kind, threadKey } = $props();

  let open = $state(false);
  let loaded = $state(false);
  let saving = $state(false);
  // sendPath: '' (inherit) | 'rf_only' | 'is_only'. waitForAck default true.
  let sendPath = $state('');
  let waitForAck = $state(true);

  let overridden = $derived(sendPath !== '' || waitForAck === false);

  const sendPathLabel = $derived(
    sendPath === 'rf_only' ? 'RF only'
      : sendPath === 'is_only' ? 'APRS-IS only'
      : sendPath === 'both' ? 'RF + APRS-IS'
      : 'Default path',
  );

  async function load() {
    if (loaded || !threadKey) return;
    try {
      const p = await getConversationPrefs(kind, threadKey);
      sendPath = p?.send_path ?? '';
      waitForAck = p?.wait_for_ack ?? true;
      loaded = true;
    } catch {
      // Loaded stays false → the controls remain disabled so the operator
      // can't unknowingly overwrite a stored override with the default
      // state we're showing. Surface the failure and let them reopen to
      // retry the fetch.
      toasts.error("Couldn't load routing for this conversation — try again.");
    }
  }

  async function toggle() {
    open = !open;
    if (open) await load();
  }

  async function persist(next) {
    const prev = { sendPath, waitForAck };
    sendPath = next.sendPath;
    waitForAck = next.waitForAck;
    saving = true;
    try {
      const resp = await putConversationPrefs(kind, threadKey, {
        send_path: sendPath,
        wait_for_ack: waitForAck,
      });
      // Echo the server's canonical state (a reset returns defaults).
      sendPath = resp?.send_path ?? '';
      waitForAck = resp?.wait_for_ack ?? true;
    } catch {
      sendPath = prev.sendPath;
      waitForAck = prev.waitForAck;
      toasts.error("Couldn't save routing for this conversation — try again.");
    } finally {
      saving = false;
    }
  }

  function onSendPath(v) {
    if (v === sendPath) return;
    persist({ sendPath: v, waitForAck });
  }
  function onWaitForAck(v) {
    if (v === waitForAck) return;
    persist({ sendPath, waitForAck: v });
  }

  // Close on outside click / Escape.
  function onWindowClick(e) {
    if (!open) return;
    if (e.target instanceof Node && rootEl && !rootEl.contains(e.target)) open = false;
  }
  function onKeydown(e) {
    if (e.key === 'Escape') open = false;
  }
  let rootEl = $state(null);
</script>

<svelte:window onclick={onWindowClick} onkeydown={onKeydown} />

<div class="routing" bind:this={rootEl}>
  <Tooltip>
    <Tooltip.Trigger>
      <button
        type="button"
        class="routing-btn"
        class:active={overridden}
        aria-label="Conversation routing"
        aria-haspopup="true"
        aria-expanded={open}
        onclick={toggle}
        data-testid="conversation-routing-toggle"
      >
        <Icon name="settings" size="md" />
        {#if overridden}<span class="dot" aria-hidden="true"></span>{/if}
      </button>
    </Tooltip.Trigger>
    <Tooltip.Content>Routing{overridden ? ` — ${sendPathLabel}${!waitForAck ? ', no resend' : ''}` : ''}</Tooltip.Content>
  </Tooltip>

  {#if open}
    <div class="panel" role="dialog" aria-label="Conversation routing" data-testid="conversation-routing-panel">
      <p class="panel-title">Send path</p>
      <RadioGroup value={sendPath} onValueChange={onSendPath} disabled={saving || !loaded}>
        <div class="opts">
          <Radio value="" label="Use default" />
          <Radio value="rf_only" label="RF only" />
          <Radio value="is_only" label="APRS-IS only" />
        </div>
      </RadioGroup>
      <p class="panel-hint">Overrides the global send path for this conversation only. Retries follow the same path.</p>

      {#if kind === 'dm'}
        <hr class="sep" />
        <label class="ack-row">
          <Checkbox checked={waitForAck} onCheckedChange={onWaitForAck} disabled={saving || !loaded} />
          <span>Automatic Resend until ACK</span>
        </label>
        <p class="panel-hint">On: resend until the recipient acknowledges. Turn off for radios that never send acks (e.g. TIDRadio TD-H9) so a message is sent once, with no re-sends.</p>
      {/if}
    </div>
  {/if}
</div>

<style>
  .routing {
    position: relative;
    display: inline-flex;
  }
  .routing-btn {
    position: relative;
    background: transparent;
    border: none;
    color: var(--color-text-muted);
    cursor: pointer;
    padding: 4px;
    border-radius: var(--radius);
    display: inline-flex;
    align-items: center;
    justify-content: center;
  }
  .routing-btn:hover {
    color: var(--color-text-default);
    background: var(--color-surface-hover, rgba(127, 127, 127, 0.12));
  }
  .routing-btn.active {
    color: var(--color-accent, var(--color-text-default));
  }
  .dot {
    position: absolute;
    top: 2px;
    right: 2px;
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--color-accent, currentColor);
  }
  .panel {
    position: absolute;
    top: calc(100% + 6px);
    right: 0;
    z-index: 40;
    width: 260px;
    padding: 12px;
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: var(--radius);
    box-shadow: 0 8px 24px rgba(0, 0, 0, 0.24);
  }
  .panel-title {
    margin: 0 0 8px;
    font-size: 11px;
    font-weight: 700;
    letter-spacing: 1px;
    text-transform: uppercase;
    color: var(--color-text-dim);
  }
  .opts {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .panel-hint {
    margin: 8px 0 0;
    font-size: 12px;
    line-height: 1.4;
    color: var(--color-text-muted);
  }
  .sep {
    border: none;
    border-top: 1px solid var(--color-border);
    margin: 12px 0;
  }
  .ack-row {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 13px;
    color: var(--color-text-default);
    cursor: pointer;
  }
</style>
