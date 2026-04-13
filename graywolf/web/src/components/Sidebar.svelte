<script>
  import { link } from 'svelte-spa-router';
  import { location } from 'svelte-spa-router';
  import logoUrl from '../assets/graywolf.svg';

  const topItems = [
    { path: '/', label: 'Dashboard' },
    { path: '/map', label: 'Live Map' },
  ];

  const navGroups = [
    {
      label: 'Operations',
      items: [
        { path: '/beacons', label: 'Beacons' },
        { path: '/digipeater', label: 'Digipeater' },
        { path: '/igate', label: 'iGate' },
        { path: '/logs', label: 'Logs' },
      ],
    },
    {
      label: 'Settings',
      items: [
        { path: '/channels', label: 'Channels' },
        { path: '/audio-devices', label: 'Audio Devices' },
        { path: '/ptt', label: 'PTT' },
        { path: '/gps', label: 'GPS' },
        { path: '/simulation', label: 'Simulation' },
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
                class:active={currentPath === item.path || currentPath.startsWith(item.path + '/')}
                aria-current={currentPath === item.path ? 'page' : undefined}
              >
                <span class="nav-label">{item.label}</span>
              </a>
            </li>
          {/each}
        </ul>
      </div>
    {/each}
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
    padding: 0;
  }

  .nav-group {
    padding: 8px 0 4px;
  }

  .nav-group-label {
    font-size: 10px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 1px;
    color: var(--text-secondary);
    opacity: 0.6;
    padding: 6px 16px;
    margin: 0;
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

  .dashboard-link {
    font-weight: 600;
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
    .nav-scroll {
      flex: 1;
      display: flex;
      overflow-x: auto;
      padding: 0;
    }
    .nav-group, .dashboard-list {
      padding: 0;
      margin: 0;
      border: none;
      background: transparent;
    }
    .nav-group-label { display: none; }
    .nav-list {
      display: flex;
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
