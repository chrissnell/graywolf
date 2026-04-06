<script>
  let { title = '', children } = $props();
</script>

<div class="card">
  {#if title}
    <div class="card-header">{title}</div>
  {/if}
  <div class="card-body">
    {@render children()}
  </div>
</div>

<style>
  .card {
    background: var(--bg-secondary);
    border: 1px solid var(--border-color);
    border-radius: var(--radius);
    overflow: hidden;
  }
  .card-header {
    padding: 12px 16px;
    font-size: 13px;
    font-weight: 600;
    color: var(--text-secondary);
    text-transform: uppercase;
    letter-spacing: 0.5px;
    border-bottom: 1px solid var(--border-color);
    background: var(--bg-tertiary);
  }
  .card-body {
    padding: 16px;
  }
</style>
