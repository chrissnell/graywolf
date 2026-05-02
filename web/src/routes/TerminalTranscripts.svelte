<script>
  // Stub for /terminal/transcripts -- the real implementation lands
  // in Phase 3 (transcript logging + browse subroute). Showing
  // structured empty-state copy here keeps deep links from 404'ing
  // and lets the PreConnectForm point at this URL today.
  import { EmptyState } from '@chrissnell/chonky-ui';
</script>

<div class="page">
  <h1>Transcripts</h1>
  <EmptyState
    title="Transcripts not yet available"
    description="Session transcripts will appear here once Phase 3 ships transcript logging."
  />
  <p>
    <a href="#/terminal">Back to terminal</a>
  </p>
</div>

<style>
  .page { padding: 16px 24px; max-width: 720px; }
  h1 { font-size: 1.4rem; margin: 0 0 12px; }
</style>
