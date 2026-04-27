<script>
  // ChannelListbox — accessible, mobile-first channel picker that
  // replaces plain <Select> on Beacons, Digipeater, iGate, and Kiss
  // pages. The native <select> can't render a two-line option row
  // with a glyph column, truncates unreadably on phone widths, and
  // doesn't expose backing state at all.
  //
  // Conforms to the ARIA 1.2 combobox + listbox pattern:
  //   - The trigger has role="combobox", aria-expanded, aria-controls.
  //   - The popup has role="listbox".
  //   - Each row has role="option", aria-selected, and a stable id so
  //     aria-activedescendant can point at it during arrow-key nav.
  //   - Typeahead is supported via printable-key buffer with 500ms
  //     reset.
  //
  // Prop surface (uniform across Beacons/Digipeater/iGate/Kiss):
  //   - value: the currently selected channel id.
  //   - valueType: 'string' | 'number' — iGate uses integer
  //     form.tx_channel; every other page uses a stringified form.
  //     The component coerces on both read and write so either page
  //     works without changing its form shape.
  //   - channels: the list of channel objects (already enriched with
  //     backing by the shared store).
  //   - id: DOM id applied to the trigger, for <label for=...>.
  //   - disabled: match the underlying form-control disabled rules.
  //   - ariaLabel / ariaLabelledBy: fall-through accessibility hooks.
  //   - onChange: optional callback fired after selection.
  //   - capabilityFilter: (channel) => { ok, reason } predicate that
  //     disables (does not hide) options that fail the check. Phase 2
  //     uses this with `txPredicate` so non-TX-capable channels show
  //     up in the list — the operator editing an existing broken row
  //     needs to see their previous choice and why it's invalid — but
  //     can't be selected, are skipped in keyboard nav, and are
  //     excluded from typeahead. Default: always-ok.
  //
  // The listbox is controlled: parent owns `value`, we call bindable.

  import ChannelOption from './ChannelOption.svelte';
  import { ariaLabel as rowAriaLabel } from '../channelBacking.js';

  let {
    value = $bindable(),
    valueType = 'string',
    channels = [],
    id = 'channel-listbox',
    disabled = false,
    ariaLabel = undefined,
    ariaLabelledBy = undefined,
    placeholder = 'Select a channel',
    onChange = undefined,
    capabilityFilter = () => ({ ok: true, reason: '' }),
  } = $props();

  // Coerce value for comparison. Native handling: accept both string
  // and number, match on numeric equality.
  function asNumber(v) {
    if (v === '' || v == null) return null;
    const n = typeof v === 'string' ? parseInt(v, 10) : v;
    return Number.isFinite(n) ? n : null;
  }

  function emitValue(n) {
    if (n == null) return null;
    return valueType === 'number' ? n : String(n);
  }

  let open = $state(false);
  let activeIdx = $state(-1);
  let triggerEl = $state(null);
  let listEl = $state(null);

  // Precompute the capability verdict per option so every consumer
  // (render, nav, typeahead, commit, aria) reads the same verdict —
  // no drift, no re-evaluating in each code path.
  let capability = $derived(
    channels.map((c) => {
      const v = capabilityFilter(c) || { ok: true, reason: '' };
      return { ok: !!v.ok, reason: v.reason || '' };
    }),
  );

  function isEnabled(idx) {
    return !!capability[idx]?.ok;
  }

  // Step the active row in `dir` (±1) skipping disabled options.
  // Returns the next enabled index in that direction, or `fromIdx`
  // unchanged if every option ahead is disabled (so the caret doesn't
  // wrap past the ends and doesn't jump into a dead cell).
  function stepIdx(fromIdx, dir) {
    if (channels.length === 0) return fromIdx;
    let i = fromIdx + dir;
    while (i >= 0 && i < channels.length) {
      if (isEnabled(i)) return i;
      i += dir;
    }
    return fromIdx;
  }

  // First / last enabled index for Home / End. Falls back to the
  // current activeIdx if every option is disabled.
  function firstEnabledIdx() {
    for (let i = 0; i < channels.length; i += 1) {
      if (isEnabled(i)) return i;
    }
    return activeIdx;
  }
  function lastEnabledIdx() {
    for (let i = channels.length - 1; i >= 0; i -= 1) {
      if (isEnabled(i)) return i;
    }
    return activeIdx;
  }

  // Typeahead buffer.
  let typeBuf = $state('');
  let typeTimer = null;
  function pushTypeahead(ch) {
    typeBuf += ch.toLowerCase();
    if (typeTimer) clearTimeout(typeTimer);
    typeTimer = setTimeout(() => {
      typeBuf = '';
    }, 500);
    // Find first enabled option whose name starts with the buffer.
    // Disabled options are excluded: typeahead should land the user
    // on something they can actually commit.
    const idx = channels.findIndex(
      (c, i) => isEnabled(i) && (c.name || '').toLowerCase().startsWith(typeBuf),
    );
    if (idx !== -1) {
      activeIdx = idx;
      scrollActiveIntoView();
    }
  }

  let currentIdx = $derived.by(() => {
    const n = asNumber(value);
    if (n == null) return -1;
    return channels.findIndex((c) => c.id === n);
  });
  let selectedChannel = $derived(
    currentIdx >= 0 ? channels[currentIdx] : null,
  );

  function openList() {
    if (disabled) return;
    open = true;
    // Start focus on the currently selected row if it's enabled,
    // otherwise the first enabled row, otherwise the first row (so
    // the caret is somewhere even if every option is broken).
    if (currentIdx >= 0 && isEnabled(currentIdx)) {
      activeIdx = currentIdx;
    } else {
      const f = firstEnabledIdx();
      activeIdx = f >= 0 ? f : (channels.length > 0 ? 0 : -1);
    }
    // Scroll into view after the popup mounts.
    queueMicrotask(scrollActiveIntoView);
  }

  function closeList(opts = {}) {
    open = false;
    typeBuf = '';
    if (opts.focusTrigger && triggerEl) triggerEl.focus();
  }

  function commit(idx) {
    const c = channels[idx];
    if (!c) return;
    // Disabled options cannot be committed. No state change, no
    // onchange fire — just bail. The trigger stays open so the user
    // can pick a different row.
    if (!isEnabled(idx)) return;
    value = emitValue(c.id);
    onChange?.(c);
    closeList({ focusTrigger: true });
  }

  function onTriggerKey(ev) {
    if (disabled) return;
    switch (ev.key) {
      case 'ArrowDown':
      case 'ArrowUp':
      case 'Enter':
      case ' ':
        ev.preventDefault();
        openList();
        break;
      case 'Escape':
        if (open) {
          ev.preventDefault();
          closeList({ focusTrigger: true });
        }
        break;
      default:
        if (ev.key.length === 1 && !ev.ctrlKey && !ev.metaKey && !ev.altKey) {
          if (!open) openList();
          pushTypeahead(ev.key);
        }
    }
  }

  function onListKey(ev) {
    if (!open) return;
    switch (ev.key) {
      case 'ArrowDown':
        ev.preventDefault();
        activeIdx = stepIdx(activeIdx, 1);
        scrollActiveIntoView();
        break;
      case 'ArrowUp':
        ev.preventDefault();
        activeIdx = stepIdx(activeIdx, -1);
        scrollActiveIntoView();
        break;
      case 'Home':
        ev.preventDefault();
        activeIdx = firstEnabledIdx();
        scrollActiveIntoView();
        break;
      case 'End':
        ev.preventDefault();
        activeIdx = lastEnabledIdx();
        scrollActiveIntoView();
        break;
      case 'Enter':
      case ' ':
        ev.preventDefault();
        if (activeIdx >= 0) commit(activeIdx);
        break;
      case 'Escape':
      case 'Tab':
        ev.preventDefault();
        closeList({ focusTrigger: true });
        break;
      default:
        if (ev.key.length === 1 && !ev.ctrlKey && !ev.metaKey && !ev.altKey) {
          pushTypeahead(ev.key);
        }
    }
  }

  function scrollActiveIntoView() {
    if (!listEl || activeIdx < 0) return;
    const row = listEl.querySelector(`[data-idx="${activeIdx}"]`);
    if (row && typeof row.scrollIntoView === 'function') {
      row.scrollIntoView({ block: 'nearest' });
    }
  }

  function onDocMouseDown(ev) {
    if (!open) return;
    const tgt = ev.target;
    if (triggerEl?.contains(tgt) || listEl?.contains(tgt)) return;
    closeList();
  }

  $effect(() => {
    if (open) {
      document.addEventListener('mousedown', onDocMouseDown);
      return () => document.removeEventListener('mousedown', onDocMouseDown);
    }
  });

  function optionId(idx) {
    return `${id}-opt-${idx}`;
  }

  let activeDescendant = $derived(
    open && activeIdx >= 0 ? optionId(activeIdx) : undefined,
  );

  // Build a per-option aria-label that includes the reason when the
  // option is disabled so screen readers announce e.g. "Channel 3,
  // VHF, no output device configured, unavailable" via
  // aria-activedescendant during keyboard nav.
  function optionAriaLabel(c, idx) {
    const base = rowAriaLabel(c);
    const cap = capability[idx];
    if (cap && !cap.ok) {
      const r = cap.reason ? cap.reason + ', ' : '';
      return `${base}, ${r}unavailable`;
    }
    return base;
  }
