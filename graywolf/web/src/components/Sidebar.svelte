<script>
  import { link } from 'svelte-spa-router';
  import { location } from 'svelte-spa-router';
  import { Icon, NotificationBadge } from '@chrissnell/chonky-ui';
  import { messages } from '../lib/messagesStore.svelte.js';
  import logoUrl from '../assets/graywolf.svg';

  const topItems = [
    { path: '/', label: 'Dashboard' },
    { path: '/map', label: 'Live Map' },
  ];

  // The Messages entry is modeled separately from `navGroups[].items`
  // so it can render an Icon + NotificationBadge — the other Operations
  // links are plain-label for now. Keeping them in the same group
  // visually without forcing every other label into an Icon treatment.
  const operationsItems = [
    { path: '/messages', label: 'Messages', icon: 'message-square', badge: true },
    { path: '/beacons', label: 'Beacons' },
    { path: '/digipeater', label: 'Digipeater' },
    { path: '/igate', label: 'iGate' },
    { path: '/logs', label: 'Logs' },
  ];

  const navGroups = [
    {
      label: 'Operations',
      items: operationsItems,
    },
    {
      label: 'Settings',
      items: [
        { path: '/channels', label: 'Channels' },
        { path: '/audio-devices', label: 'Audio Devices' },
        { path: '/ptt', label: 'PTT' },
        { path: '/gps', label: 'GPS' },
        { path: '/position-log', label: 'Position Log' },
        { path: '/simulation', label: 'Simulation' },
        { path: '/preferences', label: 'Preferences' },
      ],
    },
    {
      label: 'Interfaces',
      items: [
        { path: '/kiss', label: 'KISS' },
        { path: '/agw', label: 'AGW' },
      ],
    },
  ];

  let currentPath = $state('');
  $effect(() => {
    const unsub = location.subscribe((v) => { currentPath = v; });
    return unsub;
  });

  // Reactive global unread signal — recomputes when any thread's
  // unreadCount / muted / archived flag changes.
  let unreadTotal = $derived(messages.unreadTotal);
</script>

