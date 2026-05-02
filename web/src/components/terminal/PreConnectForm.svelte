<script>
  import { onMount } from 'svelte';
  import { Button, Input, Select, Collapsible, Box, EmptyState, Icon } from '@chrissnell/chonky-ui';
  import { channelsStore } from '../../lib/stores/channels.svelte.js';
  import { terminalSessions } from '../../lib/terminal/sessions.svelte.js';
  import { profilesStore, profileLabel } from '../../lib/terminal/profiles.svelte.js';

  let { onSubmit, onRawTail } = $props();

  onMount(() => {
    if (!profilesStore.loaded && !profilesStore.loading) {
      profilesStore.load();
    }
  });

  // Channel selector. Every channel is selectable; the form's submit
  // path branches on the channel's mode -- packet / aprs+packet open a
  // connected-mode session, aprs-only channels swap the route to the
  // raw-packet tail view (Plan §3f).
  let channelOptions = $derived(
    channelsStore.list.map((c) => ({
      value: String(c.id),
      label: c.name + ' (' + modeLabel(c.mode) + ')',
    }))
  );

  function modeLabel(mode) {
    if (mode === 'aprs+packet') return 'APRS+Packet';
    if (mode === 'aprs') return 'APRS only';
    if (mode === 'packet') return 'Packet';
    return mode || '?';
  }

  let selectedChannel = $derived(
    channelsStore.list.find((c) => String(c.id) === String(channelId)) ?? null
  );
  let selectedIsAPRSOnly = $derived(selectedChannel?.mode === 'aprs');

  let channelId = $state('');
  let localCall = $state('');
  // SSID 0 is a valid value (the default for any callsign), not a
  // missing-field signal. validateSSID below treats 0 as valid; the
  // form never errors on a blank SSID input.
  let localSSID = $state(0);
  let destCall = $state('');
  let destSSID = $state(0);
  let viaPath = $state('');

  // Inline errors are shown only after blur, never on every keystroke.
  let localCallError = $state('');
  let destCallError = $state('');
  let localSSIDError = $state('');
  let destSSIDError = $state('');
  let formError = $state('');

  // Advanced (collapsed by default).
  let advancedOpen = $state(false);
  let mod128 = $state(false);
  let paclen = $state(0);
  let maxframe = $state(0);
  let n2 = $state(0);
  let t1ms = $state(0);
  let t2ms = $state(0);
  let t3ms = $state(0);
  let backoff = $state('linear');

  const CALL_RE = /^[A-Z0-9]{1,6}$/;

  function normalizeCall(value) {
    return (value ?? '').toUpperCase().trim();
  }

  function validateCall(value, label) {
    const c = normalizeCall(value);
    if (!c) return `${label} is required.`;
    if (!CALL_RE.test(c)) return `${label} must be 1-6 letters/digits.`;
    return '';
  }

  function validateSSID(value, label) {
    const n = Number(value);
    if (!Number.isInteger(n) || n < 0 || n > 15) return `${label} SSID must be 0-15.`;
    return '';
  }

  function onLocalCallBlur() {
    localCall = normalizeCall(localCall);
    localCallError = validateCall(localCall, 'Your callsign');
  }

  function onDestCallBlur() {
    destCall = normalizeCall(destCall);
    destCallError = validateCall(destCall, 'Destination callsign');
  }

  function onLocalSSIDBlur() {
    localSSIDError = validateSSID(localSSID, 'Your');
  }

  function onDestSSIDBlur() {
    destSSIDError = validateSSID(destSSID, 'Destination');
  }

  function parseVia(value) {
    return (value ?? '')
      .split(/[\s,]+/)
      .map((s) => normalizeCall(s))
      .filter((s) => s.length > 0);
  }

  function applyProfile(p) {
    if (!p) return;
    if (p.channel_id) channelId = String(p.channel_id);
    localCall = p.local_call ?? '';
    localSSID = p.local_ssid ?? 0;
    destCall = p.dest_call ?? '';
    destSSID = p.dest_ssid ?? 0;
    viaPath = p.via_path ?? '';
    mod128 = !!p.mod128;
    paclen = p.paclen ?? 0;
    maxframe = p.maxframe ?? 0;
    n2 = p.n2 ?? 0;
    t1ms = p.t1_ms ?? 0;
    t2ms = p.t2_ms ?? 0;
    t3ms = p.t3_ms ?? 0;
    // Trigger validation to clear any prior errors.
    onLocalCallBlur();
    onDestCallBlur();
  }

  async function togglePin(p) {
    try {
      await profilesStore.setPinned(p.id, !p.pinned);
    } catch (err) {
      formError = String(err.message ?? err);
    }
  }

  async function removeProfile(p) {
    try {
      await profilesStore.remove(p.id);
    } catch (err) {
      formError = String(err.message ?? err);
    }
  }

  function handleSubmit(e) {
    e?.preventDefault?.();
    formError = '';
    if (!channelId) {
      formError = 'Select a channel.';
      return;
    }
    if (selectedIsAPRSOnly) {
      // APRS-only channels can't open a connected-mode session; surface
      // the raw-packet tail view instead. Operator can still scroll/
      // filter the live feed.
      onRawTail?.(selectedChannel);
      return;
    }
    onLocalCallBlur();
    onDestCallBlur();
    onLocalSSIDBlur();
    onDestSSIDBlur();
    if (localCallError || destCallError || localSSIDError || destSSIDError) return;

    const initial = {
      channel_id: Number(channelId),
      local_call: localCall,
      local_ssid: Number(localSSID),
      dest_call: destCall,
      dest_ssid: Number(destSSID),
      via: parseVia(viaPath),
      mod128,
      paclen: Number(paclen) || 0,
      maxframe: Number(maxframe) || 0,
      n2: Number(n2) || 0,
      t1_ms: Number(t1ms) || 0,
      t2_ms: Number(t2ms) || 0,
      t3_ms: Number(t3ms) || 0,
      backoff,
    };

    const id = terminalSessions.open(initial);
    if (id === null) {
      formError = 'Connection limit reached -- close a session to open another.';
      return;
    }
    onSubmit?.(id);
  }
