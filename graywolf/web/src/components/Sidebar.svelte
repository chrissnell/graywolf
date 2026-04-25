<script>
  import { untrack } from 'svelte';
  import { link } from 'svelte-spa-router';
  import { location } from 'svelte-spa-router';
  import { Icon, NotificationBadge, Drawer } from '@chrissnell/chonky-ui';
  import { messages } from '../lib/messagesStore.svelte.js';
  import { updates } from '../lib/updatesStore.svelte.js';
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
        { path: '/callsign', label: 'Station Callsign' },
        { path: '/channels', label: 'Channels' },
        { path: '/audio-devices', label: 'Audio Devices' },
        { path: '/ptt', label: 'PTT' },
        { path: '/gps', label: 'GPS' },
        { path: '/position-log', label: 'Position Log' },
        { path: '/simulation', label: 'Simulation' },
        { path: '/preferences/maps', label: 'Maps' },
        { path: '/preferences', label: 'General' },
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

  // Update-check signal — true when a newer GitHub release exists and
  // the operator hasn't dismissed the banner. Drives both the About
  // link's red dot here and the banner on the About tab; dismissing
  // in one place clears the other automatically via updates.dismiss().
  let hasUnseenUpdate = $derived(updates.hasUnseenUpdate);

  // Sidebar is always-mounted in the authenticated shell, so this
  // mount-time fetch is the single call that primes hasUnseenUpdate
  // on every page load — the badge can appear on Dashboard/Map/etc.
  // without the operator ever visiting About first. No reactive reads
  // in the body, so this runs exactly once.
  $effect(() => {
    updates.fetchStatus();
  });

  // Drawer open state (mobile only). Closed on link click in onNavClick;
  // an effect on currentPath below acts as a safety net for programmatic
  // navigation (e.g., post-login redirects).
  let menuOpen = $state(false);

  // Safety net: if currentPath changes for any reason other than a click
  // inside the drawer (e.g., post-login redirect), ensure the drawer is
  // closed. The per-link onclick is the *primary* close path because it
  // sequences close-then-navigate and lets bits-ui's PresenceManager play
  // the 150ms exit cleanly. We `untrack` the menuOpen write so opening
  // the drawer doesn't immediately re-run this effect and snap it shut.
  $effect(() => {
    currentPath; // track only currentPath
    untrack(() => {
      if (menuOpen) menuOpen = false;
    });
  });

  function onNavClick() {
    // Close before navigation fires. bits-ui keeps the drawer DOM mounted
    // until getAnimations() settles, so the slide-out animation completes
    // even after the route swaps.
    menuOpen = false;
  }

  // Dashboard route match — exact only ('/'); avoid matching every sub-route.
  let isDashboardActive = $derived(currentPath === '/');
  // Live Map route match — '/map' or any '/map/*' sub-route.
  let isMapActive = $derived(currentPath === '/map' || currentPath.startsWith('/map/'));
  // Messages route match — '/messages' or any '/messages/*' sub-route.
  let isMessagesActive = $derived(currentPath === '/messages' || currentPath.startsWith('/messages/'));

  // Per-group active item: longest-prefix match wins. This prevents e.g.
  // '/preferences/maps' from highlighting both the Maps entry and a
  // 'General' (/preferences) entry — only the most specific match lights up.
  function activePathFor(items, path) {
    let best = '';
    for (const it of items) {
      if (path === it.path || path.startsWith(it.path + '/')) {
        if (it.path.length > best.length) best = it.path;
      }
    }
    return best;
  }
  let activeGroupPaths = $derived(
    navGroups.map((g) => activePathFor(g.items, currentPath)),
  );
</script>

