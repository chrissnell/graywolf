<script>
  import { onMount } from 'svelte';
  import { Button, Select } from '@chrissnell/chonky-ui';
  import Modal from './Modal.svelte';
  import {
    CELL_PX, COLS, ROWS,
    PRIMARY_TABLE, ALTERNATE_TABLE,
    SPRITE_URLS,
    FIRST_SYMBOL_CODE, LAST_SYMBOL_CODE,
    OVERLAY_CHARS,
    backgroundPosition, loadSymbols, describe,
  } from '../lib/aprsSymbols.js';

  const overlayOptions = [
    { value: '', label: '(none)' },
    ...OVERLAY_CHARS.split('').map((c) => ({ value: c, label: c })),
  ];

  let {
    open = $bindable(false),
    table = $bindable('/'),
    symbol = $bindable('-'),
    overlay = $bindable(''),
    onConfirm = undefined,
  } = $props();

  // Local working copy so cancel discards.
  let workTable = $state(table);
  let workSymbol = $state(symbol);
  let workOverlay = $state(overlay);
  let symbols = $state(null);

  $effect(() => {
    if (open) {
      workTable = table || PRIMARY_TABLE;
      workSymbol = symbol || '-';
      workOverlay = overlay || '';
    }
  });

  onMount(async () => {
    symbols = await loadSymbols();
  });

  // The sprite holds 94 printable APRS symbols starting at '!' (0x21).
  // The last printable '~' lives at row 5, col 13; cells 14 and 15 of
  // row 5 are unused — pad them with null so the grid stays rectangular.
  const cells = (() => {
    const out = [];
    for (let code = FIRST_SYMBOL_CODE; code <= LAST_SYMBOL_CODE; code++) {
      out.push(String.fromCharCode(code));
    }
    while (out.length < COLS * ROWS) out.push(null);
    return out;
  })();

  function selectCell(c) {
    if (!c) return;
    workSymbol = c;
  }

  function selectTable(t) {
    workTable = t;
    if (t === PRIMARY_TABLE) workOverlay = '';
  }

  function confirm() {
    table = workTable;
    symbol = workSymbol;
    overlay = workOverlay;
    onConfirm?.({ table: workTable, symbol: workSymbol, overlay: workOverlay });
    open = false;
  }

  function cancel() {
    open = false;
  }

  // Tooltip for the currently-hovered or selected cell.
  let hoveredChar = $state(null);
  $effect(() => {
    if (!open) hoveredChar = null;
  });

  let activeChar = $derived(hoveredChar || workSymbol);
  let activeLabel = $derived(describe(symbols, workTable, activeChar));
</script>

