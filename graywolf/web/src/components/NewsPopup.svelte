<script>
  import { Modal as ChonkyModal } from '@chrissnell/chonky-ui';
  import { Header, Body, Footer, Close, Description } from './Modal.svelte';
  import { releaseNotes } from '../lib/releaseNotesStore.svelte.js';
  import ReleaseNoteCard from './ReleaseNoteCard.svelte';

  // The popup is mounted by App.svelte only when unseen.length > 0.
  // We still re-check on open to handle the two-tab race where tab A
  // acked between tab B's fetch and mount.
  let open = $state(true);

  // Capture the element that had focus before the modal opened so we
  // can restore it on close. This matters for keyboard users — without
  // restore, focus falls back to <body> and they lose their place.
  let previouslyFocused = null;
  let gotItButton = $state(null);

  // Guard so the ack-on-close path only fires once even if ESC and the
  // × both somehow collapse through onClose.
  let ackFired = false;

  // Defense: if App.svelte mounted us with zero unseen (shouldn't
  // happen with the {#if} guard, but tab-race paranoia), collapse
  // silently without an ack POST.
  $effect(() => {
    if (releaseNotes.unseen.length === 0 && open) {
      open = false;
    }
  });

  $effect(() => {
    if (!open) return;
    previouslyFocused = document.activeElement;
    // Focus Got it after the modal mounts. A microtask plus a short
    // raf gives bits-ui time to place its own initial focus first so
    // we don't fight with the library.
    queueMicrotask(() => {
      requestAnimationFrame(() => {
        gotItButton?.focus();
      });
    });
    // bits-ui renders Dialog.Content in a portal, so a keydown on a
    // component-local wrapper won't see ESC. Attach at document level
    // as a safety net in case bits-ui ever dismisses without firing
    // onClose; otherwise the onClose path handles ack.
    document.addEventListener('keydown', onKeyDown, true);
    return () => document.removeEventListener('keydown', onKeyDown, true);
  });

  function ackAndClose() {
    if (ackFired) {
      open = false;
      return;
    }
    ackFired = true;
    // Optimistic — clears unseen immediately so the modal unmounts
    // cleanly; the POST fires in the background with retry-then-toast
    // (see releaseNotesStore.ack).
    releaseNotes.ack();
    open = false;
  }

  // bits-ui fires onClose on ×, overlay click, and ESC. Every dismiss
  // path routes through ack — there is only one dismiss intent.
  function handleClose() {
    ackAndClose();
    // Restore focus after the modal unmount completes. bits-ui also
    // does its own focus restore, but we captured explicitly so we
    // can be authoritative about where focus lands.
    const el = previouslyFocused;
    requestAnimationFrame(() => {
      if (el && typeof el.focus === 'function') el.focus();
    });
  }

  // Safety net: if for any reason bits-ui's ESC handler dismisses the
  // dialog without firing onClose (shouldn't happen under chonky-ui
  // 0.3.0, but we defend), intercept ESC and route through ack.
  function onKeyDown(ev) {
    if (ev.key === 'Escape' && !ackFired) {
      // Let bits-ui also do its close — our handler just ensures ack
      // happens even if the library's close path short-circuits ours.
      ackAndClose();
    }
  }

  let count = $derived(releaseNotes.unseen.length);
  let countLabel = $derived(
    count === 1
      ? '1 new release note since your last login'
      : `${count} new release notes since your last login`
  );
  // Subtitle shows the range the user is catching up on when lastSeen
  // is known; fresh installs / unseen-empty users get just the current
  // build. lastSeen comes from the /unseen endpoint envelope (see
  // dto.ReleaseNotesResponse.LastSeen).
  let subtitle = $derived(
    releaseNotes.lastSeen && releaseNotes.current
      ? `Since your last visit · v${releaseNotes.lastSeen} → v${releaseNotes.current}`
      : releaseNotes.current
      ? `v${releaseNotes.current}`
      : ''
  );
</script>

{#if open && count > 0}
  <ChonkyModal
    bind:open
    onClose={handleClose}
    class="news-popup"
  >
    <Header>
      <div class="news-header">
        <h2>What's new in Graywolf</h2>
        {#if subtitle}
          <p class="news-subtitle">{subtitle}</p>
        {/if}
      </div>
      <Close />
    </Header>
    <Body>
      <Description class="sr-only">{countLabel}</Description>
      <div class="card-stack">
        {#each releaseNotes.unseen as note (note.version)}
          <ReleaseNoteCard {note} compact={false} />
        {/each}
      </div>
    </Body>
    <Footer class="news-footer">
      <button
        bind:this={gotItButton}
        type="button"
        class="btn btn-primary"
        onclick={ackAndClose}
      >
        Got it
      </button>
    </Footer>
  </ChonkyModal>
{/if}

<style>
  .news-header {
    display: flex;
    flex-direction: column;
    gap: 2px;
    flex: 1 1 auto;
    min-width: 0;
  }
  .news-header :global(h2) {
    margin: 0;
    font-size: 18px;
    font-weight: 600;
    color: var(--text-primary);
    line-height: 1.25;
  }
  .news-subtitle {
    margin: 0;
    font-size: 12px;
    color: var(--text-muted);
    line-height: 1.3;
  }

  .card-stack {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

  .sr-only {
    position: absolute;
    width: 1px;
    height: 1px;
    padding: 0;
    margin: -1px;
    overflow: hidden;
    clip: rect(0, 0, 0, 0);
    white-space: nowrap;
    border: 0;
  }

  /* Sheet presentation on narrow viewports. Chonky's 0.3.0 Modal uses
     width: min(480px, 90vw) + max-height: 85vh at every breakpoint —
     not a full-screen sheet. Override with :global() so the media
     queries land on the dialog root bits-ui renders inside the portal. */
  @media (max-width: 768px) {
    :global(.news-popup) {
      width: 100vw !important;
      max-width: 100vw !important;
      height: 100vh !important;
      max-height: 100vh !important;
      border-radius: 0 !important;
      top: 0 !important;
      left: 0 !important;
      transform: none !important;
    }
    :global(.news-footer) {
      position: sticky;
      bottom: 0;
    }
  }

  @media (max-width: 480px) {
    :global(.news-footer) :global(button) {
      width: 100%;
    }
  }

  @media (prefers-reduced-motion: reduce) {
    :global(.news-popup),
    :global(.news-popup *),
    :global(.news-popup *::before),
    :global(.news-popup *::after) {
      animation-duration: 0s !important;
      transition-duration: 0s !important;
    }
  }
</style>
