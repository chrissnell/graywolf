<script>
  // Callsign autocomplete for the compose dialog.
  //
  // We hand-roll the input + dropdown instead of bending chonky's
  // <Combobox>: that composable's bindings are `value` and `open`
  // only, with no filter/onInput hook for server-fed results and no
  // aria-activedescendant override — things we need for the
  // plan-specified "highlight best station, not first bot" rule and
  // for the `(new)` free-form entry. Keyboard nav, aria-combobox
  // roles, and focus management are implemented explicitly so the
  // component satisfies WCAG 1.1.1 / 4.1.2 on its own terms.
  //
  // Plan details honored:
  //   - 150 ms debounce against /stations/autocomplete
  //   - uppercase input as user types
  //   - two groups: "APRS Services" (source=bot) curated order,
  //     "Stations" (cache | history | cache+history) ranked
  //   - initial-highlight: best-ranked station, NOT first bot
  //   - free-form `(new)` entry when input matches no row
  //   - Enter commits highlighted; unknown strings allowed

  import { onMount } from 'svelte';
  import { Icon } from '@chrissnell/chonky-ui';
  import { autocompleteStations } from '../../api/messages.js';
  import { relativeLong } from './time.js';

  /** @type {{
   *    value?: string,
   *    placeholder?: string,
   *    onCommit?: (call: string) => void,
   *    disabled?: boolean,
   *    autofocus?: boolean,
   *  }}
   */
  let {
    value = $bindable(''),
    placeholder = 'Callsign, tactical, or APRS service',
    onCommit,
    disabled = false,
    autofocus = false,
  } = $props();

  let inputEl = $state(null);
  let listId = 'cb-list-' + Math.random().toString(36).slice(2, 8);
  let open = $state(false);
  let results = $state([]);   // {callsign, source, last_heard, description}
  let highlight = $state(-1);
  let debounceTimer = null;

  // Popover positioning: the listbox is rendered with `position: fixed`
  // and manually pinned to the input's bounding rect so it escapes any
  // ancestor `overflow: auto` (e.g., chonky's `.modal-body`). Updated on
  // open, scroll, resize, and when results change (height can shift).
  let popTop = $state(0);
  let popLeft = $state(0);
  let popWidth = $state(0);
  let popFlipAbove = $state(false);

  function updatePopoverPosition() {
    if (!inputEl) return;
    const r = inputEl.getBoundingClientRect();
    const vh = window.innerHeight || document.documentElement.clientHeight;
    const spaceBelow = vh - r.bottom;
    const spaceAbove = r.top;
    const maxListH = 320;
    const flip = spaceBelow < 160 && spaceAbove > spaceBelow;
    popFlipAbove = flip;
    popLeft = r.left;
    popWidth = r.width;
    if (flip) {
      // Pin bottom edge to just above the input.
      popTop = Math.max(8, r.top - Math.min(maxListH, spaceAbove - 8) - 4);
    } else {
      popTop = r.bottom + 4;
    }
  }

  $effect(() => {
    if (!open) return;
    updatePopoverPosition();
    const onScrollOrResize = () => updatePopoverPosition();
    window.addEventListener('scroll', onScrollOrResize, true);
    window.addEventListener('resize', onScrollOrResize);
    return () => {
      window.removeEventListener('scroll', onScrollOrResize, true);
      window.removeEventListener('resize', onScrollOrResize);
    };
  });

  // Re-measure when results change — the list can grow or shrink which
  // affects flip decisions.
  $effect(() => {
    // Read results.length so this effect tracks it reactively.
    // eslint-disable-next-line no-unused-expressions
    results.length;
    if (open) updatePopoverPosition();
  });

  // Upper-case at the input layer so the dropdown sees normalized
  // queries. This is what every APRS UI does.
  function onInput(e) {
    const raw = e.target.value || '';
    value = raw.toUpperCase();
    // Keep the DOM input in sync with the normalized value.
    if (e.target.value !== value) e.target.value = value;
    open = true;
    clearTimeout(debounceTimer);
    debounceTimer = setTimeout(runFetch, 150);
  }

  async function runFetch() {
    const q = (value || '').trim();
    try {
      const res = await autocompleteStations({ q, limit: 20 });
      results = Array.isArray(res) ? res : [];
      // Initial highlight: pick the best station (non-bot) if any,
      // else the first row. Bots remain visible at the top of the
      // rendered list, but highlight defaults to stations so the
      // operator's most common intent (typing their correspondent's
      // prefix) commits on Enter.
      const stationIdx = results.findIndex(r => r.source !== 'bot');
      highlight = stationIdx >= 0 ? stationIdx : (results.length > 0 ? 0 : -1);
    } catch {
      results = [];
      highlight = -1;
    }
  }

  // Group results for rendering. Bots first (curated order from
  // server), stations second (server-ranked). Order within each
  // group preserved as returned.
  const groups = $derived.by(() => {
    const bots = [];
    const stations = [];
    for (const r of results) {
      if (r.source === 'bot') bots.push(r);
      else stations.push(r);
    }
    const list = [];
    if (bots.length > 0)     list.push({ heading: 'APRS Services', items: bots });
    if (stations.length > 0) list.push({ heading: 'Stations', items: stations });
    return list;
  });

  // Flat index so keyboard arrow navigation maps to a single row.
  const flatItems = $derived.by(() => {
    const arr = [];
    for (const g of groups) for (const r of g.items) arr.push(r);
    return arr;
  });

  function commit(idx) {
    const chosen = idx >= 0 ? flatItems[idx] : null;
    const raw = (value || '').trim();
    const call = chosen ? (chosen.callsign || '') : raw;
    if (!call) return;
    value = call.toUpperCase();
    if (inputEl) inputEl.value = value;
    open = false;
    onCommit?.(value);
  }

  function onKeyDown(e) {
    if (!open && (e.key === 'ArrowDown' || e.key === 'ArrowUp')) {
      open = true;
      runFetch();
      e.preventDefault();
      return;
    }
    if (!open) return;
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      highlight = Math.min(flatItems.length - 1, highlight + 1);
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      highlight = Math.max(0, highlight - 1);
    } else if (e.key === 'Enter') {
      e.preventDefault();
      commit(highlight);
    } else if (e.key === 'Escape') {
      open = false;
    }
  }

  function onFocusIn() {
    open = true;
    if (results.length === 0) runFetch();
  }
  function onBlurOut(e) {
    // Delay so a click on an option isn't dropped by the blur.
    setTimeout(() => {
      if (!document.activeElement || !document.activeElement.closest?.('[data-combobox="' + listId + '"]')) {
        open = false;
      }
    }, 120);
  }

  onMount(() => {
    if (autofocus && inputEl) {
      inputEl.focus();
    }
  });

  // Composed activedescendant id for a11y:
  const activeId = $derived(highlight >= 0 && flatItems[highlight]
    ? `${listId}-opt-${highlight}`
    : undefined);
