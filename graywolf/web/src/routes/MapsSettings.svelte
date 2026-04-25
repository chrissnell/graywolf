<script>
  import { onMount } from 'svelte';
  import { Box, Button, Input, Toggle } from '@chrissnell/chonky-ui';
  import { mapsState, ISSUES_URL } from '../lib/settings/maps-store.svelte.js';
  import { validateCallsign } from '../lib/maps/callsign.js';
  import { toasts } from '../lib/stores.js';
  import PageHeader from '../components/PageHeader.svelte';

  let consented = $state(false);
  let callsignInput = $state('');
  let lastError = $state(null); // { ok, status, code, message }

  let revealedToken = $state(null);
  let revealing = $state(false);

  let validation = $derived(validateCallsign(callsignInput));
  let canSubmit = $derived(consented && validation.ok && !mapsState.registering);

  onMount(() => mapsState.fetchConfig());

  async function onRegister() {
    lastError = null;
    const result = await mapsState.register(callsignInput);
    if (!result.ok) {
      lastError = result;
    } else {
      callsignInput = '';
    }
  }

  async function onShowToken() {
    if (revealedToken) {
      revealedToken = null;
      return;
    }
    revealing = true;
    revealedToken = await mapsState.revealToken();
    revealing = false;
  }

  async function onCopyToken() {
    const t = revealedToken ?? mapsState.tokenOnce;
    if (!t) return;
    try {
      await navigator.clipboard.writeText(t);
      toasts.success('Token copied to clipboard');
    } catch {
      toasts.error("Couldn't copy — try the download button instead");
    }
  }

  function onDownloadToken() {
    const t = revealedToken ?? mapsState.tokenOnce;
    if (!t) return;
    const blob = new Blob(
      [`callsign: ${mapsState.callsign}\ntoken: ${t}\n`],
      { type: 'text/plain' },
    );
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `graywolf-maps-${mapsState.callsign.toLowerCase()}.txt`;
    document.body.appendChild(a);
    a.click();
    a.remove();
    URL.revokeObjectURL(url);
  }

  async function onReregister() {
    revealedToken = null;
    lastError = null;
    const result = await mapsState.register(mapsState.callsign);
    if (!result.ok) {
      lastError = result;
    }
  }

  // Three radio options. "Graywolf private maps (offline)" is disabled in
  // Plan 1 because no PMTiles download exists yet; Plan 2 enables it.
  const sources = [
    {
      value: 'osm',
      label: 'OpenStreetMap public tiles',
      sublabel: 'Free, available everywhere, less polished cartography.',
    },
    {
      value: 'graywolf',
      label: 'Graywolf private maps (online)',
      sublabel: 'Polished cartography. Requires registration and an internet connection.',
    },
    {
      value: 'graywolf-offline',
      label: 'Graywolf private maps (offline)',
      sublabel: 'Coming soon -- pre-downloaded state tiles for off-grid use.',
      disabled: true,
    },
  ];

  function isDisabled(src) {
    if (src.disabled) return true;
    if (src.value === 'graywolf' && !mapsState.registered) return true;
    return false;
  }

  function onSourceChange(v) {
    if (v === 'graywolf-offline') return; // Plan 2 stub
    mapsState.setSource(v);
  }
</script>

<PageHeader title="Maps" subtitle="Choose your basemap source" />