</script>

<form class="preconnect" onsubmit={handleSubmit} novalidate>
  <EmptyState
    title="Start an AX.25 session"
    description="Connect to a remote BBS or KISS-aware station over the radio."
  />

  <div class="profile-lists">
    {#if profilesStore.pinned.length > 0}
      <div class="profile-group">
        <strong>Pinned</strong>
        <ul>
          {#each profilesStore.pinned as p (p.id)}
            <li>
              <button type="button" class="profile-link" onclick={() => applyProfile(p)} aria-label={`Use profile ${profileLabel(p)}`}>
                {profileLabel(p)}
              </button>
              <Button size="sm" variant="ghost" onclick={() => togglePin(p)} aria-label="Unpin profile">
                <Icon name="pin-off" size="sm" />
              </Button>
              <Button size="sm" variant="ghost" onclick={() => removeProfile(p)} aria-label="Remove profile">
                <Icon name="trash" size="sm" />
              </Button>
            </li>
          {/each}
        </ul>
      </div>
    {/if}
    {#if profilesStore.recents.length > 0}
      <div class="profile-group">
        <strong>Recents</strong>
        <ul>
          {#each profilesStore.recents as p (p.id)}
            <li>
              <button type="button" class="profile-link" onclick={() => applyProfile(p)} aria-label={`Use profile ${profileLabel(p)}`}>
                {profileLabel(p)}
              </button>
              <Button size="sm" variant="ghost" onclick={() => togglePin(p)} aria-label="Pin profile">
                <Icon name="pin" size="sm" />
              </Button>
              <Button size="sm" variant="ghost" onclick={() => removeProfile(p)} aria-label="Remove recent">
                <Icon name="x" size="sm" />
              </Button>
            </li>
          {/each}
        </ul>
      </div>
    {/if}
    {#if profilesStore.loaded && profilesStore.profiles.length === 0}
      <p class="empty">
        No saved or recent connections yet. Successful connects appear here automatically;
        pin one to save it permanently.
      </p>
    {/if}
    <a class="transcripts-link" href="#/terminal/transcripts">Browse saved transcripts</a>
  </div>

  <Box>
    <div class="grid">
      <label class="row">
        <span>Channel</span>
        <Select bind:value={channelId} options={channelOptions} placeholder="Choose a channel" />
      </label>

      <label class="row">
        <span>Your callsign</span>
        <Input
          bind:value={localCall}
          onblur={onLocalCallBlur}
          placeholder="K0SWE"
          aria-invalid={!!localCallError}
          autocapitalize="characters"
          autocomplete="off"
        />
        {#if localCallError}<span class="err">{localCallError}</span>{/if}
      </label>

      <label class="row narrow">
        <span>Your SSID</span>
        <Input
          type="number"
          min="0"
          max="15"
          bind:value={localSSID}
          onblur={onLocalSSIDBlur}
          aria-invalid={!!localSSIDError}
        />
        {#if localSSIDError}<span class="err">{localSSIDError}</span>{/if}
      </label>

      <label class="row">
        <span>Destination callsign</span>
        <Input
          bind:value={destCall}
          onblur={onDestCallBlur}
          placeholder="W1AW"
          aria-invalid={!!destCallError}
          autocapitalize="characters"
          autocomplete="off"
        />
        {#if destCallError}<span class="err">{destCallError}</span>{/if}
      </label>

      <label class="row narrow">
        <span>Destination SSID</span>
        <Input
          type="number"
          min="0"
          max="15"
          bind:value={destSSID}
          onblur={onDestSSIDBlur}
          aria-invalid={!!destSSIDError}
        />
        {#if destSSIDError}<span class="err">{destSSIDError}</span>{/if}
      </label>

      <label class="row">
        <span>Via path (optional)</span>
        <Input bind:value={viaPath} placeholder="WIDE2-1, RELAY" autocomplete="off" />
      </label>
    </div>
  </Box>

  <Collapsible bind:open={advancedOpen} title="Advanced">
    <div class="advanced-grid">
      <label>
        <input type="checkbox" bind:checked={mod128} />
        Negotiate modulo-128 (SABME)
      </label>
      <label>Paclen (0 = default)
        <Input type="number" min="0" max="2048" bind:value={paclen} />
      </label>
      <label>Maxframe (window k)
        <Input type="number" min="0" max="127" bind:value={maxframe} />
      </label>
      <label>N2 (max retries)
        <Input type="number" min="0" max="100" bind:value={n2} />
      </label>
      <label>T1 (ms)
        <Input type="number" min="0" max="60000" bind:value={t1ms} />
      </label>
      <label>T2 (ms)
        <Input type="number" min="0" max="60000" bind:value={t2ms} />
      </label>
      <label>T3 (ms)
        <Input type="number" min="0" max="600000" bind:value={t3ms} />
      </label>
      <label>Backoff
        <Select bind:value={backoff} options={[
          { value: 'none', label: 'None' },
          { value: 'linear', label: 'Linear' },
          { value: 'exponential', label: 'Exponential' },
        ]} />
      </label>
    </div>
  </Collapsible>

  {#if formError}
    <div class="err form-err" role="alert">{formError}</div>
  {/if}

  <div class="actions">
    <Button type="submit" variant="primary">
      {selectedIsAPRSOnly ? 'View raw packet feed' : 'Connect'}
    </Button>
  </div>
</form>

<style>
  .preconnect {
    display: flex;
    flex-direction: column;
    gap: 12px;
    max-width: 720px;
  }
  .profile-lists {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  .profile-group {
    border: 1px solid var(--color-border, #ddd);
    padding: 8px 12px;
    border-radius: 4px;
  }
  .profile-group strong {
    display: block;
    margin-bottom: 4px;
    font-size: 12px;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--color-text-muted, #666);
  }
  .profile-group ul {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .profile-group li {
    display: flex;
    align-items: center;
    gap: 4px;
  }
  .profile-link {
    flex: 1 1 auto;
    text-align: left;
    border: none;
    background: transparent;
    padding: 4px 6px;
    cursor: pointer;
    font: inherit;
    color: var(--color-text, #222);
    border-radius: 3px;
  }
  .profile-link:hover { background: var(--color-surface, #f0f0f0); }
  .empty { color: var(--color-text-muted, #666); margin: 0; font-size: 13px; }
  .transcripts-link { font-size: 12px; }
  .grid {
    display: grid;
    grid-template-columns: 1fr 90px;
    gap: 10px 16px;
  }
  .row { display: flex; flex-direction: column; gap: 4px; grid-column: 1 / 2; }
  .row.narrow { grid-column: 2 / 3; }
  .row span:first-child { font-size: 13px; color: var(--color-text-muted, #666); }
  .err { color: var(--color-danger, #c41010); font-size: 12px; }
  .form-err { padding: 8px; border: 1px solid var(--color-danger, #c41010); border-radius: 4px; }
  .advanced-grid {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 10px 16px;
    padding: 8px 0;
  }
  .actions { display: flex; gap: 8px; justify-content: flex-end; }
</style>
