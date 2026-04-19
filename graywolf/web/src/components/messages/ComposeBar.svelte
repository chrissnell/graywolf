<script>
  // Sticky bottom composer.
  //
  // Behavior:
  //   - Auto-grow textarea from 36 → 120 px.
  //   - Character counter: neutral until ≤ 10 remain → warning,
  //     at ≤ 0 soft-splits into Part 2/2. Hard cap at 3 parts (201
  //     chars) disables send.
  //   - Enter sends (and Ctrl/Cmd+Enter too). Shift+Enter inserts a
  //     newline. IME composition guards prevent sending mid-candidate.
  //   - iOS keyboard handling: `position: absolute` + manual
  //     translateY driven by visualViewport.resize.
  //
  // Tactical additions:
  //   - `To:` field locked as an a11y pill (role=text,
  //     aria-label describes destination; tabindex=-1 so it's out
  //     of the tab order).
  //   - Textarea aria-describedby points at the pill's id so a
  //     screen reader announces the destination when focus lands.
  //   - Broadcast banner shown once per session per tactical key
  //     via sessionStorage; suppressed when the thread is empty.
  //   - Send icon swaps to radio-tower.

  import { onMount } from 'svelte';
  import { Icon } from '@chrissnell/chonky-ui';
  import CallsignAutocomplete from './CallsignAutocomplete.svelte';

  const APRS_LIMIT = 67; // chars per APRS message body
  const MAX_PARTS = 3;

  /** @type {{
   *    mode: 'compose' | 'thread',
   *    isTactical?: boolean,
   *    tacticalKey?: string,
   *    tacticalAlias?: string,
   *    dmPeer?: string,
   *    threadHasMessages?: boolean,
   *    onSend?: (text: string, to?: string) => Promise<any>,
   *    onPickTo?: (call: string) => void,
   *    autoFocus?: boolean,
   *    embedded?: boolean,
   *  }}
   */
  let {
    mode = 'thread',
    isTactical = false,
    tacticalKey = '',
    tacticalAlias = '',
    dmPeer = '',
    threadHasMessages = true,
    onSend,
    onPickTo,
    autoFocus = true,
    embedded = false,
  } = $props();

  let text = $state('');
  let toInput = $state('');
  let textareaEl = $state(null);
  let containerEl = $state(null);
  let sending = $state(false);
  let banner = $state(null);

  const length = $derived((text || '').length);
  const parts = $derived(Math.max(1, Math.ceil(length / APRS_LIMIT)));
  const over = $derived(parts > MAX_PARTS);
  const remaining = $derived(APRS_LIMIT - (length % APRS_LIMIT || APRS_LIMIT));
  const showPartBadge = $derived(parts > 1);

  const pillId = 'compose-to-' + Math.random().toString(36).slice(2, 8);
  const bannerStorageKey = $derived(tacticalKey ? `msg.broadcastBanner.${tacticalKey}` : '');

  function autoGrow() {
    if (!textareaEl) return;
    textareaEl.style.height = 'auto';
    const h = Math.min(120, Math.max(36, textareaEl.scrollHeight));
    textareaEl.style.height = `${h}px`;
  }

  async function send() {
    if (over || sending) return;
    const body = (text || '').trim();
    if (!body) return;
    let target = isTactical ? tacticalKey : (mode === 'thread' ? dmPeer : (toInput || '').trim().toUpperCase());
    if (!target) {
      textareaEl?.focus();
      return;
    }
    sending = true;
    try {
      if (parts > 1) {
        // Manually slice to fit the APRS 67-char window; prepend
        // "{N/M} " as a human-readable hint (NOT an APRS-101 format).
        for (let i = 0; i < parts; i++) {
          const slice = body.slice(i * APRS_LIMIT, (i + 1) * APRS_LIMIT);
          const tagged = `{${i + 1}/${parts}} ${slice}`;
          await onSend?.(tagged, target);
        }
      } else {
        await onSend?.(body, target);
      }
      text = '';
      autoGrow();
      textareaEl?.focus({ preventScroll: true });
    } finally {
      sending = false;
    }
  }

  function onKeyDown(e) {
    // Messaging-app convention: plain Enter sends, Shift+Enter inserts
    // a newline. Ctrl/Cmd+Enter also sends (for muscle-memory users).
    //
    // IME guard: when composing a non-Latin character via an input
    // method editor (Japanese, Chinese, Korean, etc.) the Enter key
    // commits the candidate. e.isComposing is true in that case — we
    // must NOT treat it as a send. Legacy WebKit fires keyCode 229
    // during composition; check that too for robustness.
    if (e.key !== 'Enter') return;
    if (e.isComposing || e.keyCode === 229) return;
    if (e.shiftKey) return; // Shift+Enter → newline
    e.preventDefault();
    send();
  }

  function onInput(e) {
    text = e.target.value;
    autoGrow();
  }

  function dismissBanner() {
    banner = false;
    if (bannerStorageKey) {
      try { sessionStorage.setItem(bannerStorageKey, '1'); } catch { /* ignore */ }
    }
  }

  onMount(() => {
    autoGrow();
    if (autoFocus && textareaEl) {
      textareaEl.focus({ preventScroll: true });
    }

    // Per-session banner suppression, plus "suppress if empty thread"
    // (the empty state itself conveys the broadcast semantic).
    if (isTactical && threadHasMessages && bannerStorageKey) {
      try {
        banner = sessionStorage.getItem(bannerStorageKey) !== '1';
      } catch {
        banner = true;
      }
    } else {
      banner = false;
    }

    // iOS keyboard handling: translate the compose pane to sit above
    // the software keyboard without using position:fixed (which
    // floats under the keyboard on iOS). Gracefully degrades in
    // environments without visualViewport (desktop browsers, JSDOM).
    const vv = typeof window !== 'undefined' ? window.visualViewport : null;
    if (!vv) return;
    function apply() {
      if (!containerEl) return;
      const offset = Math.max(0, window.innerHeight - vv.height - vv.offsetTop);
      containerEl.style.transform = `translateY(${-offset}px)`;
    }
    vv.addEventListener('resize', apply);
    vv.addEventListener('scroll', apply);
    apply();
    return () => {
      vv.removeEventListener('resize', apply);
      vv.removeEventListener('scroll', apply);
      if (containerEl) containerEl.style.transform = '';
    };
  });

  // Re-evaluate banner when tacticalKey or threadHasMessages change.
  $effect(() => {
    if (isTactical && threadHasMessages && bannerStorageKey) {
      try {
        banner = sessionStorage.getItem(bannerStorageKey) !== '1';
      } catch {
        banner = true;
      }
    } else {
      banner = false;
    }
  });

  function onToCommit(call) {
    onPickTo?.(call);
    toInput = call;
    textareaEl?.focus({ preventScroll: true });
  }
