<script>
  // Sticky bottom composer.
  //
  // Behavior:
  //   - Auto-grow textarea from 36 → 120 px.
  //   - Character counter: neutral → warning at ≤ 20 remaining →
  //     danger over cap. Multi-part slices reserve 6 chars for the
  //     "{N/M} " prefix. Default mode: APRS_LIMIT=67 / MAX_PARTS=3
  //     (183 chars composable). Long mode (advanced, opt-in):
  //     APRS_LIMIT=200 / MAX_PARTS=5 (970 chars composable, but the
  //     operational intent is ~200-char single frames — splitting is
  //     left active so word-boundary behaviour stays consistent).
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
  import {
    messagesPreferencesState,
    DEFAULT_MAX_MESSAGE_TEXT,
  } from '../../lib/settings/messages-preferences-store.svelte.js';

  // Multi-part messages carry a "{N/M} " prefix (6 chars with single-digit
  // N and M) so each slice must reserve room for it to stay under APRS_LIMIT.
  const PART_PREFIX_LEN = 6;

  // Reactive per-frame cap: 67 (default / enforced) or 200 (long mode).
  // Falls back to the safe default (67) until the preferences store has
  // loaded — we'd rather briefly enforce 67 on a freshly-mounted compose
  // bar than flash a 200-char cap that the server may not honour.
  const APRS_LIMIT = $derived(
    messagesPreferencesState.loaded
      ? messagesPreferencesState.maxMessageText
      : DEFAULT_MAX_MESSAGE_TEXT,
  );
  const LONG_MODE = $derived(APRS_LIMIT > DEFAULT_MAX_MESSAGE_TEXT);
  // Parts ceiling scales with the per-frame cap.
  //   Default: MAX_PARTS=3 → 3 * (67-6) = 183 chars composable (today's behaviour).
  //   Long:    MAX_PARTS=5 → 5 * (200-6) = 970 chars composable. Formula
  //            sanity-check: ceil((200 + 6*5) / (200-6)) = ceil(230/194) = 2,
  //            so 5 is comfortable headroom for any realistic 200-char
  //            payload after whitespace-aligned splitting.
  const MAX_PARTS = $derived(LONG_MODE ? 5 : 3);
  const PART_SLICE = $derived(APRS_LIMIT - PART_PREFIX_LEN);

  // Split `body` into chunks of at most `maxLen` chars, preferring
  // whitespace boundaries so words don't get chopped across parts.
  // Falls back to a hard cut when a single token already exceeds
  // maxLen with no whitespace to break on. Caller is expected to
  // pass pre-trimmed text whose length exceeds `maxLen`.
  function splitOnWhitespace(body, maxLen) {
    const out = [];
    let rest = body;
    while (rest.length > maxLen) {
      let cut = -1;
      for (let i = maxLen; i > 0; i--) {
        if (/\s/.test(rest[i])) { cut = i; break; }
      }
      if (cut === -1) cut = maxLen; // unbreakable word — hard cut fallback
      out.push(rest.slice(0, cut).replace(/\s+$/, ''));
      rest = rest.slice(cut).replace(/^\s+/, '');
    }
    if (rest.length > 0) out.push(rest);
    return out;
  }

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
  // Truncate-risk banner visibility (long mode + draft > 67 chars).
  // Session-scoped dismissal via sessionStorage.
  let truncateBannerDismissed = $state(false);

  const length = $derived((text || '').length);
  // Single-part fits in APRS_LIMIT verbatim; multi-part uses the smaller
  // PART_SLICE to leave room for the "{N/M} " prefix on each wire message.
  // Split on whitespace so we never chop a word across two parts.
  const partsList = $derived.by(() => {
    const trimmed = (text || '').trim();
    if (trimmed.length === 0) return [];
    if (trimmed.length <= APRS_LIMIT) return [trimmed];
    return splitOnWhitespace(trimmed, PART_SLICE);
  });
  const parts = $derived(Math.max(1, partsList.length));
  // Send is blocked when the body cannot fit in MAX_PARTS wire frames. The
  // threshold scales naturally with APRS_LIMIT via PART_SLICE and MAX_PARTS.
  const over = $derived(parts > MAX_PARTS);
  // Per-frame "chars remaining" for the single-part view. For multi-part
  // drafts we show "Part N/M" instead, so this only matters at length <= APRS_LIMIT.
  const remaining = $derived(APRS_LIMIT - (length % APRS_LIMIT || APRS_LIMIT));
  const showPartBadge = $derived(parts > 1);
  // Counter colour ramp:
  //   - warning at <= 20 remaining in the current (single) frame, so
  //     operators get a heads-up before the soft split kicks in;
  //   - danger once `over` is true (body cannot fit in MAX_PARTS frames
  //     and send is blocked). Multi-part sends stay neutral — they're
  //     valid APRS and send fine.
  const counterWarn = $derived(!over && !showPartBadge && remaining <= 20 && length > 0);
  const counterOver = $derived(over);
  // Screen-reader announcement — only changes on threshold transitions
  // (not on every keystroke), to avoid a per-character flood through the
  // aria-live region. The visible counter stays silent; this sr-only
  // string is the only thing AT/SR hears for this element.
  const counterAnnouncement = $derived(
    counterOver
      ? `Over radio limit, ${APRS_LIMIT} maximum.`
      : showPartBadge
        ? `Message will split into ${parts} parts.`
        : counterWarn
          ? 'Approaching radio character limit.'
          : ''
  );
  // Truncate-risk banner: only meaningful when long mode is active AND the
  // current draft already passes the 67-char legacy-receiver threshold.
  const showTruncateBanner = $derived(
    LONG_MODE &&
    length > DEFAULT_MAX_MESSAGE_TEXT &&
    !truncateBannerDismissed,
  );

  const pillId = 'compose-to-' + Math.random().toString(36).slice(2, 8);
  const bannerStorageKey = $derived(tacticalKey ? `msg.broadcastBanner.${tacticalKey}` : '');
  // Stable session key — one dismissal covers the whole tab session across
  // thread/compose remounts. Re-appears in a fresh tab.
  const TRUNCATE_BANNER_KEY = 'compose.truncateWarningDismissed';

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
      if (partsList.length > 1) {
        // Each slice is already whitespace-aligned to fit PART_SLICE
        // (= APRS_LIMIT - "{N/M} " prefix width). The "{N/M} " prefix
        // is a human-readable hint, NOT APRS-101.
        for (let i = 0; i < partsList.length; i++) {
          const tagged = `{${i + 1}/${partsList.length}} ${partsList[i]}`;
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

  function dismissTruncateBanner() {
    truncateBannerDismissed = true;
    try { sessionStorage.setItem(TRUNCATE_BANNER_KEY, '1'); } catch { /* ignore */ }
    // Return focus to the textarea so the user's editing flow isn't
    // interrupted by the close action.
    textareaEl?.focus({ preventScroll: true });
  }

  onMount(() => {
    autoGrow();
    if (autoFocus && textareaEl) {
      textareaEl.focus({ preventScroll: true });
    }

    // Hydrate the preferences store even if the user never visited /preferences
    // this session. Safe to call on every mount — the store just re-GETs,
    // and the default unloaded state (APRS_LIMIT = 67) is already safe.
    messagesPreferencesState.fetchPreferences();

    // Truncate-risk banner: restore session-scoped dismissal.
    try {
      truncateBannerDismissed = sessionStorage.getItem(TRUNCATE_BANNER_KEY) === '1';
    } catch {
      truncateBannerDismissed = false;
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

  {#if showTruncateBanner}
    <div class="banner" role="note" data-testid="truncate-banner">
      <Icon name="alert-circle" size="sm" />
      <span class="banner-text">
        This is longer than 67 chars. Older radios and some iGates may truncate it.
      </span>
      <button
        type="button"
        class="banner-dismiss"
        onclick={dismissTruncateBanner}
        aria-label="Dismiss"
        data-testid="truncate-banner-dismiss"
      >
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
      {#if messagesPreferencesState.allowLong}
        <span
          class="long-mode-pill"
          aria-label="Long-message mode active. Splits up to 200 chars across parts; some receivers may truncate."
          data-testid="long-mode-pill"
        >
          Long mode
        </span>
      {/if}
      <span
        class="counter"
        class:warn={counterWarn}
        class:over={counterOver}
      >
        {#if counterOver}
          Over radio limit ({APRS_LIMIT} max)
        {:else if showPartBadge}
          Part {parts}/{parts}
        {:else}
          {remaining}
        {/if}
      </span>
      <span class="sr-only" role="status" aria-live="polite">{counterAnnouncement}</span>
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

  /* Long-mode pill mirrors `.to-pill` geometry but uses warning tint
     (amber family) rather than primary — a soft heads-up that the
     composer is in advanced mode, not an error state. */
  .long-mode-pill {
    display: inline-flex;
    align-items: center;
    padding: 2px 8px;
    background: var(--color-warning-muted);
    color: var(--color-warning);
    border: 1px solid var(--color-warning);
    border-radius: 999px;
    font-size: 10px;
    font-family: var(--font-mono);
    font-weight: 700;
    letter-spacing: 0.5px;
    text-transform: uppercase;
    white-space: nowrap;
  }

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

  .sr-only {
    position: absolute;
    width: 1px;
    height: 1px;
    padding: 0;
    margin: -1px;
    overflow: hidden;
    clip: rect(0, 0, 0, 0);
    white-space: nowrap;
    border: 0;
  }

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
