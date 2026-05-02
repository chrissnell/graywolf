<script>
  import { Button, Input, Select, Collapsible, Box, EmptyState } from '@chrissnell/chonky-ui';
  import { channelsStore } from '../../lib/stores/channels.svelte.js';
  import { terminalSessions } from '../../lib/terminal/sessions.svelte.js';

  let { onSubmit } = $props();

  // Channel selector. Phase-2 ships only the connected-mode capable
  // modes (packet, aprs+packet); APRS-only channels are filtered out
  // so the operator cannot pick a channel the backend will refuse.
  let channelOptions = $derived(
    channelsStore.list
      .filter((c) => c.mode === 'packet' || c.mode === 'aprs+packet')
      .map((c) => ({ value: String(c.id), label: c.name + (c.mode === 'aprs+packet' ? ' (APRS+Packet)' : ' (Packet)') }))
  );

  let channelId = $state('');
  let localCall = $state('');
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

  function handleSubmit(e) {
    e?.preventDefault?.();
    formError = '';
    onLocalCallBlur();
    onDestCallBlur();
    onLocalSSIDBlur();
    onDestSSIDBlur();
    if (!channelId) {
      formError = 'Select a channel.';
      return;
    }
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

  <div class="recents-stub" aria-disabled="true">
    <strong>Recents</strong>
    <p>Recent connections will appear here once Phase 3 ships.</p>
    <a href="#/terminal/transcripts">Browse saved transcripts</a>
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
    <Button type="submit" variant="primary">Connect</Button>
  </div>
</form>

<style>
  .preconnect {
    display: flex;
    flex-direction: column;
    gap: 12px;
    max-width: 720px;
  }
  .recents-stub {
    border: 1px dashed var(--color-border, #ddd);
    padding: 10px 14px;
    border-radius: 4px;
    color: var(--color-text-muted, #666);
    font-size: 13px;
  }
  .recents-stub strong { display: block; margin-bottom: 4px; }
  .recents-stub p { margin: 0 0 4px; }
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
