<script>
  // Left pane of the Bulletins shell. Shows all bulletin groups with
  // search, All/Active/Inactive filter, and Add/Delete controls.
  // Global group (id=1) is always first and cannot be deleted.

  import { Icon } from '@chrissnell/chonky-ui';

  /** @type {{
   *    groups: any[],
   *    activeGroupId: number | null,
   *    onSelect: (group: any) => void,
   *    onAdd: () => void,
   *    onDelete: (group: any) => void,
   * }} */
  let { groups = [], activeGroupId = null, onSelect, onAdd, onDelete } = $props();

  let searchInput = $state('');
  let filter = $state('all');

  const FILTERS = [
    { id: 'all',      label: 'All' },
    { id: 'active',   label: 'Active' },
    { id: 'inactive', label: 'Inactive' },
  ];

  const q = $derived(searchInput.trim().toUpperCase());

  const filteredGroups = $derived.by(() => {
    return groups.filter((g) => {
      if (filter === 'active'   && !g.active) return false;
      if (filter === 'inactive' && g.active)  return false;
      if (q) {
        const name = g.name === '' ? 'GLOBAL' : g.name.toUpperCase();
        if (!name.includes(q)) return false;
      }
      return true;
    });
  });

  // Global is always id=1 per the seed migration.
  const selectedGroup = $derived(groups.find((g) => g.id === activeGroupId) ?? null);
  const canDelete = $derived(!!selectedGroup && selectedGroup.id !== 1);

  function handleDelete() {
    if (!canDelete) return;
    onDelete?.(selectedGroup);
  }

  function displayName(g) {
    return g.name === '' ? 'Global' : g.name;
  }
</script>

<section class="list" aria-label="Bulletin groups">
  <header class="list-header">
    <div class="search">
      <input
        type="text"
        class="search-input"
        bind:value={searchInput}
        placeholder="Search..."
        aria-label="Search bulletin groups"
      />
    </div>
    <div class="toolbar">
      <div class="toolbar-row">
        <div class="filters" role="radiogroup" aria-label="Filter groups">
          {#each FILTERS as f}
            <button
              type="button"
              class="pill"
              class:active={filter === f.id}
              role="radio"
              aria-checked={filter === f.id}
              onclick={() => (filter = f.id)}
            >
              {f.label}
            </button>
          {/each}
        </div>
      </div>
      <div class="toolbar-row toolbar-actions">
        <button
          type="button"
          class="delete-pill"
          onclick={handleDelete}
          disabled={!canDelete}
          title={canDelete ? 'Delete this group' : 'Select a non-Global group to delete'}
        >
          Delete
        </button>
        <button
          type="button"
          class="new-btn"
          onclick={() => onAdd?.()}
          aria-label="Add group"
          title="Add group"
        >
          <Icon name="plus" size="sm" />
        </button>
      </div>
    </div>
  </header>

  <ul class="rows" role="listbox" aria-label="Bulletin groups">
    {#each filteredGroups as g (g.id)}
      <li>
        <button
          type="button"
          class="group-row"
          class:selected={g.id === activeGroupId}
          role="option"
          aria-selected={g.id === activeGroupId}
          onclick={() => onSelect?.(g)}
        >
          <span class="group-name">{displayName(g)}</span>
          <span class="group-status" class:status-active={g.active} class:status-inactive={!g.active}>
            {g.active ? 'Active' : 'Inactive'}
          </span>
        </button>
      </li>
    {/each}
  </ul>
</section>

<style>
  .list {
    display: flex;
    flex-direction: column;
    height: 100%;
    background: var(--color-surface);
    border-right: 1px solid var(--color-border);
    overflow: hidden;
  }

  .list-header {
    padding: 10px 10px 0;
    border-bottom: 1px solid var(--color-border-subtle);
    flex-shrink: 0;
  }

  .search {
    display: flex;
    align-items: center;
    margin-bottom: 8px;
  }
  .search-input {
    width: 100%;
    padding: 7px 8px;
    background: var(--color-bg);
    border: 1px solid var(--color-border);
    border-radius: var(--radius);
    color: var(--color-text);
    font-family: var(--font-mono);
    font-size: 14px;
  }
  .search-input:focus {
    outline: none;
    border-color: var(--color-primary);
    box-shadow: 0 0 0 2px var(--color-primary-muted);
  }

  .toolbar {
    display: flex;
    flex-direction: column;
    gap: 4px;
    padding-bottom: 4px;
  }
  .toolbar-row {
    display: flex;
    align-items: center;
    gap: 6px;
    height: 36px;
  }
  .toolbar-actions {
    height: 28px;
  }

  .filters {
    display: flex;
    align-items: center;
    flex-wrap: nowrap;
    gap: 4px;
    flex: 1 1 auto;
    min-width: 0;
    overflow: hidden;
  }
  .pill {
    font-family: var(--font-mono);
    font-size: 11px;
    padding: 4px 10px;
    border-radius: 999px;
    background: transparent;
    color: var(--color-text-muted);
    border: 1px solid var(--color-border);
    cursor: pointer;
    transition: background 0.12s, color 0.12s, border-color 0.12s;
    white-space: nowrap;
    flex-shrink: 0;
  }
  .pill:hover {
    background: var(--color-surface-raised);
    color: var(--color-text);
  }
  .pill.active {
    background: var(--color-primary-muted);
    color: var(--color-primary);
    border-color: var(--color-primary);
  }

  .delete-pill {
    font-family: var(--font-mono);
    font-size: 11px;
    padding: 4px 10px;
    border-radius: 999px;
    background: transparent;
    color: var(--color-danger);
    border: 1px solid var(--color-danger);
    cursor: pointer;
    flex-shrink: 0;
    transition: background 0.12s, color 0.12s, border-color 0.12s;
    line-height: 1;
  }
  .delete-pill:hover:not(:disabled) {
    background: var(--color-danger);
    color: white;
  }
  .delete-pill:focus-visible {
    outline: 2px solid var(--color-danger);
    outline-offset: 2px;
  }
  .delete-pill:disabled {
    opacity: 0.4;
    cursor: not-allowed;
  }

  .new-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 28px;
    height: 28px;
    padding: 0;
    border: 1px solid transparent;
    border-radius: var(--radius);
    background: transparent;
    cursor: pointer;
    flex-shrink: 0;
    color: var(--color-text-muted);
    transition: background 0.12s, color 0.12s, border-color 0.12s;
  }
  .new-btn:hover {
    background: var(--color-surface-raised);
    color: var(--color-primary);
    border-color: var(--color-border);
  }
  .new-btn:focus-visible {
    outline: 2px solid var(--color-primary);
    outline-offset: 2px;
  }

  .rows {
    list-style: none;
    margin: 0;
    padding: 0;
    flex: 1 1 auto;
    overflow-y: auto;
    min-height: 0;
  }

  .group-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    width: 100%;
    padding: 8px 12px;
    text-align: left;
    background: transparent;
    border: none;
    cursor: pointer;
    border-bottom: 1px solid var(--color-border-subtle);
    transition: background 0.1s;
  }
  .group-row:hover { background: var(--color-surface-raised); }
  .group-row.selected { background: var(--color-primary-muted); }

  .group-name {
    font-family: var(--font-mono);
    font-weight: 600;
    font-size: 14px;
    color: var(--color-text);
    letter-spacing: 0.3px;
  }

  .group-status {
    font-size: 11px;
    font-weight: 600;
    letter-spacing: 0.5px;
    text-transform: uppercase;
  }
  .status-active   { color: var(--color-success, #2ea043); }
  .status-inactive { color: var(--color-danger,  #cf222e); }
</style>
