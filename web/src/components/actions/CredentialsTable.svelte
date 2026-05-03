<script>
  let { newCredOpen = $bindable(false) } = $props();
</script>

<section class="creds-section">
  <h2>OTP Credentials</h2>
  <p class="placeholder">CredentialsTable placeholder.</p>
</section>

<style>
  .creds-section { display: flex; flex-direction: column; gap: 0.75rem; }
  .placeholder { color: var(--text-muted); font-size: 0.85rem; }
</style>
