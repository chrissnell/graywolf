<script>
  // Modal editor for the operator macro list. Macros persist as
  // {label, payload(base64)} on the server. The editor presents
  // payload as a plain-text textarea with backslash-escape support so
  // operators can encode control bytes (\r\n\t\\\xNN) without a hex
  // editor. Save round-trips UTF-8 -> base64 so the underlying wire
  // format remains JSON-safe.

  import { Modal, Button, Input, Icon } from '@chrissnell/chonky-ui';

  import { macrosStore } from '../../lib/terminal/macros.svelte.js';
  import { bytesToB64, b64ToBytes } from '../../lib/terminal/envelope.js';

  let { open = $bindable(false) } = $props();

  // editing rows track {label, payloadText}. payloadText is the
  // human-readable, escape-decoded text the operator types.
  let rows = $state([]);
  let saveError = $state('');
  let saving = $state(false);

  $effect(() => {
    if (open) {
      saveError = '';
      rows = macrosStore.macros.map((m) => ({
        label: m.label,
        payloadText: bytesToText(b64ToBytes(m.payload)),
      }));
    }
  });

  function addRow() {
    rows = [...rows, { label: '', payloadText: '' }];
  }

  function removeRow(i) {
    rows = rows.filter((_, idx) => idx !== i);
  }

  async function save() {
    saveError = '';
    saving = true;
    try {
      const out = [];
      for (const r of rows) {
        const label = r.label.trim();
        if (!label) {
          throw new Error('Every macro needs a label.');
        }
        const bytes = textToBytes(r.payloadText ?? '');
        out.push({ label, payload: bytesToB64(bytes) });
      }
      await macrosStore.saveMacros(out);
      open = false;
    } catch (err) {
      saveError = String(err.message ?? err);
    } finally {
      saving = false;
    }
  }

  // bytesToText renders bytes back to the editor's display form. Bytes
  // < 0x20 (except CR/LF/TAB) and 0x7F render as \xNN escapes so the
  // operator can see + edit them.
  function bytesToText(bytes) {
    let out = '';
    for (let i = 0; i < bytes.length; i++) {
      const b = bytes[i];
      if (b === 0x5c) { out += '\\\\'; continue; }
      if (b === 0x0d) { out += '\\r'; continue; }
      if (b === 0x0a) { out += '\\n'; continue; }
      if (b === 0x09) { out += '\\t'; continue; }
      if (b < 0x20 || b === 0x7f) {
        out += '\\x' + b.toString(16).padStart(2, '0');
        continue;
      }
      out += String.fromCharCode(b);
    }
    return out;
  }

  // textToBytes parses the editor's escape-encoded form back into a
  // Uint8Array. Unknown escapes pass through unchanged so the operator
  // sees the literal chars in a round-trip rather than silently
  // losing them.
  function textToBytes(text) {
    const out = [];
    let i = 0;
    while (i < text.length) {
      const c = text.charCodeAt(i);
      if (c !== 0x5c) {
        if (c < 0x80) {
          out.push(c);
          i++;
        } else {
          // Encode multi-byte UTF-8 chars via TextEncoder to stay correct.
          const enc = new TextEncoder().encode(text.slice(i, i + 1));
          for (const b of enc) out.push(b);
          i++;
        }
        continue;
      }
      // Backslash escape.
      const next = text.charCodeAt(i + 1);
      if (next === 0x72 /* r */) { out.push(0x0d); i += 2; continue; }
      if (next === 0x6e /* n */) { out.push(0x0a); i += 2; continue; }
      if (next === 0x74 /* t */) { out.push(0x09); i += 2; continue; }
      if (next === 0x5c /* \ */) { out.push(0x5c); i += 2; continue; }
      if (next === 0x78 /* x */) {
        const hex = text.slice(i + 2, i + 4);
        if (/^[0-9a-fA-F]{2}$/.test(hex)) {
          out.push(parseInt(hex, 16));
          i += 4;
          continue;
        }
      }
      // Unknown escape: emit the backslash literally and advance one.
      out.push(0x5c);
      i += 1;
    }
    return new Uint8Array(out);
  }
</script>