</script>

<div class="channel-listbox" class:disabled>
  <button
    bind:this={triggerEl}
    type="button"
    {id}
    class="trigger"
    role="combobox"
    aria-haspopup="listbox"
    aria-expanded={open}
    aria-controls={`${id}-list`}
    aria-activedescendant={activeDescendant}
    aria-label={ariaLabel}
    aria-labelledby={ariaLabelledBy}
    aria-disabled={disabled ? 'true' : undefined}
    tabindex={disabled ? -1 : 0}
    onclick={() => (open ? closeList() : openList())}
    onkeydown={onTriggerKey}
  >
    {#if selectedChannel}
      <ChannelOption channel={selectedChannel} variant="trigger-compact" />
    {:else}
      <span class="placeholder">{placeholder}</span>
    {/if}
    <span class="chev" aria-hidden="true">{open ? '▴' : '▾'}</span>
  </button>

  {#if open}
    <ul
      bind:this={listEl}
      id={`${id}-list`}
      class="list"
      role="listbox"
      tabindex="-1"
      onkeydown={onListKey}
    >
      {#if channels.length === 0}
        <li class="empty" role="option" aria-selected="false" aria-disabled="true">
          No channels configured
        </li>
      {:else}
        {#each channels as c, idx (c.id)}
          <!-- Keyboard handling for options is centralised on the
               <ul role="listbox"> above via onListKey — Enter commits
               the active option. The svelte-a11y lint doesn't see
               the parent handler, so suppress the per-row check. -->
          <!-- svelte-ignore a11y_click_events_have_key_events -->
          <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
          <li
            id={optionId(idx)}
            data-idx={idx}
            class="option"
            class:active={activeIdx === idx}
            class:selected={currentIdx === idx}
            class:unavailable={!capability[idx]?.ok}
            role="option"
            aria-selected={currentIdx === idx}
            aria-disabled={!capability[idx]?.ok ? 'true' : undefined}
            aria-label={optionAriaLabel(c, idx)}
            onmouseenter={() => {
              // Honour the skip-in-nav rule on mouse hover too, so
              // arrow-key position doesn't teleport to a disabled row
              // on accidental mouse movement.
              if (isEnabled(idx)) activeIdx = idx;
            }}
            onclick={() => commit(idx)}
          >
            <ChannelOption
              channel={c}
              unavailable={!capability[idx]?.ok}
              unavailableReason={capability[idx]?.reason ?? ''}
            />
          </li>
        {/each}
      {/if}
    </ul>
  {/if}
</div>

<style>
  .channel-listbox {
    position: relative;
    width: 100%;
  }
  .trigger {
    display: flex;
    align-items: center;
    justify-content: space-between;
    width: 100%;
    min-height: 38px;
    gap: 8px;
    padding: 6px 10px;
    background: var(--bg-input, var(--bg-secondary));
    color: var(--text-primary);
    border: 1px solid var(--border-color);
    border-radius: var(--radius, 4px);
    font: inherit;
    text-align: left;
    cursor: pointer;
  }
  .trigger:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }
  .channel-listbox.disabled .trigger {
    opacity: 0.6;
    cursor: not-allowed;
  }
  .placeholder {
    color: var(--text-muted);
  }
  .chev {
    color: var(--text-muted);
    flex-shrink: 0;
    line-height: 1;
  }
  .list {
    position: absolute;
    z-index: 50;
    top: calc(100% + 4px);
    left: 0;
    right: 0;
    list-style: none;
    margin: 0;
    padding: 4px 0;
    max-height: 320px;
    overflow-y: auto;
    background: var(--bg-secondary);
    border: 1px solid var(--border-color);
    border-radius: var(--radius, 4px);
    box-shadow: 0 6px 20px rgba(0, 0, 0, 0.25);
  }
  .option {
    padding: 6px 10px;
    cursor: pointer;
  }
  .option.active,
  .option:hover {
    background: var(--bg-tertiary, rgba(255, 255, 255, 0.05));
  }
  .option.selected {
    background: var(--color-info-muted, rgba(70, 130, 255, 0.12));
  }
  /* Disabled-but-visible option styling. Matches chonky-ui's
     .listbox-item[aria-disabled="true"] token (opacity 0.4,
     cursor not-allowed) — but override the .option :hover /
     .option.active background so the row doesn't flash highlighted
     when the mouse drifts over it. The operator needs to see that
     the row is there (and why it's unselectable) without being
     invited to click it. */
  .option.unavailable {
    opacity: 0.55;
    cursor: default;
  }
  .option.unavailable:hover,
  .option.unavailable.active {
    background: transparent;
  }
  .empty {
    padding: 10px 12px;
    color: var(--text-muted);
    font-style: italic;
  }
  /* Mobile: full-width trigger + popup already. Tap targets a bit
     taller so phone users can hit them without fumbling. */
  @media (max-width: 600px) {
    .trigger {
      min-height: 44px;
    }
    .option {
      padding: 10px 12px;
    }
  }
</style>