</script>

<div class="wrap" data-combobox={listId}>
  <div class="input-wrap">
    <span class="lead" aria-hidden="true"><Icon name="search" size="sm" /></span>
    <input
      bind:this={inputEl}
      type="text"
      class="input"
      role="combobox"
      {value}
      {placeholder}
      {disabled}
      aria-controls={listId}
      aria-expanded={open}
      aria-autocomplete="list"
      aria-activedescendant={activeId}
      autocomplete="off"
      autocapitalize="characters"
      spellcheck="false"
      oninput={onInput}
      onfocus={onFocusIn}
      onblur={onBlurOut}
      onkeydown={onKeyDown}
      data-testid="callsign-autocomplete-input"
    />
  </div>
  {#if open && (flatItems.length > 0 || (value && value.trim()))}
    <ul
      id={listId}
      role="listbox"
      class="list"
      class:flip-above={popFlipAbove}
      style:top="{popTop}px"
      style:left="{popLeft}px"
      style:width="{popWidth}px"
      data-combobox={listId}
      data-testid="callsign-autocomplete-list"
    >
      {#each groups as g}
        <li role="presentation" class="group-heading">{g.heading}</li>
        {#each g.items as r, i}
          {@const flatIdx = flatItems.indexOf(r)}
          <li
            id={`${listId}-opt-${flatIdx}`}
            role="option"
            class="item"
            class:active={flatIdx === highlight}
            aria-selected={flatIdx === highlight}
            onmousedown={(e) => { e.preventDefault(); commit(flatIdx); }}
            onmouseenter={() => highlight = flatIdx}
          >
            <span class="item-lead" aria-hidden="true">
              <Icon name={r.source === 'bot' ? 'bot' : 'user'} size="sm" />
            </span>
            <div class="item-body">
              <span class="item-call">{r.callsign}</span>
              {#if r.source === 'bot' && r.description}
                <span class="item-desc">{r.description}</span>
              {:else if r.last_heard}
                <span class="item-desc">heard {relativeLong(r.last_heard)}</span>
              {/if}
            </div>
          </li>
        {/each}
      {/each}
      {#if value && value.trim() && !flatItems.some(r => (r.callsign || '').toUpperCase() === value.trim().toUpperCase())}
        <li role="presentation" class="group-heading">Or send to</li>
        <li
          role="option"
          id={`${listId}-opt-new`}
          class="item new"
          aria-selected={highlight === -2}
          onmousedown={(e) => { e.preventDefault(); commit(-1); }}
        >
          <span class="item-lead" aria-hidden="true"><Icon name="send" size="sm" /></span>
          <div class="item-body">
            <span class="item-call">{value.trim().toUpperCase()}</span>
            <span class="item-desc">(new)</span>
          </div>
        </li>
      {/if}
    </ul>
  {/if}
</div>

<style>
  .wrap {
    position: relative;
    width: 100%;
  }
  .input-wrap {
    position: relative;
    display: flex;
    align-items: center;
  }
  .lead {
    position: absolute;
    left: 8px;
    top: 50%;
    transform: translateY(-50%);
    display: inline-flex;
    color: var(--color-text-dim);
    pointer-events: none;
    z-index: 1;
  }
  .input {
    width: 100%;
    padding: 7px 8px 7px 28px;
    background: var(--color-bg);
    border: 1px solid var(--color-border);
    border-radius: var(--radius);
    color: var(--color-text);
    font-family: var(--font-mono);
    font-size: 14px;
    letter-spacing: 0.5px;
  }
  .input:focus {
    outline: none;
    border-color: var(--color-primary);
    box-shadow: 0 0 0 2px var(--color-primary-muted);
  }
  .list {
    /* position:fixed so the listbox escapes ancestor `overflow: auto`
       (chonky's .modal-body clips absolutely-positioned descendants).
       Top/left/width are set inline from the input's bounding rect;
       updated on scroll/resize via the $effect in <script>. */
    position: fixed;
    max-height: 320px;
    overflow-y: auto;
    list-style: none;
    padding: 4px 0;
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: var(--radius);
    box-shadow: 0 4px 16px rgba(0, 0, 0, 0.4);
    /* z-index high enough to sit above chonky Modal's backdrop + content. */
    z-index: 1000;
  }
  .group-heading {
    padding: 6px 12px 4px;
    font-size: 10px;
    font-weight: 700;
    letter-spacing: 1px;
    text-transform: uppercase;
    color: var(--color-text-dim);
  }
  .item {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 12px;
    cursor: pointer;
  }
  .item:hover, .item.active {
    background: var(--color-surface-raised);
  }
  .item-lead {
    color: var(--color-text-muted);
    display: inline-flex;
  }
  .item.new .item-lead { color: var(--color-primary); }
  .item-body {
    display: flex;
    flex-direction: column;
    gap: 1px;
    min-width: 0;
  }
  .item-call {
    font-family: var(--font-mono);
    font-weight: 600;
    color: var(--color-text);
    letter-spacing: 0.5px;
  }
  .item-desc {
    font-size: 11px;
    color: var(--color-text-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
</style>
