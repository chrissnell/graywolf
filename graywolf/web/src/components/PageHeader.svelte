<script>
  let { title = '', subtitle = '', children } = $props();
</script>

<div class="page-header">
  <div class="header-text">
    <h2 class="page-title">{title}</h2>
    {#if subtitle}
      <p class="page-subtitle">{subtitle}</p>
    {/if}
  </div>
  {#if children}
    <div class="header-actions">
      {@render children()}
    </div>
  {/if}
</div>

<style>
  .page-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 20px;
    gap: 16px;
    flex-wrap: wrap;
  }
  .page-title {
    font-size: 20px;
    font-weight: 600;
  }
  .page-subtitle {
    font-size: 13px;
    color: var(--text-muted);
    margin-top: 2px;
  }
  .header-actions {
    display: flex;
    gap: 8px;
    align-items: center;
  }
</style>
