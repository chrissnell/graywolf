<script>
  let { editOpen = $bindable(false) } = $props();
</script>

<section class="actions-section">
  <h2>Actions</h2>
  <p class="placeholder">ActionsTable placeholder.</p>
</section>

<style>
  .actions-section { display: flex; flex-direction: column; gap: 0.75rem; }
  .placeholder { color: var(--text-muted); font-size: 0.85rem; }
</style>
