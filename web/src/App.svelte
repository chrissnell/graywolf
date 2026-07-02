<script>
  import './app.css';
  import Router, { location, replace } from 'svelte-spa-router';
  import { Toaster } from '@chrissnell/chonky-ui';
  import { Platform } from './lib/platform.js';
  import Sidebar from './components/Sidebar.svelte';
  import NewsPopup from './components/NewsPopup.svelte';
  import ServerUpdatedBanner from './components/ServerUpdatedBanner.svelte';
  import { serverVersion } from './lib/stores/server-version.svelte.js';
  import { start as startMessagesTransport } from './lib/messagesTransport.js';
  import { releaseNotes } from './lib/releaseNotesStore.svelte.js';
  import { unitsState } from './lib/settings/units-store.svelte.js';
  import { themeState } from './lib/settings/theme-store.svelte.js';

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
  import SystemLogs from './routes/SystemLogs.svelte';
  import LiveMapV2 from './routes/LiveMapV2.svelte';
  import About from './routes/About.svelte';
  import Preferences from './routes/Preferences.svelte';
  import MapsSettings from './routes/MapsSettings.svelte';
  import MessagesSettings from './routes/MessagesSettings.svelte';
  import Messages from './routes/Messages.svelte';
  import Terminal from './routes/Terminal.svelte';
  import TerminalTranscripts from './routes/TerminalTranscripts.svelte';
  import Actions from './routes/Actions.svelte';
  import Bulletins from './routes/Bulletins.svelte';

  const baseRoutes = {
    '/login': Login,
    '/': Dashboard,
    '/map': LiveMapV2,
    '/messages': Messages,
    '/messages/*': Messages,
    '/bulletins': Bulletins,
    '/terminal': Terminal,
    '/terminal/transcripts': TerminalTranscripts,
    '/actions': Actions,
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
    '/system-logs': SystemLogs,
    '/preferences': Preferences,
    '/preferences/maps': MapsSettings,
    '/preferences/messages': MessagesSettings,
    '/about': About,
  };
  const routes = (() => {
    if (Platform.kind !== 'android') return baseRoutes;
    const r = { ...baseRoutes };
    delete r['/agw'];
    delete r['/login'];
    // Actions command-handlers exec shell scripts, which Android's W^X
    // sandbox forbids; the tab is hidden in the sidebar and the route
    // is dropped so a stray hash nav can't render a dead surface.
    delete r['/actions'];
    return r;
  })();

  // Derive the path straight from the router's own `location` store so it
  // stays in lockstep with the rendered route. A hand-rolled subscription
  // into a separate $state copy can lag a tick behind <Router>, and when
  // leaving a full-bleed route (/map, /messages) that stale value kept
  // `full-bleed` (padding:0) on the next page, rendering it flush against
  // the sidebar with no gap.
  let currentPath = $derived($location);

  let isLoginPage = $derived(currentPath === '/login' && Platform.kind !== 'android');

  $effect(() => {
    if (Platform.kind === 'android' && currentPath === '/login') {
      // replace() uses history.replaceState — no new history entry, so
      // pressing back doesn't loop the user through /login again. Direct
      // `window.location.hash = '#/'` would push, causing a visible
      // navigation ping-pong on Android's back button.
      replace('/');
    }
  });

  let version = $state('');
  let authChecked = $state(false);
  let authValid = $state(false);

  $effect(() => {
    // Probe auth state before rendering protected routes.
    // /api/auth/setup is unauthenticated, so it always works.
    //
    // Android skips every hash-redirect to /login: the SPA there
    // authenticates via the per-launch bearer token injected by the
    // WebView bridge (androidBridge.js), so a 401 indicates a token
    // mismatch that a reload can't fix. /login is also stripped from
    // the route map on Android, so the redirect would render a blank
    // page anyway.
    const isAndroid = Platform.kind === 'android';
    fetch('/api/auth/setup')
      .then(r => r.json())
      .then(data => {
        if (data.needs_setup && !isAndroid) {
          window.location.hash = '#/login';
          authChecked = true;
          return;
        }
        // Not first-run — check if we have a valid session.
        // Fetch version (public endpoint) in parallel with auth probe.
        fetch('/api/version').then(r => r.json()).then(d => { version = d.version; }).catch(() => {});
        return fetch('/api/status', { credentials: 'same-origin' }).then(r => {
          authValid = r.status !== 401;
          if (r.status === 401 && !isAndroid) window.location.hash = '#/login';
          authChecked = true;
        });
      })
      .catch(() => { authChecked = true; authValid = false; });
  });

  // Start the messages transport once we know the user is authenticated.
  // Running it app-wide (not per-route) keeps the sidebar unread badge
  // fresh from every page — the whole point of a global signal. Polling
  // every 5 s is cheap enough to be always-on; SSE is opt-in via `?sse=1`.
  let messagesTransportStarted = false;
  $effect(() => {
    if (authChecked && authValid && currentPath !== '' && !isLoginPage && !messagesTransportStarted) {
      messagesTransportStarted = true;
      startMessagesTransport();
      // Watch for the server build changing underneath this tab (operator
      // upgraded graywolf) and surface a reload banner. Idempotent.
      serverVersion.start();
      // Pull release notes the user hasn't acknowledged yet. App.svelte
      // mounts <NewsPopup> only when unseen.length > 0, so an empty
      // response is a silent no-op.
      releaseNotes.fetchUnseen();
      // Pull the persisted units preference so every page formats
      // distances/altitudes/speeds the way the operator last saved.
      unitsState.fetchConfig();
      themeState.fetchConfig();
    }
  });
</script>

<Toaster />

{#if isLoginPage}
  <Router {routes} />
{:else if authChecked}
  <ServerUpdatedBanner />
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
    /* dvh tracks the *visible* viewport as the mobile address bar
       collapses/expands; 100vh (the largest viewport) would push the map's
       bottom indicators behind the address bar (GH #348). vh first as a
       fallback for browsers without dvh. */
    height: 100vh;
    height: 100dvh;
    overflow: hidden;
    position: relative;
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
      height: calc(100dvh - 56px - env(safe-area-inset-top));
    }
  }

  /* Landscape phones: the sidebar becomes a slim vertical icon rail on the
     left instead of a horizontal top bar, so the map keeps the full
     viewport height — precious in landscape (GH #419). Declared after the
     max-width rule so it overrides the top-bar margins for the narrow
     landscape phones (<=768px wide) that match both. */
  @media (orientation: landscape) and (max-height: 500px) {
    .main-content {
      margin-left: calc(
        var(--landscape-rail-width) + env(safe-area-inset-left)
      );
      margin-top: 0;
      padding: 16px;
    }
    .main-content.full-bleed {
      height: 100vh;
      height: 100dvh;
    }
  }
</style>
