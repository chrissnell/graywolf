<script>
  // Participant chip row shown inside a tactical thread header.
  //
  // Desktop (≥768 px): horizontally scrolling row of inline chips
  //   (monogram-on-color-hash background). Fades out on the right
  //   edge so overflow is discoverable.
  // Mobile (<768 px): collapses to a single "Users, N participants"
  //   button that opens a chonky <Drawer anchor="bottom"> with a
  //   vertical list. This reclaims ~60 px of header budget where
  //   screen real estate matters most. Collapsed-by-default is NOT
  //   a setting — it's a hard breakpoint behavior (per plan).
  //
  // Tap-to-DM: navigates to `/messages?thread=dm:${callsign}` which
  // opens or creates the 1:1 thread with compose focused.
  //
  // Props:
  //   - tacticalKey  — the tactical callsign for the participants fetch
  //   - onOpenDm     — (callsign) => void — parent handles nav
  //
  // Data:
  //   - fetches GET /api/messages/tactical/{key}/participants?within=7d
  //     on mount + whenever tacticalKey changes
  //   - re-fetches on a 60 s cadence while mounted (participants
  //     drift slowly; poll is fine, no SSE hook needed)

  import { onMount } from 'svelte';
  import { Button, Drawer, Icon } from '@chrissnell/chonky-ui';
  import { getTacticalParticipants } from '../../api/messages.js';
  import { callsignColors, callsignMonogram } from '../../lib/callsignColor.js';
  import { relativeLong } from './time.js';

  /** @type {{ tacticalKey: string, onOpenDm?: (callsign: string) => void }} */
  let { tacticalKey, onOpenDm } = $props();

  /** @type {Array<{ callsign?: string, last_active?: string, message_count?: number }>} */
  let participants = $state([]);
  let drawerOpen = $state(false);
  let isMobile = $state(false);
  let refreshTimer = null;

  function matchMobile() {
    if (typeof window === 'undefined') return;
    isMobile = window.matchMedia('(max-width: 767px)').matches;
  }

  async function fetchParticipants() {
    if (!tacticalKey) { participants = []; return; }
    try {
      const env = await getTacticalParticipants(tacticalKey, { within: '7d' });
      participants = (env?.participants || []).filter(p => !!p.callsign);
    } catch {
      // Non-fatal; leave the previous list.
    }
  }

  onMount(() => {
    matchMobile();
    const mq = window.matchMedia('(max-width: 767px)');
    const on = () => matchMobile();
    mq.addEventListener?.('change', on);
    return () => mq.removeEventListener?.('change', on);
  });

  $effect(() => {
    // Re-fetch whenever the tacticalKey changes.
    void tacticalKey;
    fetchParticipants();
    clearInterval(refreshTimer);
    refreshTimer = setInterval(fetchParticipants, 60_000);
    return () => clearInterval(refreshTimer);
  });

  function openDm(call) {
    if (!call) return;
    drawerOpen = false;
    onOpenDm?.(call);
  }
</script>

