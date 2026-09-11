<script>
  // Dialog for dropping a Cursor-on-Target (CoT) at a map location.
  // Opened from the map's right-click context menu (first item in the
  // list) with the clicked lat/lon. Mirrors fixed-point-dialog.svelte's
  // shape, but the coordinates are read-only (the point IS the click
  // location -- there's nothing to correct) and Send fires the object
  // immediately via onConfirm rather than just adding a local marker.

  import { Button } from '@chrissnell/chonky-ui';
  import Modal from '../../components/Modal.svelte';
  import SymbolPicker from '../../components/SymbolPicker.svelte';
  import { createAprsIconElement } from './aprs-icon-element.js';

  let {
    open = $bindable(false),
    lat = 0,
    lon = 0,
    onConfirm = undefined,
  } = $props();

  let name = $state('');
  // "/D" is the confirmed default icon (primary table, symbol D).
  let table = $state('/');
  let symbol = $state('D');
  let overlay = $state('');
  let comment = $state('');
  let pickerOpen = $state(false);

  // Reset the working fields each time the dialog opens so a prior
  // entry doesn't bleed into the next drop.
  $effect(() => {
    if (open) {
      name = '';
      table = '/';
      symbol = 'D';
      overlay = '';
      comment = '';
    }
  });

  // Send enables once both operator-authored fields are non-empty; the
  // icon always has the "/D" default so it's never itself a blocker.
  let canSend = $derived(name.trim() !== '' && comment.trim() !== '');

  // Mount a live APRS icon preview into the bound container; re-render
  // whenever the chosen symbol changes.
  function iconPreview(node) {
    const render = () => {
      node.replaceChildren(
        createAprsIconElement({ table, symbol, overlay: overlay || null, displayPx: 28 }),
      );
    };
    render();
    return {
      update() {
        render();
      },
    };
  }

  function send() {
    if (!canSend) return;
    onConfirm?.({
      object_name: name.trim(),
      symbol_table: table,
      symbol,
      overlay,
      comment: comment.trim(),
      latitude: lat,
      longitude: lon,
    });
    open = false;
  }

  function cancel() {
    open = false;
  }
</script>

<Modal bind:open title="Add Cursor-on-Target">
  <div class="cot-form">
    <label class="cot-field">
      <span class="cot-label">Object Name</span>
      <!-- svelte-ignore a11y_autofocus -->
      <input
        class="cot-input"
        type="text"
        bind:value={name}
        placeholder="e.g. WOOFWOOF"
        maxlength="9"
        autofocus
        onkeydown={(e) => e.key === 'Enter' && send()}
      />
    </label>

    <div class="cot-field">
      <span class="cot-label">Icon</span>
      <div class="cot-icon-row">
        <div class="cot-icon-preview" use:iconPreview={{ table, symbol, overlay }}></div>
        <Button onclick={() => (pickerOpen = true)}>Choose icon…</Button>
      </div>
    </div>

    <label class="cot-field">
      <span class="cot-label">Comment</span>
      <input
        class="cot-input"
        type="text"
        bind:value={comment}
        placeholder="e.g. Aid station, water available"
        onkeydown={(e) => e.key === 'Enter' && send()}
      />
    </label>

    <div class="cot-coord-row">
      <label class="cot-field">
        <span class="cot-label">Latitude</span>
        <input class="cot-input" type="text" value={lat.toFixed(6)} disabled readonly />
      </label>
      <label class="cot-field">
        <span class="cot-label">Longitude</span>
        <input class="cot-input" type="text" value={lon.toFixed(6)} disabled readonly />
      </label>
    </div>

    <div class="cot-actions">
      <Button onclick={cancel}>Cancel</Button>
      <Button variant="primary" onclick={send} disabled={!canSend}>Send</Button>
    </div>
  </div>
</Modal>

<SymbolPicker bind:open={pickerOpen} bind:table bind:symbol bind:overlay />

<style>
  .cot-form {
    display: flex;
    flex-direction: column;
    gap: 14px;
    min-width: 320px;
  }
  .cot-field {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .cot-label {
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 1px;
    color: var(--color-text-muted);
  }
  .cot-input {
    width: 100%;
    box-sizing: border-box;
    background: var(--color-surface);
    color: var(--color-text);
    border: 1px solid var(--color-border);
    border-radius: 4px;
    font-family: var(--font-mono);
    font-size: 14px;
    padding: 8px 10px;
  }
  .cot-input:focus {
    outline: none;
    border-color: var(--color-primary, #4a9eff);
  }
  .cot-input:disabled {
    opacity: 0.65;
    cursor: not-allowed;
  }
  .cot-icon-row {
    display: flex;
    align-items: center;
    gap: 12px;
  }
  .cot-icon-preview {
    width: 28px;
    height: 28px;
    flex: 0 0 auto;
  }
  .cot-coord-row {
    display: flex;
    gap: 12px;
  }
  .cot-coord-row .cot-field {
    flex: 1 1 0;
    min-width: 0;
  }
  .cot-actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    margin-top: 4px;
  }
</style>