{#snippet navItems()}
  <ul class="nav-list dashboard-list">
    {#each topItems as item}
      <li>
        <a
          href={item.path}
          use:link
          class="nav-link dashboard-link"
          class:active={currentPath === item.path}
          aria-current={currentPath === item.path ? 'page' : undefined}
          onclick={onNavClick}
        >
          <span class="nav-label">{item.label}</span>
        </a>
      </li>
    {/each}
  </ul>
  {#each navGroups as group, groupIdx}
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
              class:active={item.path === activeGroupPaths[groupIdx]}
              aria-current={currentPath === item.path ? 'page' : undefined}
              onclick={onNavClick}
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
      aria-label={hasUnseenUpdate ? 'About, update available' : undefined}
      onclick={onNavClick}
    >
      <span class="nav-label">About</span>
      <span class="nav-badge">
        <NotificationBadge count={hasUnseenUpdate ? 1 : 0} label="Update available" />
      </span>
    </a>
  </div>
{/snippet}

<!-- Desktop sidebar (≥769px) -->
<nav class="sidebar" aria-label="Main navigation">
  <div class="sidebar-header">
    <a href="/" use:link class="logo-link" aria-label="Dashboard">
      <img src={logoUrl} alt="" class="logo-img" />
      <h1 class="logo">graywolf</h1>
    </a>
  </div>
  <div class="nav-scroll">
    {@render navItems()}
  </div>
</nav>

<!-- Mobile top app bar (≤768px) -->
<header class="top-bar" aria-label="App bar">
  <a href="/" use:link class="top-bar-brand" aria-label="Dashboard">
    <img src={logoUrl} alt="" class="top-bar-logo" />
    <span class="top-bar-wordmark">graywolf</span>
  </a>

  <a
    href="/"
    use:link
    class="top-bar-action"
    class:active={isDashboardActive}
    aria-label="Dashboard"
    aria-current={isDashboardActive ? 'page' : undefined}
  >
    <span class="top-bar-icon" aria-hidden="true">
      <!-- Inline dashboard glyph: Chonky icon allowlist lacks layout-dashboard/home. -->
      <svg
        xmlns="http://www.w3.org/2000/svg"
        width="24"
        height="24"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        stroke-width="1.75"
        stroke-linecap="round"
        stroke-linejoin="round"
      >
        <rect x="3" y="3" width="7" height="9" />
        <rect x="14" y="3" width="7" height="5" />
        <rect x="14" y="12" width="7" height="9" />
        <rect x="3" y="16" width="7" height="5" />
      </svg>
    </span>
  </a>

  <a
    href="/map"
    use:link
    class="top-bar-action"
    class:active={isMapActive}
    aria-label="Live Map"
    aria-current={isMapActive ? 'page' : undefined}
  >
    <span class="top-bar-icon" aria-hidden="true">
      <!-- Inline globe glyph: Chonky icon allowlist lacks 'globe'. -->
      <svg
        xmlns="http://www.w3.org/2000/svg"
        width="24"
        height="24"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        stroke-width="1.75"
        stroke-linecap="round"
        stroke-linejoin="round"
      >
        <circle cx="12" cy="12" r="9" />
        <path d="M3 12h18" />
        <path d="M12 3a14 14 0 0 1 0 18" />
        <path d="M12 3a14 14 0 0 0 0 18" />
      </svg>
    </span>
  </a>

  <a
    href="/messages"
    use:link
    class="top-bar-action"
    class:active={isMessagesActive}
    aria-label={unreadTotal > 0 ? `Messages, ${unreadTotal} unread` : 'Messages'}
    aria-current={isMessagesActive ? 'page' : undefined}
  >
    <span class="top-bar-icon" aria-hidden="true">
      <Icon name="message-square" size={24} strokeWidth={1.75} />
    </span>
    <span class="top-bar-badge">
      <NotificationBadge count={unreadTotal} />
    </span>
  </a>

  <span class="top-bar-spacer"></span>

  <button
    type="button"
    class="top-bar-action hamburger"
    aria-label="Open menu"
    aria-expanded={menuOpen}
    aria-controls="graywolf-main-nav"
    aria-haspopup="dialog"
    onclick={() => (menuOpen = true)}
  >
    <span class="top-bar-icon" aria-hidden="true">
      <!-- Inline hamburger glyph: Chonky icon allowlist lacks 'menu'. -->
      <svg
        xmlns="http://www.w3.org/2000/svg"
        width="24"
        height="24"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        stroke-width="1.75"
        stroke-linecap="round"
        stroke-linejoin="round"
      >
        <line x1="4" y1="6" x2="20" y2="6" />
        <line x1="4" y1="12" x2="20" y2="12" />
        <line x1="4" y1="18" x2="20" y2="18" />
      </svg>
    </span>
  </button>
</header>

<!-- Mobile drawer — opened by the hamburger above. -->
<Drawer
  bind:open={menuOpen}
  anchor="left"
  id="graywolf-main-nav"
  aria-label="Main navigation"
>
  <Drawer.Header>
    <a
      href="/"
      use:link
      class="drawer-brand"
      aria-label="Dashboard"
      onclick={onNavClick}
    >
      <img src={logoUrl} alt="" class="drawer-brand-logo" />
      <span class="drawer-brand-wordmark">graywolf</span>
    </a>
    <Drawer.Close aria-label="Close menu">
      <svg
        xmlns="http://www.w3.org/2000/svg"
        width="20"
        height="20"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        stroke-width="1.75"
        stroke-linecap="round"
        stroke-linejoin="round"
        aria-hidden="true"
      >
        <line x1="6" y1="6" x2="18" y2="18" />
        <line x1="18" y1="6" x2="6" y2="18" />
      </svg>
    </Drawer.Close>
  </Drawer.Header>
  <Drawer.Body>
    <nav class="drawer-nav" aria-label="Main navigation">
      {@render navItems()}
    </nav>
  </Drawer.Body>
</Drawer>

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

  /* .nav-trailing pins About to the bottom of the sidebar
     (margin-top: auto pushes it past the last nav group). */
  .nav-trailing {
    margin-top: auto;
    border-top: 1px solid var(--border-color);
    padding: 6px 0;
  }

  /* ===== Top app bar (mobile only) ===== */

  .top-bar {
    /* Hidden on desktop; shown via @media below. */
    display: none;
  }

  /* Drawer brand row (lives inside Drawer.Header on mobile only). */
  .drawer-brand {
    display: flex;
    align-items: center;
    gap: 10px;
    text-decoration: none;
    color: var(--text-secondary);
    flex: 1;
    min-width: 0;
  }
  .drawer-brand-logo {
    width: 28px;
    height: 28px;
    flex-shrink: 0;
  }
  .drawer-brand-wordmark {
    font-size: 16px;
    font-weight: 700;
    letter-spacing: 1px;
  }

  .drawer-nav {
    /* Reset list/spacing so the shared snippet renders cleanly inside the drawer. */
    display: block;
  }

  @media (max-width: 768px) {
    /* Desktop sidebar collapses on mobile; replaced by top bar + drawer. */
    .sidebar {
      display: none;
    }

    .top-bar {
      display: flex;
      align-items: center;
      gap: 4px;
      position: fixed;
      top: 0;
      left: 0;
      right: 0;
      height: calc(56px + env(safe-area-inset-top));
      padding: env(safe-area-inset-top) 8px 0
        max(8px, env(safe-area-inset-right));
      padding-left: max(8px, env(safe-area-inset-left));
      background: var(--bg-secondary);
      border-bottom: 1px solid var(--border-color);
      z-index: 100;
      box-sizing: border-box;
    }

    .top-bar-brand {
      display: inline-flex;
      align-items: center;
      gap: 8px;
      text-decoration: none;
      color: var(--text-secondary);
      padding: 0 8px;
      height: 44px;
      flex-shrink: 0;
      min-width: 0;
    }
    .top-bar-logo {
      width: 32px;
      height: 32px;
      display: block;
      flex-shrink: 0;
    }
    .top-bar-wordmark {
      font-size: 16px;
      font-weight: 700;
      letter-spacing: 1px;
      white-space: nowrap;
    }

    .top-bar-action {
      position: relative;
      display: inline-flex;
      align-items: center;
      justify-content: center;
      width: 44px;
      height: 44px;
      flex-shrink: 0;
      color: var(--text-secondary);
      background: transparent;
      border: none;
      border-radius: 6px;
      cursor: pointer;
      text-decoration: none;
      transition: background 0.15s, color 0.15s;
      /* Reset button defaults */
      padding: 0;
      font: inherit;
    }
    .top-bar-action:hover,
    .top-bar-action:focus-visible {
      background: var(--bg-hover);
      color: var(--text-primary);
    }
    .top-bar-action.active {
      color: var(--accent);
      background: var(--bg-tertiary);
    }
    .top-bar-icon {
      display: inline-flex;
      align-items: center;
      justify-content: center;
      width: 24px;
      height: 24px;
      pointer-events: none;
    }
    .top-bar-badge {
      position: absolute;
      top: 4px;
      right: 4px;
      pointer-events: none;
    }

    .top-bar-spacer {
      flex: 1 1 auto;
    }
  }
</style>
