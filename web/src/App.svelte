<script>
  import './app.css';
  import Router, { location } from 'svelte-spa-router';
  import Sidebar from './components/Sidebar.svelte';
  import ToastContainer from './components/ToastContainer.svelte';

  import Login from './routes/Login.svelte';
  import Dashboard from './routes/Dashboard.svelte';
  import Channels from './routes/Channels.svelte';
  import AudioDevices from './routes/AudioDevices.svelte';
  import Ptt from './routes/Ptt.svelte';
  import TxTiming from './routes/TxTiming.svelte';
  import Kiss from './routes/Kiss.svelte';
  import Agw from './routes/Agw.svelte';
  import Igate from './routes/Igate.svelte';
  import IgateFilters from './routes/IgateFilters.svelte';
  import Digipeater from './routes/Digipeater.svelte';
  import Beacons from './routes/Beacons.svelte';
  import Gps from './routes/Gps.svelte';
  import Simulation from './routes/Simulation.svelte';
  import Logs from './routes/Logs.svelte';

  const routes = {
    '/login': Login,
    '/': Dashboard,
    '/channels': Channels,
    '/audio-devices': AudioDevices,
    '/ptt': Ptt,
    '/tx-timing': TxTiming,
    '/kiss': Kiss,
    '/agw': Agw,
    '/igate': Igate,
    '/igate/filters': IgateFilters,
    '/digipeater': Digipeater,
    '/beacons': Beacons,
    '/gps': Gps,
    '/simulation': Simulation,
    '/logs': Logs,
  };

  let currentPath = $state('');
  $effect(() => {
    const unsub = location.subscribe((v) => { currentPath = v; });
    return unsub;
  });

  let isLoginPage = $derived(currentPath === '/login');
</script>

<ToastContainer />

{#if isLoginPage}
  <Router {routes} />
{:else}
  <div class="app-layout">
    <Sidebar />
    <main class="main-content">
      <Router {routes} />
    </main>
  </div>
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
  }

  @media (max-width: 768px) {
    .main-content {
      margin-left: 0;
      margin-bottom: 60px;
      padding: 16px;
    }
  }
</style>