</script>

<div class="compose" class:embedded bind:this={containerEl} data-testid="compose-bar">
  {#if banner}
    <div class="banner" role="note" data-testid="broadcast-banner">
      <Icon name="radio-tower" size="sm" />
      <span class="banner-text">
        Everyone monitoring <strong>{tacticalKey}</strong> will see this message.
      </span>
      <button type="button" class="banner-dismiss" onclick={dismissBanner} aria-label="Dismiss broadcast notice">
        <Icon name="x" size="sm" />
      </button>
    </div>
  {/if}

  {#if mode === 'compose' && !isTactical}
    <div class="to-row">
      <label class="to-label" for="compose-to-input">To</label>
      <CallsignAutocomplete
        bind:value={toInput}
        placeholder="Callsign or APRS service"
        onCommit={onToCommit}
        autofocus={true}
      />
    </div>
  {:else if isTactical}
    <div class="to-row">
      <span class="to-label">To</span>
      <div
        id={pillId}
        class="to-pill"
        role="group"
        aria-label={`Broadcasting to ${tacticalKey}${tacticalAlias ? ', ' + tacticalAlias : ''}`}
        data-testid="tactical-pill"
      >
        <Icon name="radio-tower" size="sm" />
        <span class="pill-call">{tacticalKey}</span>
        {#if tacticalAlias}
          <span class="pill-alias">{tacticalAlias}</span>
        {/if}
      </div>
    </div>
  {/if}

  <div class="input-row">
    <textarea
      bind:this={textareaEl}
      class="textarea"
      rows="1"
      placeholder={isTactical ? `Message ${tacticalKey}...` : (dmPeer ? `Message ${dmPeer}...` : 'Type a message...')}
      oninput={onInput}
      onkeydown={onKeyDown}
      aria-describedby={isTactical ? pillId : undefined}
      aria-label="Message body"
      data-testid="compose-textarea"
      value={text}
    ></textarea>
    <div class="controls">
      <span class="counter" class:warn={remaining <= 10 && parts === 1} class:over>
        {#if over}
          Too long — max {MAX_PARTS} parts ({MAX_PARTS * APRS_LIMIT} chars)
        {:else if showPartBadge}
          Part {parts}/{parts}
        {:else}
          {remaining}
        {/if}
      </span>
      <button
        type="button"
        class="send"
        onclick={send}
        disabled={over || sending || (text || '').trim().length === 0}
        aria-label="Send message"
        data-testid="compose-send"
      >
        <Icon name={isTactical ? 'radio-tower' : 'send'} size="sm" />
      </button>
    </div>
  </div>
</div>

<style>
  .compose {
    /* position:absolute inside the thread pane so visualViewport
       translations work on iOS. The parent MessageThread provides
       the containing block. The `embedded` variant (e.g. inside
       ComposeNewModal) renders inline so the modal body handles
       placement. */
    position: absolute;
    left: 0;
    right: 0;
    bottom: 0;
    background: var(--color-surface);
    border-top: 1px solid var(--color-border);
    padding: 8px 12px calc(8px + env(safe-area-inset-bottom));
    z-index: 2;
  }
  .compose.embedded {
    position: relative;
    border-top: none;
    padding: 0;
  }

  .banner {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 10px;
    margin-bottom: 8px;
    background: var(--color-warning-muted);
    color: var(--color-warning);
    border: 1px solid var(--color-warning);
    border-radius: var(--radius);
    font-size: 12px;
    font-family: var(--font-mono);
  }
  .banner-text { flex: 1 1 auto; }
  .banner-dismiss {
    background: transparent;
    border: none;
    color: inherit;
    cursor: pointer;
    display: inline-flex;
    padding: 2px;
    border-radius: var(--radius);
  }
  .banner-dismiss:hover { background: rgba(0, 0, 0, 0.2); }

  .to-row {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 6px;
    font-family: var(--font-mono);
  }
  .to-label {
    font-size: 11px;
    font-weight: 700;
    letter-spacing: 1px;
    text-transform: uppercase;
    color: var(--color-text-dim);
    flex-shrink: 0;
    width: 28px;
  }
  .to-pill {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 4px 10px;
    background: var(--color-primary-muted);
    color: var(--color-primary);
    border: 1px solid var(--color-primary);
    border-radius: 999px;
    font-size: 12px;
    font-family: var(--font-mono);
  }
  .pill-call { font-weight: 700; letter-spacing: 0.5px; }
  .pill-alias { opacity: 0.7; }

  .input-row {
    display: flex;
    align-items: flex-end;
    gap: 8px;
  }
  .textarea {
    flex: 1 1 auto;
    min-height: 36px;
    max-height: 120px;
    resize: none;
    padding: 8px 10px;
    background: var(--color-bg);
    border: 1px solid var(--color-border);
    border-radius: var(--radius);
    color: var(--color-text);
    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', system-ui,
      'Helvetica Neue', Arial, sans-serif;
    font-size: 14px;
    line-height: 1.4;
    overflow-y: auto;
  }
  .textarea:focus {
    outline: none;
    border-color: var(--color-primary);
    box-shadow: 0 0 0 2px var(--color-primary-muted);
  }
  .controls {
    display: flex;
    align-items: center;
    gap: 8px;
    padding-bottom: 4px;
  }
  .counter {
    font-size: 11px;
    color: var(--color-text-dim);
    font-family: var(--font-mono);
    min-width: 32px;
    text-align: right;
  }
  .counter.warn { color: var(--color-warning); }
  .counter.over { color: var(--color-danger); }

  .send {
    width: 36px;
    height: 36px;
    border-radius: 999px;
    border: none;
    background: var(--color-primary);
    color: var(--color-primary-fg);
    display: inline-flex;
    align-items: center;
    justify-content: center;
    cursor: pointer;
    transition: background 0.12s, opacity 0.12s;
  }
  .send:hover:not(:disabled) { background: var(--color-primary-hover); }
  .send:disabled {
    opacity: 0.4;
    cursor: not-allowed;
  }
</style>
