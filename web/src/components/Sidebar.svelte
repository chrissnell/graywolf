<script>
  import { link } from 'svelte-spa-router';
  import { location } from 'svelte-spa-router';

  const navItems = [
    { path: '/', label: 'Dashboard' },
    { path: '/channels', label: 'Channels' },
    { path: '/audio-devices', label: 'Audio Devices' },
    { path: '/ptt', label: 'PTT' },
    { path: '/kiss', label: 'KISS' },
    { path: '/agw', label: 'AGW' },
    { path: '/igate', label: 'iGate' },
    { path: '/igate/filters', label: 'iGate Filters' },
    { path: '/digipeater', label: 'Digipeater' },
    { path: '/beacons', label: 'Beacons' },
    { path: '/gps', label: 'GPS' },
    { path: '/simulation', label: 'Simulation' },
    { path: '/logs', label: 'Logs' },
  ];

  let currentPath = $state('');
  $effect(() => {
    const unsub = location.subscribe((v) => { currentPath = v; });
    return unsub;
  });
</script>

<nav class="sidebar" role="navigation" aria-label="Main navigation">
  <div class="sidebar-header">
    <h1 class="logo">graywolf</h1>
  </div>
  <ul class="nav-list">
    {#each navItems as item}
      <li>
        <a
          href={item.path}
          use:link
          class="nav-link"
          class:active={currentPath === item.path || (item.path !== '/' && currentPath.startsWith(item.path))}
          aria-current={currentPath === item.path ? 'page' : undefined}
        >
          <span class="nav-label">{item.label}</span>
        </a>
      </li>
    {/each}
  </ul>
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

  .logo {
    font-size: 18px;
    font-weight: 700;
    color: var(--accent);
    letter-spacing: 1px;
  }

  .nav-list {
    list-style: none;
    padding: 8px 0;
    flex: 1;
  }

  .nav-link {
    display: flex;
    align-items: center;
    gap: 0;
    padding: 8px 16px;
    color: var(--text-secondary);
    transition: background 0.15s, color 0.15s;
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
    padding-left: 13px;
  }

@media (max-width: 768px) {
    .sidebar {
      width: 100%;
      height: auto;
      position: fixed;
      bottom: 0;
      top: auto;
      flex-direction: row;
      border-right: none;
      border-top: 1px solid var(--border-color);
    }
    .sidebar-header { display: none; }
    .nav-list {
      display: flex;
      overflow-x: auto;
      padding: 0;
    }
    .nav-link {
      flex-direction: column;
      gap: 2px;
      padding: 8px 12px;
      font-size: 10px;
    }
    .nav-link.active {
      border-left: none;
      border-top: 2px solid var(--accent);
      padding-left: 12px;
    }
    .nav-label { white-space: nowrap; }
  }
</style>