<Modal bind:open onClose={() => (open = false)} class="macro-editor-modal">
  <Modal.Header>
    <h3 class="modal-title">Edit macros</h3>
    <Modal.Close aria-label="Close">
      <Icon name="x" size="sm" />
    </Modal.Close>
  </Modal.Header>
  <Modal.Body>
    <div class="editor">
      <p class="intro">
        Macros are reusable payloads you can fire from the toolbar above
        the terminal. Type a short label and the bytes to send when the
        button is clicked.
      </p>
      <p class="hint">
        Payload accepts <code>\r</code> <code>\n</code> <code>\t</code> <code>\\</code> and
        <code>\xNN</code> escape sequences. Plain text is sent as UTF-8.
      </p>

      {#each rows as row, i (i)}
        <fieldset class="macro-card">
          <legend class="macro-card-legend">Macro {i + 1}</legend>
          <div class="field">
            <label class="field-label" for={`macro-${i}-label`}>Button label</label>
            <Input
              id={`macro-${i}-label`}
              bind:value={row.label}
              placeholder="login"
              aria-label={`Macro ${i + 1} label`}
            />
          </div>
          <div class="field">
            <label class="field-label" for={`macro-${i}-payload`}>Payload</label>
            <textarea
              id={`macro-${i}-payload`}
              class="payload"
              rows="3"
              aria-label={`Macro ${i + 1} payload`}
              placeholder="hello\r"
              bind:value={row.payloadText}
            ></textarea>
          </div>
          <div class="card-actions">
            <Button variant="danger" size="sm" onclick={() => removeRow(i)} aria-label={`Remove macro ${i + 1}`}>
              <Icon name="trash-2" size="sm" /> Delete
            </Button>
          </div>
        </fieldset>
      {/each}

      {#if rows.length === 0}
        <p class="empty">No macros yet. Click <strong>+ Add macro</strong> below to create one.</p>
      {/if}

      {#if saveError}
        <p class="err" role="alert">{saveError}</p>
      {/if}
    </div>
  </Modal.Body>
  <Modal.Footer>
    <Button variant="accent" onclick={addRow}>+ Add macro</Button>
    <span class="footer-spacer"></span>
    <Button variant="ghost" onclick={() => (open = false)}>Cancel</Button>
    <Button variant="primary" disabled={saving} onclick={save}>
      {saving ? 'Saving...' : 'Save'}
    </Button>
  </Modal.Footer>
</Modal>

<style>
  .modal-title { margin: 0; font-size: 17px; font-weight: 600; }

  /* Override chonky-ui's default modal width (480px). The editor wants
     room for stacked label + payload + delete-button cards. */
  :global(.macro-editor-modal) {
    width: min(720px, 95vw);
    max-width: 95vw;
  }

  .editor {
    display: flex;
    flex-direction: column;
    gap: 14px;
    font-size: 14px;
  }
  .intro {
    margin: 0;
    font-size: 14px;
    line-height: 1.5;
    color: var(--color-text, #111);
  }
  .hint {
    margin: 0;
    font-size: 13px;
    line-height: 1.5;
    color: var(--color-text-muted, #666);
  }
  .hint code {
    font-family: var(--font-mono, ui-monospace, SFMono-Regular, Menlo, monospace);
    font-size: 12px;
    background: var(--color-surface, #f0f0f0);
    padding: 1px 5px;
    border-radius: 3px;
  }
  .macro-card {
    display: flex;
    flex-direction: column;
    gap: 10px;
    padding: 12px 14px 10px;
    border: 1px solid var(--color-border, #ddd);
    border-radius: 6px;
    background: var(--color-bg, #fff);
    margin: 0;
  }
  .macro-card-legend {
    padding: 0 6px;
    font-size: 13px;
    font-weight: 600;
    color: var(--color-text-muted, #666);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
  .field {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .field-label {
    font-size: 13px;
    font-weight: 600;
    color: var(--color-text, #111);
  }
  .payload {
    font-family: var(--font-mono, ui-monospace, SFMono-Regular, Menlo, monospace);
    font-size: 14px;
    padding: 8px 10px;
    border: 1px solid var(--color-border, #ccc);
    border-radius: 4px;
    resize: vertical;
    min-width: 0;
    width: 100%;
    box-sizing: border-box;
    line-height: 1.4;
  }
  .card-actions {
    display: flex;
    justify-content: flex-end;
  }
  .empty {
    color: var(--color-text-muted, #666);
    margin: 0;
    font-style: italic;
    font-size: 14px;
    padding: 18px;
    text-align: center;
    border: 1px dashed var(--color-border, #ddd);
    border-radius: 6px;
  }
  .err { color: var(--color-danger, #c41010); margin: 0; font-size: 13px; }
  .footer-spacer { flex: 1 1 auto; }
</style>