<Modal bind:open title="Select APRS Symbol">
  <div class="picker">
    <div class="tabs" role="tablist">
      <button
        type="button"
        role="tab"
        class:active={workTable === PRIMARY_TABLE}
        onclick={() => selectTable(PRIMARY_TABLE)}
      >
        Primary
      </button>
      <button
        type="button"
        role="tab"
        class:active={workTable === ALTERNATE_TABLE}
        onclick={() => selectTable(ALTERNATE_TABLE)}
      >
        Alternate
      </button>
    </div>

    <div
      class="grid"
      style="--cell: {CELL_PX}px; --cols: {COLS}; --rows: {ROWS};"
    >
      {#each cells as c, i (i)}
        {#if c === null}
          <div class="cell empty" aria-hidden="true"></div>
        {:else}
          {@const isSelected = c === workSymbol}
          {@const label = describe(symbols, workTable, c)}
          <button
            type="button"
            class="cell"
            class:selected={isSelected}
            class:unnamed={!label}
            title={label || ''}
            aria-label={label || `symbol ${c}`}
            onclick={() => selectCell(c)}
            onmouseenter={() => hoveredChar = c}
            onmouseleave={() => hoveredChar === c && (hoveredChar = null)}
            style="background-image: url({SPRITE_URLS[workTable]}); background-position: {backgroundPosition(c, CELL_PX)};"
          ></button>
        {/if}
      {/each}
    </div>

    <div class="info">
      <div
        class="preview"
        style="background-image: url({SPRITE_URLS[workTable]}); background-position: {backgroundPosition(workSymbol, CELL_PX * 2)}; background-size: {COLS * CELL_PX * 2}px {ROWS * CELL_PX * 2}px;"
      >
        {#if workOverlay && workTable === ALTERNATE_TABLE}
          <span class="overlay-char">{workOverlay}</span>
        {/if}
      </div>
      <div class="info-label">{activeLabel || '\u00a0'}</div>
    </div>

    {#if workTable === ALTERNATE_TABLE}
      <div class="overlay-row">
        <label for="sym-overlay">Overlay character</label>
        <Select
          id="sym-overlay"
          class="overlay-select"
          bind:value={workOverlay}
          options={overlayOptions}
          placeholder="(none)"
        />
        <span class="overlay-hint">Optional A&ndash;Z or 0&ndash;9.</span>
      </div>
    {/if}

    <div class="actions">
      <Button onclick={cancel}>Cancel</Button>
      <Button variant="primary" onclick={confirm}>Use Symbol</Button>
    </div>
  </div>
</Modal>

<style>
  .picker {
    display: flex;
    flex-direction: column;
    gap: 12px;
    min-width: 420px;
  }
  .tabs {
    display: flex;
    gap: 4px;
    border-bottom: 1px solid var(--color-border);
  }
  .tabs button {
    background: transparent;
    border: none;
    padding: 8px 14px;
    font-size: 13px;
    color: var(--color-text-dim, #888);
    cursor: pointer;
    border-bottom: 2px solid transparent;
    margin-bottom: -1px;
  }
  .tabs button:hover {
    color: var(--color-text, #ddd);
  }
  .tabs button.active {
    color: var(--color-text, #ddd);
    border-bottom-color: var(--color-primary, #4a9eff);
  }

  .grid {
    display: grid;
    grid-template-columns: repeat(var(--cols), var(--cell));
    grid-template-rows: repeat(var(--rows), var(--cell));
    gap: 2px;
    padding: 4px;
    background: #fff;
    border: 1px solid var(--color-border);
    border-radius: 4px;
    width: max-content;
  }
  .cell {
    width: var(--cell);
    height: var(--cell);
    padding: 0;
    border: 1px solid transparent;
    border-radius: 2px;
    background-color: transparent;
    background-repeat: no-repeat;
    cursor: pointer;
    transition: background-color 60ms, border-color 60ms;
  }
  .cell:hover {
    background-color: rgba(0, 0, 0, 0.08);
    border-color: var(--color-border);
  }
  .cell.selected {
    background-color: rgba(74, 158, 255, 0.25);
    border-color: var(--color-primary, #4a9eff);
  }
  .cell.empty {
    pointer-events: none;
  }
  .cell.unnamed {
    opacity: 0.45;
  }
  .cell.unnamed.selected,
  .cell.unnamed:hover {
    opacity: 1;
  }

  .info {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 4px 0;
  }
  .preview {
    width: 48px;
    height: 48px;
    background-repeat: no-repeat;
    border: 1px solid var(--color-border);
    border-radius: 4px;
    background-color: #fff;
    position: relative;
    flex: 0 0 auto;
  }
  .overlay-char {
    position: absolute;
    inset: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    font-family: ui-monospace, SFMono-Regular, monospace;
    font-size: 18px;
    font-weight: 700;
    line-height: 1;
    color: #000;
    text-shadow: 0 0 2px #fff, 0 0 2px #fff, 0 0 2px #fff;
    pointer-events: none;
  }
  .info-label {
    font-size: 14px;
    color: var(--color-text, #ddd);
    min-height: 18px;
  }

  .overlay-row {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 8px 0 4px;
  }
  .overlay-row label {
    font-size: 13px;
    color: var(--color-text);
  }
  .overlay-row :global(.overlay-select) {
    min-width: 88px;
  }
  .overlay-hint {
    font-size: 12px;
    color: var(--color-text-muted, #888);
  }

  .actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    margin-top: 4px;
  }
</style>
