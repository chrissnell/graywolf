<script>
  import { Button, Input, Toggle } from '@chrissnell/chonky-ui';

  // Two-way bound list of arg-spec rows. Parent treats this as the
  // canonical `arg_schema` value; we mutate via `rows = [...]` so
  // Svelte's reactivity tracks every change.
  //
  // `mode` controls layout: 'kv' (default) renders the multi-row
  // key/regex/required editor; 'freeform' renders a single row with
  // regex/max_len/required for the implicit `arg` key. The parent
  // (EditActionModal) is responsible for normalizing argSchema to the
  // single-row shape when mode flips to freeform.
  let {
    argSchema = $bindable([]),
    mode = 'kv',
  } = $props();

  const DEFAULT_REGEX = '^[A-Za-z0-9,_-]{1,32}$';
  const FREEFORM_DEFAULT_REGEX = '^[\\x20-\\x7E]+$';
  const FREEFORM_MAX_CEILING = 200;

  // Per-row error state, keyed by row index. A row is "invalid" iff its
  // regex string fails `new RegExp(value)`. The parent reads
  // `hasErrors()` to block save. Freeform mode reuses index 0.
  let errors = $state({});

  // The parent (EditActionModal) overwrites argSchema wholesale on
  // mode flips, so the row a stale error was indexed against may not
  // exist anymore. Clear errors whenever mode toggles to keep the UI
  // honest — fresh validation re-fires on next blur.
  $effect(() => {
    mode;
    errors = {};
  });

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
  {#if mode === 'freeform'}
    {#if argSchema.length === 1}
      <div class="freeform-row">
        <label class="freeform-field">
          <span class="freeform-label">Regex</span>
          <Input
            type="text"
            placeholder={FREEFORM_DEFAULT_REGEX}
            bind:value={argSchema[0].regex}
            onblur={() => validateRegex(0)}
            class={errors[0] ? 'regex-invalid' : ''}
          />
          {#if errors[0]}
            <span class="error-tooltip" role="alert">{errors[0]}</span>
          {/if}
        </label>
        <label class="freeform-field narrow">
          <span class="freeform-label">Max length (1..{FREEFORM_MAX_CEILING})</span>
          <Input
            type="number"
            min="1"
            max={FREEFORM_MAX_CEILING}
            bind:value={argSchema[0].max_len}
          />
        </label>
        <label class="freeform-field toggle-field">
          <Toggle bind:checked={argSchema[0].required} />
          <span class="freeform-label inline">Required</span>
        </label>
      </div>
      <p class="explainer">
        The single freeform value is exposed to your handler as
        <code>$1</code> (positional argv) and <code>$GW_ARG</code>
        (env var). Webhooks see the bare token <code>{'{{arg}}'}</code>.
      </p>
    {/if}
  {:else}
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
  {/if}
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
  .freeform-row {
    display: grid;
    grid-template-columns: 1fr 200px 120px;
    gap: 12px;
    align-items: end;
  }
  .freeform-field {
    display: flex;
    flex-direction: column;
    gap: 4px;
    position: relative;
  }
  .freeform-field.toggle-field {
    flex-direction: row;
    align-items: center;
    gap: 8px;
  }
  .freeform-label {
    font-size: 11px;
    font-weight: 700;
    letter-spacing: 0.5px;
    text-transform: uppercase;
    color: var(--color-text-dim, var(--text-muted));
  }
  .freeform-label.inline {
    text-transform: none;
    letter-spacing: normal;
    font-weight: 600;
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
  .data-row :global(input),
  .freeform-row :global(input) {
    margin: 0 !important;
  }
</style>
