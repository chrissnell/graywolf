<script>
  import { onMount } from 'svelte';
  import { Box, Button, Input, Toggle } from '@chrissnell/chonky-ui';
  import { mapsState, ISSUES_URL } from '../lib/settings/maps-store.svelte.js';
  import { validateCallsign } from '../lib/maps/callsign.js';
  import PageHeader from '../components/PageHeader.svelte';

  let consented = $state(false);
  let callsignInput = $state('');
  let lastError = $state(null); // { ok, status, code, message }

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
