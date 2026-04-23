<script>
  import './app.css';
  import Router, { location } from 'svelte-spa-router';
  import { Toaster } from '@chrissnell/chonky-ui';
  import Sidebar from './components/Sidebar.svelte';
  import NewsPopup from './components/NewsPopup.svelte';
  import { start as startMessagesTransport } from './lib/messagesTransport.js';
  import { releaseNotes } from './lib/releaseNotesStore.svelte.js';
  import { unitsState } from './lib/settings/units-store.svelte.js';

  import Login from './routes/Login.svelte';
  import Dashboard from './routes/Dashboard.svelte';
  import Channels from './routes/Channels.svelte';
  import AudioDevices from './routes/AudioDevices.svelte';
  import Ptt from './routes/Ptt.svelte';
  import Kiss from './routes/Kiss.svelte';
  import Agw from './routes/Agw.svelte';
  import Igate from './routes/Igate.svelte';
  import Digipeater from './routes/Digipeater.svelte';
  import Beacons from './routes/Beacons.svelte';
  import Callsign from './routes/Callsign.svelte';
  import Gps from './routes/Gps.svelte';
  import Simulation from './routes/Simulation.svelte';
  import PositionLog from './routes/PositionLog.svelte';
  import Logs from './routes/Logs.svelte';
  import LiveMap from './routes/LiveMap.svelte';
  import About from './routes/About.svelte';
  import Preferences from './routes/Preferences.svelte';
  import Messages from './routes/Messages.svelte';

  const routes = {
    '/login': Login,
    '/': Dashboard,
    '/map': LiveMap,
    '/messages': Messages,
    '/messages/*': Messages,
    '/channels': Channels,
    '/audio-devices': AudioDevices,
    '/ptt': Ptt,
    '/kiss': Kiss,
    '/agw': Agw,
    '/igate': Igate,
    '/digipeater': Digipeater,
    '/beacons': Beacons,
    '/callsign': Callsign,
    '/gps': Gps,
    '/simulation': Simulation,
    '/position-log': PositionLog,
    '/logs': Logs,
    '/preferences': Preferences,
    '/about': About,
  };

  let currentPath = $state('');
  $effect(() => {
    const unsub = location.subscribe((v) => { currentPath = v; });
    return unsub;
  });

  let isLoginPage = $derived(currentPath === '/login');

  let version = $state('');
  let authChecked = $state(false);

  $effect(() => {
    // Probe auth state before rendering protected routes.
    // /api/auth/setup is unauthenticated, so it always works.
    fetch('/api/auth/setup')
      .then(r => r.json())
      .then(data => {
        if (data.needs_setup) {
          window.location.hash = '#/login';
          authChecked = true;
          return;
        }
        // Not first-run — check if we have a valid session.
        // Fetch version (public endpoint) in parallel with auth probe.
        fetch('/api/version').then(r => r.json()).then(d => { version = d.version; }).catch(() => {});
        return fetch('/api/status', { credentials: 'same-origin' }).then(r => {
          if (r.status === 401) window.location.hash = '#/login';
          authChecked = true;
        });
      })
      .catch(() => { authChecked = true; });
  });

  // Start the messages transport once we know the user is authenticated.
  // Running it app-wide (not per-route) keeps the sidebar unread badge
  // fresh from every page — the whole point of a global signal. Polling
  // every 5 s is cheap enough to be always-on; SSE is opt-in via `?sse=1`.
  let messagesTransportStarted = false;
  $effect(() => {
    if (authChecked && !isLoginPage && !messagesTransportStarted) {
      messagesTransportStarted = true;
      startMessagesTransport();
      // Pull release notes the user hasn't acknowledged yet. App.svelte
      // mounts <NewsPopup> only when unseen.length > 0, so an empty
      // response is a silent no-op.
      releaseNotes.fetchUnseen();
      // Pull the persisted units preference so every page formats
      // distances/altitudes/speeds the way the operator last saved.
      unitsState.fetchConfig();
    }
  });
</script>

<Toaster />

{#if isLoginPage}
  <Router {routes} />
{:else if authChecked}
  <div class="app-layout">
    <Sidebar />
    <main class="main-content" class:full-bleed={currentPath === '/map' || currentPath === '/messages' || currentPath.startsWith('/messages/')}>
      <Router {routes} />
      <footer class="app-footer">
        <a href="https://github.com/chrissnell/graywolf" target="_blank" rel="noopener">
          graywolf {version ? version : ''}
        </a>
      </footer>
    </main>
  </div>
  {#if releaseNotes.unseen.length > 0}
    <NewsPopup />
  {/if}
{/if}

<style>
  .app-layout {
    display: flex;
    min-height: 100vh;
  }
  .main-content {
    flex: 1;
    margin-left: var(--sidebar-width);
    padding: 24px;
    max-width: 1200px;
    display: flex;
    flex-direction: column;
  }
  .app-footer {
    margin-top: auto;
    padding: 24px 0 8px;
    text-align: center;
    font-size: 0.75rem;
    opacity: 0.5;
  }
  .app-footer a {
    color: inherit;
    text-decoration: none;
  }
  .app-footer a:hover {
    text-decoration: underline;
  }

  .main-content.full-bleed {
    max-width: none;
    padding: 0;
    height: 100vh;
    overflow: hidden;
  }
  .main-content.full-bleed .app-footer {
    display: none;
  }

  @media (max-width: 768px) {
    .main-content {
      margin-left: 0;
      margin-top: calc(56px + env(safe-area-inset-top));
      padding: 16px;
    }
    .main-content.full-bleed {
      height: calc(100vh - 56px - env(safe-area-inset-top));
    }
  }
</style>