<nav class="sidebar" aria-label="Main navigation">
  <div class="sidebar-header">
    <a href="/" use:link class="logo-link" aria-label="Dashboard">
      <img src={logoUrl} alt="" class="logo-img" />
      <h1 class="logo">graywolf</h1>
    </a>
  </div>
  <div class="nav-scroll">
    <ul class="nav-list dashboard-list">
      {#each topItems as item}
        <li>
          <a
            href={item.path}
            use:link
            class="nav-link dashboard-link"
            class:active={currentPath === item.path}
            aria-current={currentPath === item.path ? 'page' : undefined}
          >
            <span class="nav-label">{item.label}</span>
          </a>
        </li>
      {/each}
    </ul>
    {#each navGroups as group}
      <div class="nav-group">
        <h2 class="nav-group-label">{group.label}</h2>
        <ul class="nav-list">
          {#each group.items as item}
            <li>
              <a
                href={item.path}
                use:link
                class="nav-link"
                class:has-icon={item.icon}
                class:active={currentPath === item.path || currentPath.startsWith(item.path + '/')}
                aria-current={currentPath === item.path ? 'page' : undefined}
              >
                {#if item.icon}
                  <span class="nav-icon" aria-hidden="true">
                    <Icon name={item.icon} size="sm" />
                  </span>
                {/if}
                <span class="nav-label">{item.label}</span>
                {#if item.badge}
                  <span class="nav-badge">
                    <NotificationBadge count={unreadTotal} />
                  </span>
                {/if}
              </a>
            </li>
          {/each}
        </ul>
      </div>
    {/each}
    <div class="nav-trailing">
      <a
        href="/about"
        use:link
        class="nav-link"
        class:active={currentPath === '/about'}
        aria-current={currentPath === '/about' ? 'page' : undefined}
      >
        <span class="nav-label">About</span>
      </a>
    </div>
  </div>
</nav>

<style>
  .sidebar {
    width: var(--sidebar-width);
    height: 100vh;
    position: fixed;
    top: 0;
    left: 0;
    background: var(--bg-secondary);
    border-right: 1px solid var(--border-color);
    overflow-y: auto;
    z-index: 100;
    display: flex;
    flex-direction: column;
  }

  .sidebar-header {
    padding: 16px;
    border-bottom: 1px solid var(--border-color);
  }

  .logo-link {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 8px;
    text-decoration: none;
  }

  .logo-img {
    width: 64px;
    height: 64px;
    display: block;
  }

  .logo {
    font-size: 18px;
    font-weight: 700;
    color: var(--text-secondary);
    letter-spacing: 1px;
    text-align: center;
    margin: 0;
  }

  .nav-scroll {
    flex: 1;
    overflow-y: auto;
    padding: 0 0 12px;
  }

  .nav-list {
    list-style: none;
    padding: 0;
  }

  .dashboard-list {
    padding: 6px 0;
    border-bottom: 1px solid var(--border-color);
  }

  .nav-group {
    padding: 0;
  }

  .nav-group-label {
    font-size: 10px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 1.5px;
    color: var(--text-secondary);
    opacity: 0.5;
    padding: 10px 16px 6px;
    margin: 0;
    border-top: 1px solid var(--border-color);
  }

  .nav-link {
    display: flex;
    align-items: center;
    gap: 0;
    padding: 7px 16px 7px 24px;
    color: var(--text-secondary);
    transition: background 0.15s, color 0.15s;
    font-size: 13px;
    position: relative;
  }

  .nav-link.has-icon {
    padding-left: 16px;
    gap: 8px;
  }

  .nav-link.has-icon.active {
    padding-left: 13px;
  }

  .nav-icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 16px;
    height: 16px;
    flex-shrink: 0;
    color: currentColor;
  }

  .nav-badge {
    margin-left: auto;
    display: inline-flex;
    align-items: center;
  }

  .dashboard-link {
    font-weight: 600;
    padding-left: 16px;
    font-size: 13px;
  }

  .nav-link:hover {
    background: var(--bg-hover);
    color: var(--text-primary);
  }

  .nav-link.active {
    background: var(--bg-tertiary);
    color: var(--accent);
    border-left: 3px solid var(--accent);
    padding-left: 21px;
  }

  .dashboard-link.active {
    padding-left: 13px;
  }

  /* .nav-trailing pins About to the bottom of the desktop sidebar
     (margin-top: auto pushes it past the last nav group), then
     transforms into a regular mobile tab at ≤768px. */
  .nav-trailing {
    margin-top: auto;
    border-top: 1px solid var(--border-color);
    padding: 6px 0;
  }

@media (max-width: 768px) {
    /* Mobile bottom-bar: one flat horizontally-scrolling row of tabs.
       Every item — Dashboard through About — lives inside .nav-scroll
       as a peer flex child. The nav-group wrappers become transparent
       via display: contents so their <li> descendants hoist up to
       .nav-scroll's flex row. */
    .sidebar {
      width: 100%;
      height: 60px;
      position: fixed;
      bottom: 0;
      top: auto;
      flex-direction: row;
      border-right: none;
      border-top: 1px solid var(--border-color);
      overflow: hidden;
    }
    .sidebar-header {
      display: none;
    }
    .nav-scroll {
      flex: 1 1 auto;
      min-width: 0;
      display: flex;
      flex-direction: row;
      align-items: stretch;
      overflow-x: auto;
      overflow-y: hidden;
      padding: 0;
      scrollbar-width: none;  /* Firefox — hide horizontal scrollbar */
    }
    .nav-scroll::-webkit-scrollbar {
      display: none;
    }
    .nav-group,
    .dashboard-list,
    .nav-trailing {
      display: contents;
    }
    .nav-group-label {
      display: none;
    }
    .nav-list {
      display: contents;
      padding: 0;
    }
    .nav-list li {
      display: flex;
    }
    .nav-link,
    .nav-link.has-icon,
    .dashboard-link {
      /* One tab rule — reset every desktop specificity path. */
      flex-direction: column;
      align-items: center;
      justify-content: center;
      gap: 2px;
      padding: 0 10px;
      height: 60px;
      min-width: 64px;
      font-size: 10px;
      font-weight: 500;
      white-space: nowrap;
      border-left: none;
      position: relative;
    }
    .nav-link.active,
    .nav-link.has-icon.active,
    .dashboard-link.active {
      /* Top-border accent + surface background; no left-border. */
      border-left: none;
      border-top: 2px solid var(--accent);
      padding-left: 10px;
      background: var(--bg-tertiary);
    }
    .nav-icon {
      width: 18px;
      height: 18px;
    }
    /* Items without an icon get a spacer so labels align vertically
       with icon'd items. */
    .nav-link:not(.has-icon) .nav-label {
      padding-top: 18px;
    }
    .nav-label {
      white-space: nowrap;
      line-height: 1;
    }
    /* Unread badge pinned to the top-right of its tab (Messages). */
    .nav-badge {
      position: absolute;
      top: 6px;
      right: 8px;
      margin-left: 0;
    }
    /* Right-edge fade hints at horizontal scroll affordance. */
    .sidebar::after {
      content: '';
      position: absolute;
      top: 0;
      right: 0;
      height: 100%;
      width: 20px;
      pointer-events: none;
      background: linear-gradient(
        to right,
        transparent,
        var(--bg-secondary)
      );
    }
  }
</style>
