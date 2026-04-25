<script>
  import { onMount } from 'svelte';
  import { Box, Button, Input, Toggle, Radio, RadioGroup } from '@chrissnell/chonky-ui';
  import { mapsState, ISSUES_URL } from '../lib/settings/maps-store.svelte.js';
  import { validateCallsign } from '../lib/maps/callsign.js';
  import { downloadsState } from '../lib/maps/downloads-store.svelte.js';
  import { US_STATES } from '../lib/maps/state-list.js';
  import { formatBytes } from '../lib/maps/format-bytes.js';
  import StatePicker from '../lib/maps/state-picker.svelte';
  import { toasts } from '../lib/stores.js';
  import PageHeader from '../components/PageHeader.svelte';

  let consented = $state(false);
  let callsignInput = $state('');
  let lastError = $state(null); // { ok, status, code, message }

  let revealedToken = $state(null);
  let revealing = $state(false);

  let pickerOpen = $state(false);

  let validation = $derived(validateCallsign(callsignInput));
  let canSubmit = $derived(consented && validation.ok && !mapsState.registering);

  onMount(() => {
    mapsState.fetchConfig();
    downloadsState.refresh().then(() => {
      if (
        [...downloadsState.items.values()].some(
          (d) => d.state === 'downloading' || d.state === 'pending',
        )
      ) {
        downloadsState.ensurePolling();
      }
    });
  });

  // Build a name lookup for the downloaded list.
  const slugToName = Object.fromEntries(US_STATES.map((s) => [s.slug, s.name]));

  let downloadedRows = $derived.by(() => {
    const rows = [];
    for (const [slug, item] of downloadsState.items) {
      if (item.state === 'complete') {
        rows.push({ slug, name: slugToName[slug] ?? slug, ...item });
      }
    }
    rows.sort((a, b) => a.name.localeCompare(b.name));
    return rows;
  });

  let activeDownloads = $derived.by(() => {
    const rows = [];
    for (const [slug, item] of downloadsState.items) {
      if (item.state === 'downloading' || item.state === 'pending' || item.state === 'error') {
        rows.push({ slug, name: slugToName[slug] ?? slug, ...item });
      }
    }
    rows.sort((a, b) => a.name.localeCompare(b.name));
    return rows;
  });

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

  // Two radio options. Offline tiles are preferred automatically when
  // downloads exist; the Graywolf row picks them up transparently and
  // falls back to online elsewhere.
  const sources = [
    {
      value: 'osm',
      label: 'OpenStreetMap public tiles',
      sublabel: 'Free, available everywhere, less polished cartography.',
    },
    {
      value: 'graywolf',
      label: 'Graywolf private maps',
      sublabel: 'Polished cartography. Requires registration.',
    },
  ];

  function isDisabled(src) {
    if (src.value === 'graywolf' && !mapsState.registered) return true;
    return false;
  }

  function onSourceChange(v) {
    mapsState.setSource(v);
  }
</script>

<PageHeader title="Maps" subtitle="Choose your basemap source" />

{#if !mapsState.registered}
  <Box title="About Graywolf private maps">
    <p class="prose">
      Graywolf can use a private basemap hosted by the project author,
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
    <label for="maps-callsign-input" class="maps-input-label-text">Your callsign</label>
    <div class="maps-row maps-row-aligned">
      <div class="maps-callsign-input">
        <Input
          id="maps-callsign-input"
          type="text"
          placeholder="N5XXX"
          bind:value={callsignInput}
          autocapitalize="characters"
          autocomplete="off"
          spellcheck={false}
          inputmode="text"
          disabled={!consented}
        />
      </div>
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
  <RadioGroup
    value={mapsState.source}
    onValueChange={onSourceChange}
    name="map-source"
    class="source-radio-group"
    aria-label="Choose a basemap source"
  >
    {#each sources as src}
      <div class="source-radio-row">
        <Radio
          value={src.value}
          label={src.label}
          disabled={isDisabled(src)}
        />
        <p class="source-sublabel">{src.sublabel}</p>
      </div>
      {#if src.value === 'graywolf' && mapsState.source === 'graywolf' && downloadsState.completed.size > 0}
        <p class="source-offline-hint">
          Using offline tiles for {downloadsState.completed.size}
          of 51 state{downloadsState.completed.size === 1 ? '' : 's'}.
          Areas without offline coverage fall back to online.
        </p>
      {/if}
    {/each}
  </RadioGroup>
</Box>

{#if mapsState.registered}
  <Box title="Offline maps">
    <p class="prose">
      Download per-state vector tiles for off-grid use. The map will use these
      automatically where coverage exists; it falls back to online tiles for
      areas you have not downloaded.
    </p>

    {#if activeDownloads.length > 0}
      <h3 class="prose-heading">In progress</h3>
      <ul class="downloaded-list">
        {#each activeDownloads as row (row.slug)}
          <li class="downloaded-row">
            <span class="downloaded-name">{row.name}</span>
            <span class="downloaded-meta">
              {#if row.state === 'downloading'}
                {formatBytes(row.bytes_downloaded)}
                {#if row.bytes_total > 0}
                  / {formatBytes(row.bytes_total)}
                  ({Math.round((row.bytes_downloaded / row.bytes_total) * 100)}%)
                {/if}
              {:else if row.state === 'error'}
                <span class="status-error">Error: {row.error_message || 'Download failed'}</span>
              {:else}
                Pending...
              {/if}
            </span>
            {#if row.state === 'downloading' && row.bytes_total > 0}
              <progress class="downloaded-progress" value={row.bytes_downloaded} max={row.bytes_total}></progress>
            {/if}
          </li>
        {/each}
      </ul>
    {/if}

    {#if downloadedRows.length === 0 && activeDownloads.length === 0}
      <p class="form-hint">No states downloaded yet.</p>
    {:else if downloadedRows.length > 0}
      <h3 class="prose-heading">Downloaded ({downloadedRows.length})</h3>
      <ul class="downloaded-list">
        {#each downloadedRows as row (row.slug)}
          <li class="downloaded-row">
            <span class="downloaded-name">{row.name}</span>
            <span class="downloaded-meta">
              {formatBytes(row.bytes_total)}
              {#if row.downloaded_at}
                · {new Date(row.downloaded_at).toLocaleDateString()}
              {/if}
            </span>
            <Button variant="default" onclick={() => downloadsState.start(row.slug)}>Re-download</Button>
            <Button variant="danger" onclick={() => downloadsState.remove(row.slug)}>Delete</Button>
          </li>
        {/each}
      </ul>
    {/if}

    <Button class="maps-cta" onclick={() => (pickerOpen = true)}>
      Add a state
    </Button>
  </Box>

  <StatePicker bind:open={pickerOpen} />
{/if}

<style>
  @import '../lib/maps/styles.css';

  .maps-input-label-text {
    display: block;
    margin-bottom: 4px;
    font-size: 13px;
    font-weight: 600;
    color: var(--text-secondary);
  }
</style>
