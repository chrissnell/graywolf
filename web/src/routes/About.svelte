<script>
  import { onMount } from 'svelte';
  import ReleaseNoteCard from '../components/ReleaseNoteCard.svelte';
  import UpdateAvailableBanner from '../components/UpdateAvailableBanner.svelte';
  import { releaseNotes } from '../lib/releaseNotesStore.svelte.js';
  import { updates } from '../lib/updatesStore.svelte.js';

  let version = $state('');

  onMount(async () => {
    try {
      const d = await fetch('/api/version').then(r => r.json());
      version = d.version || '';
    } catch {}
    // Pull the full changelog for the What's new list below.
    // Re-runs every visit — cheap and keeps the list fresh if another
    // release lands while the tab is open.
    releaseNotes.fetchAll();
    // Refresh update status. The sidebar also primes this on mount,
    // but fetching here keeps About honest if the operator lands
    // directly on /about in a new tab.
    updates.fetchStatus();
  });
</script>

<div class="about-content">
  <section class="about-section" aria-labelledby="install-heading">
    <h2 id="install-heading" class="about-section-heading">This install</h2>
    <p class="about-version">Graywolf v.{version}</p>
    <p class="about-copyright">&copy; 2026 Chris Snell, NW5W</p>
  </section>

  <section class="about-section" aria-labelledby="updates-heading">
    <h2 id="updates-heading" class="about-section-heading">Updates</h2>
    {#if updates.status === 'pending'}
      <p class="updates-pending">Checking for updates…</p>
    {:else if updates.status === 'current'}
      <p class="updates-current">You're on the latest version.</p>
    {:else}
      <UpdateAvailableBanner />
    {/if}
  </section>

  <section class="about-section whats-new" aria-labelledby="whats-new-heading">
    <h2 id="whats-new-heading" class="about-section-heading" tabindex="-1">What's new</h2>

    {#if releaseNotes.loading && releaseNotes.all.length === 0}
      <div class="skeleton-stack" aria-busy="true" aria-label="Loading release notes">
        <div class="skeleton-card"></div>
        <div class="skeleton-card"></div>
      </div>
    {:else if releaseNotes.error}
      <div class="notes-error" role="alert">
        Couldn't load release notes. Try refreshing.
      </div>
    {:else if releaseNotes.all.length === 0}
      <p class="notes-empty">No release notes yet.</p>
    {:else}
      <div class="notes-list">
        {#each releaseNotes.all as note (note.version)}
          <ReleaseNoteCard {note} compact={true} />
        {/each}
      </div>
    {/if}
  </section>

  <p class="about-license">
    Released under the
    <a href="https://www.gnu.org/licenses/old-licenses/gpl-2.0.html" target="_blank" rel="noopener">
      GNU General Public License v2.0</a>.
    You are free to redistribute and modify this software under the terms of the GPL 2.0.
  </p>
  <blockquote class="about-quote">
    <p>
      "And behold, I tell you these things that ye may learn wisdom; that ye may learn
      that when ye are in the service of your fellow beings ye are only in the service of your God."
    </p>
    <cite>
      <a href="https://www.churchofjesuschrist.org/study/scriptures/bofm/mosiah/2?lang=eng&id=p17#p17"
         target="_blank" rel="noopener">
        Mosiah 2:17
      </a>
    </cite>
  </blockquote>
</div>

<style>
  .about-content {
    max-width: 560px;
  }

  /* Shared section wrapper — "This install", "Updates", "What's new". */
  .about-section {
    margin: 0 0 32px;
  }

  /* Shared heading style across all three About sections (formerly
     .whats-new h2 only). Kept the existing visual spec: 18px semibold,
     primary text, 12px bottom margin. */
  .about-section-heading {
    margin: 0 0 12px;
    font-size: 18px;
    font-weight: 600;
    color: var(--text-primary);
  }
  /* Remove the :focus outline ring on the programmatically-focused
     "What's new" heading — it gets a tabindex="-1" purely so the
     banner's dismiss handler can move focus here, not so keyboard
     users Tab into it. Hide the ring to avoid a surprising visual. */
  .about-section-heading:focus {
    outline: none;
  }

  .updates-pending,
  .updates-current {
    font-size: 13px;
    color: var(--text-muted);
    margin: 0;
  }

  .notes-list {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  .skeleton-stack {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  .skeleton-card {
    height: 64px;
    border-radius: var(--radius);
    background: var(--bg-secondary);
    border: 1px solid var(--border-color);
    position: relative;
    overflow: hidden;
  }
  .skeleton-card::after {
    content: '';
    position: absolute;
    inset: 0;
    background: linear-gradient(
      90deg,
      transparent 0%,
      color-mix(in srgb, var(--text-muted) 12%, transparent) 50%,
      transparent 100%
    );
    animation: skeleton-shimmer 1.4s infinite linear;
  }
  @keyframes skeleton-shimmer {
    from { transform: translateX(-100%); }
    to { transform: translateX(100%); }
  }

  .notes-error {
    font-size: 13px;
    padding: 10px 12px;
    border-radius: var(--radius);
    color: var(--text-secondary);
    background: var(--bg-secondary);
    border: 1px solid var(--border-color);
  }

  .notes-empty {
    margin: 0;
    font-size: 13px;
    color: var(--text-muted);
  }

  .about-version {
    font-size: 14px;
    font-weight: 700;
    margin: 0 0 8px;
  }

  .about-copyright {
    font-size: 14px;
    color: var(--text-secondary);
    margin: 0;
  }

  .about-license {
    font-size: 14px;
    line-height: 1.6;
    color: var(--text-secondary);
    margin: 0 0 32px;
  }

  .about-license a {
    color: var(--accent);
  }

  .about-quote {
    margin: 0;
    padding: 16px 20px;
    border-left: 3px solid var(--accent);
    background: var(--bg-secondary);
    border-radius: 0 var(--radius) var(--radius) 0;
  }

  .about-quote p {
    font-style: italic;
    line-height: 1.7;
    margin: 0 0 12px;
    color: var(--text-primary);
  }

  .about-quote cite {
    font-style: normal;
    font-size: 13px;
    color: var(--text-secondary);
    display: block;
    text-align: right;
  }

  .about-quote cite a {
    color: var(--accent);
  }

  @media (prefers-reduced-motion: reduce) {
    .skeleton-card::after {
      animation: none;
    }
  }
</style>
