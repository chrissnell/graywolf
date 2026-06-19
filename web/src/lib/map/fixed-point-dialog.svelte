<script>
  // Dialog for adding a user-defined fixed map point. Opened from the map's
  // right-click context menu with the clicked lat/lon. Collects a name and
  // an APRS symbol (reusing the SymbolPicker so the icon vocabulary matches
  // station/beacon markers), then hands the result back via onConfirm.

  import { Button } from '@chrissnell/chonky-ui';
  import Modal from '../../components/Modal.svelte';
  import SymbolPicker from '../../components/SymbolPicker.svelte';
  import { createAprsIconElement } from './aprs-icon-element.js';
  import { fmtLat, fmtLon } from './popup-helpers.js';

  let {
    open = $bindable(false),
    lat = 0,
    lon = 0,
    onConfirm = undefined,
  } = $props();

  let name = $state('');
  let table = $state('/');
  let symbol = $state('.');
  let overlay = $state('');
  let pickerOpen = $state(false);

  // Reset the working fields each time the dialog opens so a prior entry
  // doesn't bleed into the next point. Default '//' is the APRS "Red dot",
  // a neutral, clearly-visible generic waypoint.
  $effect(() => {
    if (open) {
      name = '';
      table = '/';
      symbol = '/';
      overlay = '';
    }
  });

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

  function confirm() {
    // Name is required server-side; guard here so Enter / the button can't
    // fire an empty-name request that would only come back as a 400 toast.
    if (!name.trim()) return;
    onConfirm?.({ name: name.trim(), table, symbol, overlay });
    open = false;
  }

  function cancel() {
    open = false;
  }
</script>

<Modal bind:open title="Add fixed point">
  <div class="fp-form">
    <label class="fp-field">
      <span class="fp-label">Name</span>
      <!-- svelte-ignore a11y_autofocus -->
      <input
        class="fp-input"
        type="text"
        bind:value={name}
        placeholder="e.g. Aid Station 3"
        maxlength="40"
        autofocus
        onkeydown={(e) => e.key === 'Enter' && confirm()}
      />
    </label>

    <div class="fp-field">
      <span class="fp-label">Icon</span>
      <div class="fp-icon-row">
        <div class="fp-icon-preview" use:iconPreview={{ table, symbol, overlay }}></div>
        <Button onclick={() => (pickerOpen = true)}>Choose icon…</Button>
      </div>
    </div>

    <div class="fp-coords">{fmtLat(lat)} {fmtLon(lon)}</div>

    <div class="fp-actions">
      <Button onclick={cancel}>Cancel</Button>
      <Button variant="primary" onclick={confirm} disabled={!name.trim()}>Add point</Button>
    </div>
  </div>
</Modal>

<SymbolPicker bind:open={pickerOpen} bind:table bind:symbol bind:overlay />

<style>
  .fp-form {
    display: flex;
    flex-direction: column;
    gap: 14px;
    min-width: 320px;
  }
  .fp-field {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .fp-label {
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 1px;
    color: var(--color-text-muted);
  }
  .fp-input {
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
  .fp-input:focus {
    outline: none;
    border-color: var(--color-primary, #4a9eff);
  }
  .fp-icon-row {
    display: flex;
    align-items: center;
    gap: 12px;
  }
  .fp-icon-preview {
    width: 28px;
    height: 28px;
    flex: 0 0 auto;
  }
  .fp-coords {
    font-family: var(--font-mono);
    font-size: 12px;
    color: var(--color-text-muted);
  }
  .fp-actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    margin-top: 4px;
  }
</style>
