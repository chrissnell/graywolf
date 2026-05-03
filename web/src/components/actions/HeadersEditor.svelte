<script>
  import { Button, Input } from '@chrissnell/chonky-ui';

  // Two-way bound Record<string, string>. We work in an internal row
  // array so duplicate keys mid-edit don't collapse and so empty rows
  // can exist while typing. On every change we rebuild the record.
  let { headers = $bindable({}) } = $props();

  const NAME_RE = /^[A-Za-z][A-Za-z0-9-]*$/;

  // Bootstrap rows from incoming headers exactly once per modal open.
  // Object identity changes when the parent reassigns; that's our cue
  // to reseed (`prevHeaders !== headers`).
  let prevHeaders = null;
  let rows = $state([]);
  $effect(() => {
    if (headers !== prevHeaders) {
      prevHeaders = headers;
      rows = Object.entries(headers ?? {}).map(([k, v]) => ({ name: k, value: v }));
    }
  });

  let nameErrors = $state({});

  function syncBack() {
    const next = {};
    for (const r of rows) {
      const n = r.name.trim();
      if (!n) continue;
      next[n] = r.value;
    }
    prevHeaders = next; // suppress the effect from re-seeding from our own write
    headers = next;
  }

  function addRow() {
    rows = [...rows, { name: '', value: '' }];
  }

  function removeRow(i) {
    rows = rows.filter((_, idx) => idx !== i);
    const next = {};
    for (const [k, v] of Object.entries(nameErrors)) {
      const idx = Number(k);
      if (idx < i) next[idx] = v;
      else if (idx > i) next[idx - 1] = v;
    }
    nameErrors = next;
    syncBack();
  }

  function validateName(i) {
    const n = (rows[i]?.name ?? '').trim();
    if (!n) {
      nameErrors = { ...nameErrors, [i]: undefined };
      return;
    }
    nameErrors = {
      ...nameErrors,
      [i]: NAME_RE.test(n) ? undefined : 'Invalid header name',
    };
  }

  export function hasErrors() {
    return Object.values(nameErrors).some(Boolean);
  }
</script>

<div class="headers-editor">
  {#if rows.length > 0}
    <div class="header-row">
      <span class="col-name">Header</span>
      <span class="col-value">Value</span>
      <span class="col-actions"></span>
    </div>
    {#each rows as row, i (i)}
      <div class="data-row">
        <div class="col-name">
          <Input
            type="text"
            placeholder="X-Custom-Header"
            bind:value={row.name}
            oninput={syncBack}
            onblur={() => validateName(i)}
            class={nameErrors[i] ? 'name-invalid' : ''}
          />
          {#if nameErrors[i]}
            <span class="error-tooltip" role="alert">{nameErrors[i]}</span>
          {/if}
        </div>
        <div class="col-value">
          <Input
            type="text"
            placeholder="value"
            bind:value={row.value}
            oninput={syncBack}
          />
        </div>
        <div class="col-actions">
          <Button size="sm" variant="danger" onclick={() => removeRow(i)}>Delete</Button>
        </div>
      </div>
    {/each}
  {/if}

  <div class="add-row">
    <Button size="sm" variant="ghost" onclick={addRow}>+ Add header</Button>
  </div>
</div>

<style>
  .headers-editor {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .header-row,
  .data-row {
    display: grid;
    grid-template-columns: 1fr 2fr 90px;
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
  .col-name {
    position: relative;
  }
  .col-actions {
    text-align: right;
  }
  .add-row {
    margin-top: 2px;
  }
  .error-tooltip {
    position: absolute;
    top: 100%;
    left: 0;
    margin-top: 2px;
    font-size: 11px;
    color: var(--color-danger, #b91c1c);
  }
  .headers-editor :global(.name-invalid) {
    border-color: var(--color-danger, #b91c1c) !important;
  }
  .data-row :global(input) {
    margin: 0 !important;
  }
</style>
