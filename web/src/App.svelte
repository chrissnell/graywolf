<script>
  // Placeholder landing page. Phase 6 replaces this with the full
  // configuration UI (channels, beacons, iGate, digipeater, packet log,
  // live monitor). Chonky is installed as a dependency now so the
  // Phase 6 file browser for packet captures can come online without
  // a package.json churn later.
  let now = new Date().toISOString();
</script>

<main>
  <h1>graywolf</h1>
  <p>Phase 3 scaffold — configuration UI lands in Phase 6.</p>
  <p>
    API: <code>/api/channels</code>, <code>/api/beacons</code>,
    <code>/api/health</code>
  </p>
  <p><small>{now}</small></p>
</main>

<style>
  main {
    font-family: system-ui, sans-serif;
    max-width: 640px;
    margin: 2rem auto;
    padding: 1rem;
  }
  h1 {
    font-weight: 600;
  }
</style>
