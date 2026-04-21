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

  // Typeahead buffer.
  let typeBuf = $state('');
  let typeTimer = null;
  function pushTypeahead(ch) {
    typeBuf += ch.toLowerCase();
    if (typeTimer) clearTimeout(typeTimer);
    typeTimer = setTimeout(() => {
      typeBuf = '';
    }, 500);
    // Find first option whose name starts with the buffer.
    const idx = channels.findIndex((c) =>
      (c.name || '').toLowerCase().startsWith(typeBuf),
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
    // Start focus on the currently selected row, or the first row.
    activeIdx = currentIdx >= 0 ? currentIdx : 0;
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
        activeIdx = Math.min(channels.length - 1, activeIdx + 1);
        scrollActiveIntoView();
        break;
      case 'ArrowUp':
        ev.preventDefault();
        activeIdx = Math.max(0, activeIdx - 1);
        scrollActiveIntoView();
        break;
      case 'Home':
        ev.preventDefault();
        activeIdx = 0;
        scrollActiveIntoView();
        break;
      case 'End':
        ev.preventDefault();
        activeIdx = channels.length - 1;
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
        <li class="empty" role="option" aria-disabled="true">
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
            role="option"
            aria-selected={currentIdx === idx}
            aria-label={rowAriaLabel(c)}
            onmouseenter={() => (activeIdx = idx)}
            onclick={() => commit(idx)}
          >
            <ChannelOption channel={c} />
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
