<!-- Placeholder shown by svelte-spa-router's wrap() while a lazy-loaded
     route chunk (e.g. the map, which pulls in maplibre-gl) is fetching. -->
<div class="route-loading" role="status" aria-label="Loading">
  <div class="spinner"></div>
</div>

<style>
  .route-loading {
    display: flex;
    align-items: center;
    justify-content: center;
    min-height: 60vh;
  }
  .spinner {
    width: 32px;
    height: 32px;
    border-radius: 50%;
    border: 3px solid var(--border-color, #333);
    border-top-color: var(--accent, #5b9dd9);
    animation: route-loading-spin 0.8s linear infinite;
  }
  @keyframes route-loading-spin {
    to { transform: rotate(360deg); }
  }
  @media (prefers-reduced-motion: reduce) {
    .spinner { animation: none; }
  }
</style>
