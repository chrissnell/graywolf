<script>
  import { parseAllowlist } from '../../lib/actions/grammar.js';

  // Two-way bound CSV string the API stores. Internally we work in chip
  // arrays for editing convenience; on every change we rejoin to keep
  // the parent's binding stable.
  let { value = $bindable('') } = $props();

  let input = $state('');

  // Single source of truth: derive chips from the bound CSV. Mutations
  // rewrite `value`, which re-derives chips on the next pass.
  let chips = $derived(parseAllowlist(value));

  function commit(text) {
    const tokens = parseAllowlist(text);
    if (tokens.length === 0) return;
    const next = [...chips];
    for (const t of tokens) {
      const up = t.toUpperCase();
      if (!next.includes(up)) next.push(up);
    }
    value = next.join(', ');
    input = '';
  }

  function removeChip(i) {
    value = chips.filter((_, idx) => idx !== i).join(', ');
  }

  function onKeydown(e) {
    if (e.key === 'Enter' || e.key === ',') {
      e.preventDefault();
      commit(input);
      return;
    }
    if (e.key === 'Backspace' && input === '' && chips.length > 0) {
      e.preventDefault();
      removeChip(chips.length - 1);
    }
  }

  function onBlur() {
    if (input.trim()) commit(input);
  }
</script>

<!-- svelte-ignore a11y_no_static_element_interactions -->
<!-- svelte-ignore a11y_click_events_have_key_events -->
<div class="allowlist-editor" onclick={(e) => e.currentTarget.querySelector('input')?.focus()}>
  {#each chips as chip, i (chip + i)}
    <span class="chip">
      <span class="chip-call">{chip}</span>
      <button
        type="button"
        class="chip-remove"
        onclick={(e) => { e.stopPropagation(); removeChip(i); }}
        aria-label={`Remove ${chip}`}
      >x</button>
    </span>
  {/each}
  <input
    type="text"
    class="chip-input"
    placeholder={chips.length === 0 ? 'Anyone (OTP still applies)' : 'Add callsign or CALL-*'}
    bind:value={input}
    onkeydown={onKeydown}
    onblur={onBlur}
  />
</div>

<style>
  .allowlist-editor {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 4px;
    padding: 4px;
    min-height: 36px;
    background: var(--color-bg);
    border: 1px solid var(--color-border, var(--border));
    border-radius: var(--radius, 4px);
    cursor: text;
  }
  .allowlist-editor:focus-within {
    border-color: var(--color-primary, var(--accent));
    box-shadow: 0 0 0 2px var(--color-primary-muted, rgba(0, 0, 0, 0.05));
  }
  .chip {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    height: 22px;
    padding: 0 4px 0 8px;
    background: var(--surface-2, rgba(0, 0, 0, 0.06));
    border-radius: 999px;
    font-family: ui-monospace, monospace;
    font-size: 12px;
  }
  .chip-call {
    font-weight: 600;
  }
  .chip-remove {
    width: 16px;
    height: 16px;
    border: 0;
    background: transparent;
    color: inherit;
    cursor: pointer;
    padding: 0;
    border-radius: 999px;
    line-height: 1;
    font-size: 11px;
  }
  .chip-remove:hover {
    background: var(--color-surface, rgba(0, 0, 0, 0.1));
  }
  .chip-input {
    flex: 1 1 140px;
    min-width: 140px;
    border: 0;
    background: transparent;
    outline: none;
    font: inherit;
    color: inherit;
    padding: 2px 4px;
    margin: 0 !important;
  }
  .chip-input::placeholder {
    color: var(--color-text-dim, var(--text-muted));
  }
</style>