{#if !mapsState.registered}
  <Box title="About Graywolf private maps">
    <p class="prose">
      Graywolf can use a private, prettier basemap hosted by the project author,
      <strong>Chris Snell (NW5W)</strong>. Chris pays for the hosting and bandwidth
      personally, and provides this map to the amateur radio community at no cost.
    </p>
    <p class="prose">
      To prevent abuse from non-amateur clients, the map server requires a one-time
      registration per device.
    </p>
    <h3 class="prose-heading">What is sent during registration</h3>
    <ul class="prose-list">
      <li>Your callsign (uppercase, without -SSID).</li>
      <li>Your IP address, captured by the server.</li>
    </ul>
    <p class="prose">
      Nothing else. No email, no name, no metadata. Each install registers
      independently -- your laptop and your tablet each get their own token.
    </p>
    <Toggle
      checked={consented}
      onCheckedChange={(v) => (consented = v)}
      label="I understand and agree."
    />
  </Box>

  <Box title="Register this device">
    <div class="maps-row">
      <label class="maps-input-label">
        <span class="maps-input-label-text">Your callsign</span>
        <Input
          type="text"
          placeholder="N5XXX"
          bind:value={callsignInput}
          autocapitalize="characters"
          autocomplete="off"
          spellcheck={false}
          inputmode="text"
          disabled={!consented}
        />
      </label>
      <Button
        class="maps-cta"
        variant="primary"
        disabled={!canSubmit}
        onclick={onRegister}
      >
        {mapsState.registering ? 'Registering...' : 'Register'}
      </Button>
    </div>

    {#if callsignInput && !validation.ok}
      <p class="form-hint form-hint-error">{validation.message}</p>
    {:else if !consented}
      <p class="form-hint">Tick "I understand and agree" above to continue.</p>
    {:else}
      <p class="form-hint">We will send <code>{validation.callsign ?? '...'}</code> to <code>auth.nw5w.com</code>.</p>
    {/if}

    {#if lastError}
      <div class="error-card" role="alert">
        <h3>Registration failed</h3>
        <p>{lastError.message}</p>
        {#if lastError.code === 'device_limit_reached'}
          <p>This callsign has reached its 40-device limit. Please open an issue at the link below so the operator can rotate tokens for you.</p>
        {:else if lastError.code === 'rate_limited'}
          <p>Wait about 10 seconds and try again.</p>
        {:else if lastError.code === 'blocked'}
          <p>This callsign has been blocked. Please open an issue at the link below to ask the operator about it.</p>
        {/if}
        <a class="error-link" href={ISSUES_URL} target="_blank" rel="noreferrer noopener">
          Open a GitHub issue
        </a>
      </div>
    {/if}
  </Box>
{/if}

{#if mapsState.registered}
  <Box title="Registered">
    <p class="prose">
      This device is registered as <code>{mapsState.callsign}</code>.
      {#if mapsState.registeredAt}
        Registered {mapsState.registeredAt.toLocaleString()}.
      {/if}
    </p>

    {#if mapsState.tokenOnce}
      <div class="token-once" role="region" aria-label="Token displayed once">
        <p class="prose">
          <strong>Save this token.</strong> Servers only emit it once;
          if you lose it, click "Re-register this device" below to get a new one.
        </p>
        <code class="token-display">{mapsState.tokenOnce}</code>
        <div class="maps-row">
          <Button class="maps-cta" onclick={onCopyToken}>Copy</Button>
          <Button class="maps-cta" onclick={onDownloadToken}>Download as file</Button>
        </div>
      </div>
    {:else}
      <div class="maps-row">
        <Button class="maps-cta" onclick={onShowToken} disabled={revealing}>
          {revealing ? 'Loading...' : revealedToken ? 'Hide token' : 'Show token'}
        </Button>
        <Button class="maps-cta" variant="default" onclick={onReregister} disabled={mapsState.registering}>
          {mapsState.registering ? 'Re-registering...' : 'Re-register this device'}
        </Button>
      </div>
      {#if revealedToken}
        <code class="token-display">{revealedToken}</code>
      {/if}
    {/if}

    {#if lastError}
      <div class="error-card" role="alert">
        <h3>Re-registration failed</h3>
        <p>{lastError.message}</p>
        <a class="error-link" href={ISSUES_URL} target="_blank" rel="noreferrer noopener">
          Open a GitHub issue
        </a>
      </div>
    {/if}
  </Box>
{/if}

<Box title="Map source">
  <fieldset class="radio-group">
    <legend class="visually-hidden">Choose a basemap source</legend>
    {#each sources as src}
      <label class="radio-row" class:disabled={isDisabled(src)}>
        <input
          type="radio"
          name="map-source"
          value={src.value}
          checked={mapsState.source === src.value}
          disabled={isDisabled(src)}
          onchange={(e) => onSourceChange(e.currentTarget.value)}
        />
        <span class="radio-text">
          <span class="radio-label">{src.label}</span>
          <span class="radio-sublabel">{src.sublabel}</span>
        </span>
      </label>
    {/each}
  </fieldset>
</Box>

<style>
  @import '../lib/maps/styles.css';

  .maps-input-label {
    display: flex;
    flex-direction: column;
    gap: 4px;
    flex: 1;
  }
  .maps-input-label-text {
    font-size: 13px;
    font-weight: 600;
    color: var(--text-secondary);
  }
</style>
