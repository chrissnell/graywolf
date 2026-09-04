<script>
  // Clickable new-activity popup stack — fed by messagesTransport.js
  // (new DMs/tacticals), bulletinsTransport.js (new bulletins), and
  // Preferences.svelte's "Send test notification" button.
  //
  // Mounted unconditionally in App.svelte (not gated like NewsPopup,
  // since this must be live from app start on every route, including
  // the full-bleed map/messages views). Chonky-ui's toast()/<Toaster/>
  // only accepts a plain string message (no click handler, no markup —
  // confirmed by reading its source), so this is a small purpose-built
  // component rather than a wrapper around that primitive.

  import { push } from 'svelte-spa-router';
  import { Icon } from '@chrissnell/chonky-ui';
  import { notifications } from '../lib/notificationsStore.svelte.js';

  function onCardClick(n) {
    if (n.href) push(n.href);
    notifications.dismiss(n.id);
  }

  function onDismissClick(ev, id) {
    ev.stopPropagation();
    notifications.dismiss(id);
  }
</script>

{#if notifications.entries.length > 0}
  <div class="notification-stack" role="region" aria-label="New activity notifications">
    {#each notifications.entries as n (n.id)}
      <div
        class="notification-card"
        class:clickable={!!n.href}
        class:notification-card-alarm={n.kind === 'station-emergency'}
        role="button"
        tabindex="0"
        onclick={() => onCardClick(n)}
        onkeydown={(ev) => { if (ev.key === 'Enter' || ev.key === ' ') { ev.preventDefault(); onCardClick(n); } }}
      >
        <span class="notification-icon" class:notification-icon-alarm={n.kind === 'station-emergency'} aria-hidden="true">
          {#if n.kind === 'station-emergency'}
            <svg
              xmlns="http://www.w3.org/2000/svg"
              width="18"
              height="18"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="1.75"
              stroke-linecap="round"
              stroke-linejoin="round"
            >
              <path d="M12 9v4" />
              <path d="M10.29 3.86 1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0Z" />
              <path d="M12 17h.01" />
            </svg>
          {:else if n.kind === 'station-favorite'}
            <Icon name="star" size="sm" />
          {:else if n.kind === 'station-new'}
            <Icon name="radio-tower" size="sm" />
          {:else if n.kind === 'bulletin'}
            <svg
              xmlns="http://www.w3.org/2000/svg"
              width="18"
              height="18"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="1.75"
              stroke-linecap="round"
              stroke-linejoin="round"
            >
              <rect x="8" y="2" width="8" height="4" rx="1" />
              <path d="M16 4h2a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2h2" />
              <line x1="9" y1="10" x2="15" y2="10" />
              <line x1="9" y1="14" x2="15" y2="14" />
              <line x1="9" y1="18" x2="12" y2="18" />
            </svg>
          {:else}
            <Icon name="message-square" size="sm" />
          {/if}
        </span>
        <div class="notification-body">
          <p class="notification-title">{n.title}</p>
          {#if n.body}
            <p class="notification-text">{n.body}</p>
          {/if}
        </div>
        <button
          type="button"
          class="notification-dismiss"
          aria-label="Dismiss notification"
          onclick={(ev) => onDismissClick(ev, n.id)}
        >
          ✕
        </button>
      </div>
    {/each}
  </div>
{/if}

<style>
  .notification-stack {
    position: fixed;
    top: 16px;
    right: 16px;
    z-index: 200;
    display: flex;
    flex-direction: column;
    gap: 8px;
    width: min(340px, calc(100vw - 32px));
  }

  .notification-card {
    display: flex;
    align-items: flex-start;
    gap: 10px;
    padding: 10px 12px;
    background: var(--bg-secondary, #1e1e1e);
    border: 1px solid var(--border-color, #333);
    border-radius: 8px;
    box-shadow: 0 4px 16px rgba(0, 0, 0, 0.25);
    color: var(--text-primary);
  }

  .notification-card.clickable {
    cursor: pointer;
  }

  .notification-card.clickable:hover,
  .notification-card.clickable:focus-visible {
    border-color: var(--accent, #7bb8e8);
  }

  .notification-card-alarm {
    border-color: var(--color-danger, #d33);
  }

  .notification-card-alarm.clickable:hover,
  .notification-card-alarm.clickable:focus-visible {
    border-color: var(--color-danger, #d33);
  }

  .notification-icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 18px;
    height: 18px;
    flex-shrink: 0;
    margin-top: 2px;
    color: var(--accent, #7bb8e8);
  }

  .notification-icon-alarm {
    color: var(--color-danger, #d33);
  }

  .notification-body {
    flex: 1;
    min-width: 0;
  }

  .notification-title {
    margin: 0;
    font-size: 0.85rem;
    font-weight: 600;
  }

  .notification-text {
    margin: 2px 0 0;
    font-size: 0.78rem;
    opacity: 0.75;
    overflow: hidden;
    text-overflow: ellipsis;
    display: -webkit-box;
    -webkit-line-clamp: 2;
    -webkit-box-orient: vertical;
  }

  .notification-dismiss {
    background: none;
    border: none;
    cursor: pointer;
    font-size: 0.7rem;
    opacity: 0.5;
    padding: 2px;
    color: inherit;
    line-height: 1;
    flex-shrink: 0;
  }

  .notification-dismiss:hover {
    opacity: 1;
  }

  @media (max-width: 768px) {
    .notification-stack {
      top: calc(56px + var(--safe-area-top) + 8px);
    }
  }
</style>
