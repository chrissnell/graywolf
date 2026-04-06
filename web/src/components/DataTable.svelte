<script>
  let { columns = [], rows = [], onEdit = undefined, onDelete = undefined } = $props();
</script>

<div class="table-wrapper">
  <table class="data-table">
    <thead>
      <tr>
        {#each columns as col}
          <th>{col.label}</th>
        {/each}
        {#if onEdit || onDelete}
          <th class="actions-col">Actions</th>
        {/if}
      </tr>
    </thead>
    <tbody>
      {#if rows.length === 0}
        <tr>
          <td colspan={columns.length + (onEdit || onDelete ? 1 : 0)} class="empty-row">
            No items configured
          </td>
        </tr>
      {:else}
        {#each rows as row}
          <tr>
            {#each columns as col}
              <td>
                {#if col.render}
                  {@html col.render(row[col.key], row)}
                {:else if typeof row[col.key] === 'boolean'}
                  <span class="badge" class:badge-on={row[col.key]}>{row[col.key] ? 'On' : 'Off'}</span>
                {:else}
                  {row[col.key] ?? '—'}
                {/if}
              </td>
            {/each}
            {#if onEdit || onDelete}
              <td class="actions-cell">
                {#if onEdit}
                  <button class="action-btn" onclick={() => onEdit(row)} aria-label="Edit">✎</button>
                {/if}
                {#if onDelete}
                  <button class="action-btn action-delete" onclick={() => onDelete(row)} aria-label="Delete">✕</button>
                {/if}
              </td>
            {/if}
          </tr>
        {/each}
      {/if}
    </tbody>
  </table>
</div>

<style>
  .table-wrapper {
    overflow-x: auto;
    border: 1px solid var(--border-color);
    border-radius: var(--radius);
  }
  .data-table {
    width: 100%;
    border-collapse: collapse;
    font-size: 13px;
  }
  th {
    background: var(--bg-tertiary);
    color: var(--text-secondary);
    text-align: left;
    padding: 10px 14px;
    font-weight: 600;
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    white-space: nowrap;
    border-bottom: 1px solid var(--border-color);
  }
  td {
    padding: 10px 14px;
    border-bottom: 1px solid var(--border-light);
    color: var(--text-primary);
  }
  tr:last-child td {
    border-bottom: none;
  }
  tr:hover td {
    background: var(--bg-secondary);
  }
  .empty-row {
    text-align: center;
    color: var(--text-muted);
    padding: 24px;
  }
  .badge {
    display: inline-block;
    padding: 2px 8px;
    border-radius: 10px;
    font-size: 11px;
    font-weight: 600;
    background: var(--bg-hover);
    color: var(--text-muted);
  }
  .badge-on {
    background: rgba(63, 185, 80, 0.15);
    color: var(--success);
  }
  .actions-col {
    width: 100px;
    text-align: right;
  }
  .actions-cell {
    text-align: right;
    white-space: nowrap;
  }
  .action-btn {
    background: none;
    border: 1px solid var(--border-color);
    border-radius: 4px;
    color: var(--text-secondary);
    cursor: pointer;
    padding: 4px 8px;
    margin-left: 4px;
    font-size: 13px;
    transition: background 0.15s, color 0.15s;
  }
  .action-btn:hover {
    background: var(--bg-hover);
    color: var(--text-primary);
  }
  .action-delete:hover {
    color: var(--error);
    border-color: var(--error);
  }
</style>
