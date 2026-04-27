<script>
  // Bottom-sheet state picker. Renders 50 states + DC with per-state status
  // (absent / downloading / complete / error) and the appropriate action
  // button(s). Mobile-first: rows >=56px tall, buttons >=44px tap zone, the
  // search input forced to 16px to suppress iOS auto-zoom.
  import { Drawer, Button, Input } from '@chrissnell/chonky-ui';
  import { US_STATES } from './state-list.js';
  import { downloadsState } from './downloads-store.svelte.js';
  import { formatBytes } from './format-bytes.js';

  let { open = $bindable(false) } = $props();
  let query = $state('');

  let filtered = $derived.by(() => {
    const q = query.trim().toLowerCase();
    if (!q) return US_STATES;
    return US_STATES.filter(
      (s) => s.name.toLowerCase().includes(q) || s.slug.includes(q),
    );
  });

  function statusOf(slug) {
    return downloadsState.items.get(slug);
  }
</script>

<Drawer bind:open anchor="bottom">
  <Drawer.Header>
    <h2 class="picker-title">Offline state maps</h2>
    <Drawer.Close aria-label="Close">×</Drawer.Close>
  </Drawer.Header>
  <Drawer.Body>
    <div class="picker-search">
      <Input
        type="text"
        placeholder="Search states..."
        bind:value={query}
        autocomplete="off"
        spellcheck={false}
      />
    </div>
    <ul class="state-list" role="list">
      {#each filtered as state (state.slug)}
        {@const item = statusOf(state.slug)}
        {@const status = item?.state ?? 'absent'}
        <li class="state-row">
          <div class="state-text">
            <span class="state-name">{state.name}</span>
            <span class="state-status" data-status={status}>
              {#if status === 'downloading'}
                {formatBytes(item.bytes_downloaded)}
                {#if item.bytes_total > 0}
                  / {formatBytes(item.bytes_total)}
                  ({Math.round((item.bytes_downloaded / item.bytes_total) * 100)}%)
                {/if}
              {:else if status === 'complete'}
                {formatBytes(item.bytes_total)} downloaded
              {:else if status === 'error'}
                <span class="status-error">{item.error_message || 'Download failed'}</span>
              {:else if status === 'pending'}
                Pending...
              {:else}
                Not downloaded
              {/if}
            </span>
            {#if status === 'downloading' && item.bytes_total > 0}
              <progress
                class="state-progress"
                value={item.bytes_downloaded}
                max={item.bytes_total}
              ></progress>
            {/if}
          </div>
          <div class="state-actions">
            {#if status === 'absent' || status === 'error'}
              <Button onclick={() => downloadsState.start(state.slug)}>Download</Button>
            {:else if status === 'complete'}
              <Button variant="default" onclick={() => downloadsState.start(state.slug)}>
                Re-download
              </Button>
              <Button variant="danger" onclick={() => downloadsState.remove(state.slug)}>
                Delete
              </Button>
            {:else if status === 'downloading' || status === 'pending'}
              <Button variant="default" disabled>Downloading...</Button>
            {/if}
          </div>
        </li>
      {:else}
        <li class="state-empty">No states match "{query}"</li>
      {/each}
    </ul>
  </Drawer.Body>
</Drawer>

<style>
  .picker-title {
    margin: 0;
    font-size: 14px;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 1px;
  }
  .picker-search {
    margin-bottom: 12px;
  }
  /* iOS auto-zoom mitigation: input must be 16px+ at the rendered size. */
  .picker-search :global(input) {
    font-size: 16px;
  }
  .state-list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .state-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: 10px 12px;
    min-height: 56px;
    border: 1px solid var(--border-color);
    border-radius: 6px;
    background: var(--bg-secondary);
  }
  .state-text {
    display: flex;
    flex-direction: column;
    gap: 2px;
    flex: 1;
    min-width: 0;
  }
  .state-name {
    font-size: 14px;
    font-weight: 600;
    color: var(--text-primary);
  }
  .state-status {
    font-size: 12px;
    color: var(--text-muted);
  }
  .state-status[data-status="error"] {
    color: var(--color-danger);
  }
  .state-status[data-status="complete"] {
    color: var(--color-success);
  }
  .state-progress {
    width: 100%;
    height: 4px;
    margin-top: 4px;
  }
  .state-actions {
    display: flex;
    flex-direction: column;
    gap: 4px;
    flex-shrink: 0;
  }
  @media (min-width: 600px) {
    .state-actions {
      flex-direction: row;
    }
  }
  .state-empty {
    text-align: center;
    padding: 24px;
    color: var(--text-muted);
    font-size: 13px;
  }
  .status-error {
    color: var(--color-danger);
  }
</style>