{#if participants.length === 0}
  <span class="empty" data-testid="participants-empty">No participants observed yet.</span>
{:else if isMobile}
  <Button
    variant="ghost"
    size="sm"
    onclick={() => drawerOpen = true}
    aria-label={`${participants.length} participants — open list`}
    class="mobile-btn"
    data-testid="participants-collapsed"
  >
    <Icon name="users" size="sm" />
    <span>{participants.length} participants</span>
  </Button>
  <Drawer bind:open={drawerOpen} anchor="bottom">
    <Drawer.Header>
      <h3>Participants (last 7 days)</h3>
      <Drawer.Close aria-label="Close participants drawer">
        <Icon name="x" size="sm" />
      </Drawer.Close>
    </Drawer.Header>
    <Drawer.Body class="participants-body">
      <ul class="mobile-list">
        {#each participants as p}
          {@const colors = callsignColors(p.callsign || '')}
          <li>
            <button type="button" class="mobile-row" onclick={() => openDm(p.callsign)}>
              <span class="monogram-sm" style="background:{colors.bg};color:{colors.fg};border-color:{colors.stripe}">
                {callsignMonogram(p.callsign || '')}
              </span>
              <span class="mobile-text">
                <span class="mobile-call">{p.callsign}</span>
                <span class="mobile-meta">
                  last active {relativeLong(p.last_active)}
                  {#if p.message_count > 0} · {p.message_count} msg{p.message_count === 1 ? '' : 's'}{/if}
                </span>
              </span>
              <Icon name="chevron-right" size="sm" />
            </button>
          </li>
        {/each}
      </ul>
    </Drawer.Body>
  </Drawer>
{:else}
  <div class="chip-row" role="group" aria-label="Participants" data-testid="participants-row">
    {#each participants as p}
      {@const colors = callsignColors(p.callsign || '')}
      <button
        type="button"
        class="chip"
        style="background:{colors.bg};color:{colors.fg};border-color:{colors.stripe}"
        onclick={() => openDm(p.callsign)}
        title={`${p.callsign} · last active ${relativeLong(p.last_active)}${p.message_count ? ` · ${p.message_count} msgs` : ''}`}
        aria-label={`Message ${p.callsign} privately`}
        data-testid="participant-chip"
      >
        <span class="monogram">{callsignMonogram(p.callsign || '')}</span>
        <span class="chip-call">{p.callsign}</span>
      </button>
    {/each}
    <span class="fade" aria-hidden="true"></span>
  </div>
{/if}

<style>
  .empty {
    font-size: 11px;
    color: var(--color-text-dim);
    font-style: italic;
  }

  .chip-row {
    position: relative;
    display: flex;
    align-items: center;
    gap: 6px;
    overflow-x: auto;
    overflow-y: hidden;
    max-width: 100%;
    padding: 2px 0;
    scrollbar-width: thin;
  }
  .chip-row::-webkit-scrollbar { height: 4px; }
  .fade {
    position: sticky;
    right: 0;
    width: 24px;
    height: 24px;
    pointer-events: none;
    background: linear-gradient(to right, transparent, var(--color-surface));
    flex-shrink: 0;
  }

  .chip {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 3px 10px 3px 4px;
    border: 1px solid;
    border-radius: 999px;
    font-family: var(--font-mono);
    font-size: 11px;
    cursor: pointer;
    white-space: nowrap;
    flex-shrink: 0;
    transition: filter 0.12s;
  }
  .chip:hover { filter: brightness(1.15); }
  .monogram {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 20px;
    height: 20px;
    border-radius: 999px;
    background: var(--color-bg);
    color: inherit;
    font-weight: 700;
    font-size: 10px;
    letter-spacing: 0.5px;
  }
  .chip-call {
    font-weight: 600;
  }

  :global(.mobile-btn) {
    gap: 6px;
  }

  :global(.participants-body) {
    padding: 8px 0;
  }
  .mobile-list {
    list-style: none;
    padding: 0;
    margin: 0;
  }
  .mobile-row {
    width: 100%;
    background: transparent;
    border: none;
    padding: 10px 16px;
    display: flex;
    align-items: center;
    gap: 10px;
    cursor: pointer;
    color: inherit;
    font-family: var(--font-mono);
    border-bottom: 1px solid var(--color-border-subtle);
  }
  .mobile-row:hover { background: var(--color-surface-raised); }
  .monogram-sm {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 28px;
    height: 28px;
    border-radius: 999px;
    border: 1px solid;
    font-size: 11px;
    font-weight: 700;
    flex-shrink: 0;
  }
  .mobile-text {
    display: flex;
    flex-direction: column;
    gap: 2px;
    flex: 1 1 auto;
    min-width: 0;
    text-align: left;
  }
  .mobile-call {
    font-weight: 600;
    color: var(--color-text);
  }
  .mobile-meta {
    font-size: 11px;
    color: var(--color-text-muted);
  }
</style>
