<!-- web/src/routes/ptt/MethodPicker.svelte -->
<script module>
  // A deterministic key per method-option for binding to the radio group.
  // Includes ppt_method so Android's five `method:'android'` entries are
  // distinguishable.
  export function key(m) {
    return m.wire.ppt_method != null
      ? `${m.wire.method}#${m.wire.ppt_method}`
      : m.wire.method;
  }
</script>

<script>
  // Radio-card method picker. Pure presentation: the parent supplies the
  // method-options array and current selection; we emit selection changes.
  //
  // Each `methods` entry has shape:
  //   { wire: { method, ppt_method? }, label, meta, deviceClass? }
  // `wire` is what eventually goes onto the /api/ptt POST body.
  // `key()` is defined in the <script module> block above and is directly
  // accessible to this instance script — no import required.

  let {
    methods,           // array of method-option objects
    selectedWireKey,   // string; the key() of the currently-selected method
    onSelect,          // (method) => void; receives the full method-option
  } = $props();
</script>

<ul class="method-list" role="radiogroup">
  {#each methods as m (key(m))}
    {@const isSelected = key(m) === selectedWireKey}
    <li>
      <button
        type="button"
        class="method-card"
        class:selected={isSelected}
        role="radio"
        aria-checked={isSelected}
        onclick={() => onSelect(m)}
      >
        <span class="method-label">{m.label}</span>
        {#if m.meta}
          <span class="method-meta">{m.meta}</span>
        {/if}
      </button>
    </li>
  {/each}
</ul>

<style>
  .method-list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .method-card {
    width: 100%;
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: 10px 12px;
    text-align: left;
    border: 1px solid var(--border-color);
    border-radius: 6px;
    background: var(--bg-surface, #fff);
    cursor: pointer;
    font-size: 14px;
  }
  .method-card.selected {
    border-color: var(--accent, #3b82f6);
    background: var(--bg-selected, #eff6ff);
  }
  .method-label {
    font-weight: 600;
  }
  .method-meta {
    font-size: 12px;
    color: var(--text-secondary, #555);
  }
</style>
