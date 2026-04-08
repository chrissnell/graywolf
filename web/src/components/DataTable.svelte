<script>
  import { Table, Badge, Button } from '@chrissnell/chonky-ui';

  // extraActions: optional array of {icon, title, variant, onClick: (row) => any}
  // rendered before the built-in edit/delete buttons. Lets callers add custom
  // row actions without bloating this component with feature-specific props.
  let {
    columns = [],
    rows = [],
    onEdit = undefined,
    onDelete = undefined,
    extraActions = [],
  } = $props();

  let hasActions = $derived(!!onEdit || !!onDelete || extraActions.length > 0);
</script>

<div class="table-wrapper">
  <Table>
    <thead>
      <tr>
        {#each columns as col}
          <th>{col.label}</th>
        {/each}
        {#if hasActions}
          <th class="actions-col">Actions</th>
        {/if}
      </tr>
    </thead>
    <tbody>
      {#if rows.length === 0}
        <tr>
          <td colspan={columns.length + (hasActions ? 1 : 0)} class="empty-row">
            No items configured
          </td>
        </tr>
      {:else}
        {#each rows as row}
          <tr>
            {#each columns as col}
              <td>
                {#if typeof row[col.key] === 'boolean'}
                  <Badge variant={row[col.key] ? 'success' : 'default'}>{row[col.key] ? 'On' : 'Off'}</Badge>
                {:else}
                  {row[col.key] ?? '—'}
                {/if}
              </td>
            {/each}
            {#if hasActions}
              <td class="actions-cell">
                {#each extraActions as action}
                  <Button
                    size="sm"
                    variant={action.variant ?? 'ghost'}
                    title={action.title ?? ''}
                    onclick={() => action.onClick(row)}
                  >{action.icon}</Button>
                {/each}
                {#if onEdit}
                  <Button size="sm" variant="ghost" title="Edit" onclick={() => onEdit(row)}>✎</Button>
                {/if}
                {#if onDelete}
                  <Button size="sm" variant="danger" title="Delete" onclick={() => onDelete(row)}>✕</Button>
                {/if}
              </td>
            {/if}
          </tr>
        {/each}
      {/if}
    </tbody>
  </Table>
</div>

<style>
  .table-wrapper {
    overflow-x: auto;
    border: 1px solid var(--color-border);
    border-radius: var(--radius);
  }
  th {
    text-align: left;
    white-space: nowrap;
  }
  .empty-row {
    text-align: center;
    color: var(--color-text-dim);
    padding: 24px;
  }
  .actions-col {
    width: 100px;
    text-align: right;
  }
  .actions-cell {
    text-align: right;
    white-space: nowrap;
  }
</style>
