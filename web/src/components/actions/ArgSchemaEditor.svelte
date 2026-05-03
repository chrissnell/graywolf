<script>
  import { Button, Input, Toggle } from '@chrissnell/chonky-ui';

  // Two-way bound list of arg-spec rows. Parent treats this as the
  // canonical `arg_schema` value; we mutate via `rows = [...]` so
  // Svelte's reactivity tracks every change.
  let { argSchema = $bindable([]) } = $props();

  const DEFAULT_REGEX = '^[A-Za-z0-9,_-]{1,32}$';

  // Per-row error state, keyed by row index. A row is "invalid" iff its
  // regex string fails `new RegExp(value)`. The parent reads
  // `hasErrors()` to block save.
  let errors = $state({});

  function addRow() {
    argSchema = [
      ...argSchema,
      { key: '', regex: '', required: false },
    ];
  }

  function removeRow(i) {
    argSchema = argSchema.filter((_, idx) => idx !== i);
    const next = {};
    for (const [k, v] of Object.entries(errors)) {
      const idx = Number(k);
      if (idx < i) next[idx] = v;
      else if (idx > i) next[idx - 1] = v;
    }
    errors = next;
  }

  function validateRegex(i) {
    const v = (argSchema[i]?.regex ?? '').trim();
    if (!v) {
      errors = { ...errors, [i]: undefined };
      return;
    }
    try {
      new RegExp(v);
      errors = { ...errors, [i]: undefined };
    } catch (e) {
      errors = { ...errors, [i]: e.message };
    }
  }

  // Public API for the parent: did any row's regex fail validation?
  export function hasErrors() {
    return Object.values(errors).some(Boolean);
  }
</script>

<div class="arg-schema-editor">
  {#if argSchema.length > 0}
    <div class="header-row">
      <span class="col-key">Key</span>
      <span class="col-regex">Allowed regex</span>
      <span class="col-required">Required</span>
      <span class="col-actions"></span>
    </div>

    {#each argSchema as row, i (i)}
      <div class="data-row">
        <div class="col-key">
          <Input
            type="text"
            placeholder="key"
            bind:value={row.key}
          />
        </div>
        <div class="col-regex">
          <Input
            type="text"
            placeholder={DEFAULT_REGEX}
            bind:value={row.regex}
            onblur={() => validateRegex(i)}
            class={errors[i] ? 'regex-invalid' : ''}
          />
          {#if errors[i]}
            <span class="error-tooltip" role="alert">{errors[i]}</span>
          {/if}
        </div>
        <div class="col-required">
          <Toggle bind:checked={row.required} />
        </div>
        <div class="col-actions">
          <Button
            size="sm"
            variant="danger"
            onclick={() => removeRow(i)}
            aria-label={`Remove arg ${row.key || i + 1}`}
          >Delete</Button>
        </div>
      </div>
    {/each}
  {/if}

  <div class="add-row">
    <Button size="sm" variant="ghost" onclick={addRow}>+ Add arg</Button>
  </div>

  <p class="explainer">
    Default regex <code>{DEFAULT_REGEX}</code> allows letters, digits,
    comma, underscore, hyphen. Override per key when you need different
    characters. Args failing the regex are rejected.
  </p>
</div>

<style>
  .arg-schema-editor {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .header-row,
  .data-row {
    display: grid;
    grid-template-columns: 1fr 2fr 90px 90px;
    gap: 8px;
    align-items: center;
  }
  .header-row {
    font-size: 11px;
    font-weight: 700;
    letter-spacing: 0.5px;
    text-transform: uppercase;
    color: var(--color-text-dim, var(--text-muted));
    padding: 0 2px;
  }
  .col-regex {
    position: relative;
  }
  .col-actions {
    text-align: right;
  }
  .add-row {
    margin-top: 2px;
  }
  .explainer {
    margin: 4px 0 0;
    font-size: 12px;
    color: var(--color-text-muted, var(--text-muted));
  }
  .explainer code {
    font-family: ui-monospace, monospace;
    font-size: 11px;
    background: var(--accent-bg, rgba(0, 0, 0, 0.05));
    padding: 1px 4px;
    border-radius: 3px;
  }
  .error-tooltip {
    position: absolute;
    top: 100%;
    left: 0;
    margin-top: 2px;
    font-size: 11px;
    color: var(--color-danger, #b91c1c);
  }
  .arg-schema-editor :global(.regex-invalid) {
    border-color: var(--color-danger, #b91c1c) !important;
  }
  /* Chonky's global input rule adds margin-bottom:1rem; flatten it so
     rows share the same baseline as the Toggle and Delete button. */
  .data-row :global(input) {
    margin: 0 !important;
  }
</style>
