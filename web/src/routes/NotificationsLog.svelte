<script>
  // History of every popup/OS notification actually raised on this
  // device (message, bulletin, station emergency, new station, favorite)
  // -- device-local, capped at notifications-log-core.js's MAX_LOG_ENTRIES.
  // Populated by messagesTransport.js/bulletinsTransport.js/
  // stationAlertsTransport.js/stationNewTransport.js calling
  // notificationsLogStore.add() alongside their toast/OS/sound calls;
  // see docs/wiki/notifications.md.
  import { push } from 'svelte-spa-router';
  import { Button, Icon } from '@chrissnell/chonky-ui';
  import PageHeader from '../components/PageHeader.svelte';
  import { notificationsLogStore } from '../lib/notificationsLogStore.svelte.js';

  function timeAgo(timestamp) {
    const sec = Math.max(0, Math.floor((Date.now() - timestamp) / 1000));
    if (sec < 60) return `${sec}s ago`;
    const min = Math.floor(sec / 60);
    if (min < 60) return `${min} min ago`;
    const hr = Math.floor(min / 60);
    if (hr < 24) return `${hr}h ${min % 60}m ago`;
    const days = Math.floor(hr / 24);
    return `${days}d ago`;
  }

  function onRowClick(entry) {
    if (entry.href) push(entry.href);
  }

  function onClear() {
    notificationsLogStore.clear();
  }
</script>

<PageHeader title="Notifications Log" subtitle="History of popup and OS notifications shown on this device" />

<div class="log-toolbar">
  <Button variant="secondary" onclick={onClear} disabled={notificationsLogStore.entries.length === 0}>
    Clear log
  </Button>
</div>

{#if notificationsLogStore.entries.length === 0}
  <p class="log-empty">No notifications yet. They'll show up here as they arrive, same as the popup/OS notification.</p>
{:else}
  <ul class="log-rows">
    {#each notificationsLogStore.entries as entry (entry.id)}
      <li class="log-row-li">
        <div
          class="log-row"
          class:clickable={!!entry.href}
          role="button"
          tabindex="0"
          onclick={() => onRowClick(entry)}
          onkeydown={(ev) => { if (entry.href && (ev.key === 'Enter' || ev.key === ' ')) { ev.preventDefault(); onRowClick(entry); } }}
        >
          <span class="log-icon" aria-hidden="true">
            {#if entry.kind === 'station-emergency'}
              <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round">
                <path d="M12 9v4" /><path d="M10.29 3.86 1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0Z" /><path d="M12 17h.01" />
              </svg>
            {:else if entry.kind === 'station-favorite'}
              <Icon name="star" size="sm" />
            {:else if entry.kind === 'station-new'}
              <Icon name="radio-tower" size="sm" />
            {:else if entry.kind === 'bulletin'}
              <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round">
                <rect x="8" y="2" width="8" height="4" rx="1" /><path d="M16 4h2a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2h2" /><line x1="9" y1="10" x2="15" y2="10" /><line x1="9" y1="14" x2="15" y2="14" /><line x1="9" y1="18" x2="12" y2="18" />
              </svg>
            {:else}
              <Icon name="message-square" size="sm" />
            {/if}
          </span>
          <div class="log-body">
            <div class="log-row-top">
              <span class="log-title">{entry.title}</span>
              <span class="log-time">{timeAgo(entry.timestamp)}</span>
            </div>
            {#if entry.body}
              <p class="log-text">{entry.body}</p>
            {/if}
          </div>
        </div>
      </li>
    {/each}
  </ul>
{/if}

<style>
  .log-toolbar {
    display: flex;
    justify-content: flex-end;
    margin-bottom: 12px;
  }
  .log-empty {
    color: var(--text-muted);
    font-size: 13px;
    font-style: italic;
  }
  .log-rows {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    flex: none;
    gap: 4px;
  }
  .log-row-li {
    flex: none;
  }
  .log-row {
    display: flex;
    align-items: flex-start;
    gap: 10px;
    padding: 10px 12px;
    border-radius: 6px;
    background: var(--color-bg-subtle, rgba(127, 127, 127, 0.06));
  }
  .log-row.clickable {
    cursor: pointer;
  }
  .log-row.clickable:hover,
  .log-row.clickable:focus-visible {
    background: var(--color-surface-hover, color-mix(in srgb, var(--color-text) 9%, transparent));
    outline: none;
  }
  .log-icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 16px;
    height: 16px;
    flex-shrink: 0;
    margin-top: 2px;
    color: var(--accent, #7bb8e8);
  }
  .log-body {
    flex: 1;
    min-width: 0;
  }
  .log-row-top {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 12px;
  }
  .log-title {
    font-size: 13px;
    font-weight: 600;
  }
  .log-time {
    font-size: 11px;
    color: var(--text-muted);
    white-space: nowrap;
    flex-shrink: 0;
  }
  .log-text {
    margin: 2px 0 0;
    font-size: 12px;
    color: var(--text-muted);
    overflow-wrap: break-word;
  }
</style>
