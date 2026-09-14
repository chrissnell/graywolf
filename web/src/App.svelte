<script>
  import './app.css';
  import Router, { location, replace } from 'svelte-spa-router';
  import { wrap } from 'svelte-spa-router/wrap';
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

  // Every route is dynamically imported (via svelte-spa-router's wrap) so
  // Rollup code-splits each into its own chunk fetched on navigation instead
  // of folding it into the main bundle. This is what actually keeps
  // maplibre-gl/pmtiles (the map) and xterm (the terminal) out of the
  // initial load — manualChunks alone doesn't help when the route
  // components are statically imported here, since the browser must still
  // fetch every statically-imported chunk before the app can render.
  const lazy = (loader) => wrap({ asyncComponent: loader });

  const baseRoutes = {
    '/login': lazy(() => import('./routes/Login.svelte')),
    '/': lazy(() => import('./routes/Dashboard.svelte')),
    '/map': lazy(() => import('./routes/LiveMapV2.svelte')),
    '/stations': lazy(() => import('./routes/Stations.svelte')),
    '/messages': lazy(() => import('./routes/Messages.svelte')),
    '/messages/*': lazy(() => import('./routes/Messages.svelte')),
    '/terminal': lazy(() => import('./routes/Terminal.svelte')),
    '/terminal/transcripts': lazy(() => import('./routes/TerminalTranscripts.svelte')),
    '/actions': lazy(() => import('./routes/Actions.svelte')),
    '/channels': lazy(() => import('./routes/Channels.svelte')),
    '/audio-devices': lazy(() => import('./routes/AudioDevices.svelte')),
    '/ptt': lazy(() => import('./routes/Ptt.svelte')),
    '/kiss': lazy(() => import('./routes/Kiss.svelte')),
    '/agw': lazy(() => import('./routes/Agw.svelte')),
    '/igate': lazy(() => import('./routes/Igate.svelte')),
    '/digipeater': lazy(() => import('./routes/Digipeater.svelte')),
    '/beacons': lazy(() => import('./routes/Beacons.svelte')),
    '/callsign': lazy(() => import('./routes/Callsign.svelte')),
    '/gps': lazy(() => import('./routes/Gps.svelte')),
    '/simulation': lazy(() => import('./routes/Simulation.svelte')),
    '/position-log': lazy(() => import('./routes/PositionLog.svelte')),
    '/logs': lazy(() => import('./routes/Logs.svelte')),
    '/system-logs': lazy(() => import('./routes/SystemLogs.svelte')),
    '/preferences': lazy(() => import('./routes/Preferences.svelte')),
    '/preferences/beacons': lazy(() => import('./routes/BeaconSettings.svelte')),
    '/preferences/maps': lazy(() => import('./routes/MapsSettings.svelte')),
    '/preferences/storage': lazy(() => import('./routes/StorageSettings.svelte')),
    '/preferences/messages': lazy(() => import('./routes/MessagesSettings.svelte')),
    '/about': lazy(() => import('./routes/About.svelte')),
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
          if (r.status === 401 && !isAndroid) window.location.hash = '#/login';
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
    justify-content: center;
  }
  .main-content {
    flex: 1;
    min-width: 0; /* prevent flex item from expanding beyond viewport width */
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
      margin-top: calc(56px + var(--safe-area-top));
      padding: 16px;
    }
    .main-content.full-bleed {
      height: calc(100vh - 56px - var(--safe-area-top));
      height: calc(100dvh - 56px - var(--safe-area-top));
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

  /* Force compact layout — mirrors the portrait mobile rules above but
     with higher specificity so they override all media queries. */
  :global(html.force-compact-menu) .main-content {
    margin-left: 0;
    margin-top: calc(56px + var(--safe-area-top));
    padding: 16px;
  }
  :global(html.force-compact-menu) .main-content.full-bleed {
    height: calc(100vh - 56px - var(--safe-area-top));
    height: calc(100dvh - 56px - var(--safe-area-top));
  }
</style>
