<script>
  import { onMount } from 'svelte';
  import { Button, Input, Select, Icon, Tooltip } from '@chrissnell/chonky-ui';
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

  let channelId = $state('');
  let localCallRaw = $state('');
  let destCallRaw = $state('');
  let viaPath = $state('');

  let localCallError = $state('');
  let destCallError = $state('');
  let formError = $state('');

  // Advanced (collapsed by default). Only one operator in fifty cares
  // about these knobs; default everything to 0 (server picks kernel
  // defaults) and tuck the controls behind a disclosure.
  let advancedOpen = $state(false);
  let mod128 = $state(false);
  let paclen = $state(0);
  let maxframe = $state(0);
  let n2 = $state(0);
  let t1ms = $state(0);
  let t2ms = $state(0);
  let t3ms = $state(0);
  let backoff = $state('linear');

  let selectedChannel = $derived(
    channelsStore.list.find((c) => String(c.id) === String(channelId)) ?? null
  );
  let selectedIsAPRSOnly = $derived(selectedChannel?.mode === 'aprs');

  // Accept both "K0SWE" and "K0SWE-3"; SSID 0..15 is the AX.25 spec.
  const CALL_RE = /^([A-Z0-9]{1,6})(?:-([0-9]|1[0-5]))?$/;

  function parseCallsign(raw) {
    const v = (raw ?? '').toUpperCase().trim();
    if (!v) return { ok: false, error: 'required' };
    const m = CALL_RE.exec(v);
    if (!m) return { ok: false, error: 'expected CALL or CALL-N (N is 0-15)' };
    return { ok: true, call: m[1], ssid: Number(m[2] ?? 0) };
  }

  function onLocalCallBlur() {
    localCallRaw = localCallRaw.toUpperCase().trim();
    const r = parseCallsign(localCallRaw);
    localCallError = r.ok ? '' : `Your callsign: ${r.error}.`;
  }

  function onDestCallBlur() {
    destCallRaw = destCallRaw.toUpperCase().trim();
    const r = parseCallsign(destCallRaw);
    destCallError = r.ok ? '' : `Destination: ${r.error}.`;
  }

  function parseVia(value) {
    return (value ?? '')
      .split(/[\s,]+/)
      .map((s) => s.toUpperCase().trim())
      .filter((s) => s.length > 0);
  }

  function applyProfile(p) {
    if (!p) return;
    if (p.channel_id) channelId = String(p.channel_id);
    localCallRaw = p.local_ssid ? `${p.local_call ?? ''}-${p.local_ssid}` : (p.local_call ?? '');
    destCallRaw = p.dest_ssid ? `${p.dest_call ?? ''}-${p.dest_ssid}` : (p.dest_call ?? '');
    viaPath = p.via_path ?? '';
    mod128 = !!p.mod128;
    paclen = p.paclen ?? 0;
    maxframe = p.maxframe ?? 0;
    n2 = p.n2 ?? 0;
    t1ms = p.t1_ms ?? 0;
    t2ms = p.t2_ms ?? 0;
    t3ms = p.t3_ms ?? 0;
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
      formError = 'Choose a channel first.';
      return;
    }
    if (selectedIsAPRSOnly) {
      onRawTail?.(selectedChannel);
      return;
    }
    onLocalCallBlur();
    onDestCallBlur();
    if (localCallError || destCallError) return;

    const local = parseCallsign(localCallRaw);
    const dest = parseCallsign(destCallRaw);
    if (!local.ok || !dest.ok) return;

    const initial = {
      channel_id: Number(channelId),
      local_call: local.call,
      local_ssid: local.ssid,
      dest_call: dest.call,
      dest_ssid: dest.ssid,
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
  <header class="page-header">
    <div>
      <h2>New AX.25 session</h2>
      <p class="subtitle">Connect to a remote BBS or KISS-aware station over the radio.</p>
    </div>
    <a class="transcripts-link" href="#/terminal/transcripts">
      <Icon name="copy" size="sm" /> Browse transcripts
    </a>
  </header>

  {#if profilesStore.pinned.length > 0 || profilesStore.recents.length > 0}
    <section class="profile-lists" aria-label="Saved and recent connections">
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
                  Unpin
                </Button>
                <Button size="sm" variant="ghost" onclick={() => removeProfile(p)} aria-label="Remove profile">
                  <Icon name="trash-2" size="sm" />
                </Button>
              </li>
            {/each}
          </ul>
        </div>
      {/if}
      {#if profilesStore.recents.length > 0}
        <div class="profile-group">
          <strong>Recent</strong>
          <ul>
            {#each profilesStore.recents as p (p.id)}
              <li>
                <button type="button" class="profile-link" onclick={() => applyProfile(p)} aria-label={`Use profile ${profileLabel(p)}`}>
                  {profileLabel(p)}
                </button>
                <Button size="sm" variant="ghost" onclick={() => togglePin(p)} aria-label="Pin profile">
                  Pin
                </Button>
                <Button size="sm" variant="ghost" onclick={() => removeProfile(p)} aria-label="Remove recent">
                  <Icon name="x" size="sm" />
                </Button>
              </li>
            {/each}
          </ul>
        </div>
      {/if}
    </section>
  {/if}

  <section class="card" aria-label="Connection details">
    <div class="field">
      <label for="ax25-channel">Channel</label>
      <Select id="ax25-channel" bind:value={channelId} options={channelOptions} placeholder="Choose a channel" />
    </div>

    <div class="field">
      <label for="ax25-local">Your callsign</label>
      <Input
        id="ax25-local"
        bind:value={localCallRaw}
        onblur={onLocalCallBlur}
        placeholder="K0SWE  or  K0SWE-3"
        aria-invalid={!!localCallError}
        autocapitalize="characters"
        autocomplete="off"
      />
      <p class="hint">Append <code>-N</code> for an SSID (0-15). Default is no SSID.</p>
      {#if localCallError}<p class="err">{localCallError}</p>{/if}
    </div>

    <div class="field">
      <label for="ax25-dest">Destination callsign</label>
      <Input
        id="ax25-dest"
        bind:value={destCallRaw}
        onblur={onDestCallBlur}
        placeholder="W1AW  or  W1AW-3"
        aria-invalid={!!destCallError}
        autocapitalize="characters"
        autocomplete="off"
      />
      {#if destCallError}<p class="err">{destCallError}</p>{/if}
    </div>

    <div class="field">
      <label for="ax25-via">Via path <span class="optional">(optional)</span></label>
      <Input id="ax25-via" bind:value={viaPath} placeholder="WIDE2-1, RELAY" autocomplete="off" />
      <p class="hint">Comma-separated digipeater list. Leave blank for direct.</p>
    </div>
  </section>

  <details class="advanced" bind:open={advancedOpen}>
    <summary class="advanced-trigger">
      <span class="chev" aria-hidden="true">{advancedOpen ? '▾' : '▸'}</span>
      Advanced timing &amp; windowing
    </summary>
    <div class="advanced-body">
      <p class="hint">Defaults match the Linux kernel's AX.25 stack. Hover the <span class="info-glyph" aria-hidden="true">i</span> on any field for what it does.</p>
      <div class="advanced-grid">
        <label class="check">
          <input type="checkbox" bind:checked={mod128} />
          Negotiate modulo-128 (SABME)
          <Tooltip>
            <Tooltip.Trigger class="info-trigger" aria-label="What is modulo-128?">i</Tooltip.Trigger>
            <Tooltip.Content side="right">
              Use 7-bit sequence numbers (window up to 127) instead of 3-bit (window up to 7). Most TNCs and BBS hosts only speak mod-8; if the peer rejects with DM, graywolf auto-falls-back.
            </Tooltip.Content>
          </Tooltip>
        </label>

        <div class="field tight">
          <div class="label-row">
            <label for="ax25-paclen">Paclen</label>
            <Tooltip>
              <Tooltip.Trigger class="info-trigger" aria-label="What is Paclen?">i</Tooltip.Trigger>
              <Tooltip.Content side="right">
                Maximum I-frame info-field size in bytes. Larger = fewer frames per message but every retransmit is more expensive on a noisy channel. 0 picks the kernel default of 256.
              </Tooltip.Content>
            </Tooltip>
          </div>
          <Input id="ax25-paclen" type="number" min="0" max="2048" bind:value={paclen} />
        </div>

        <div class="field tight">
          <div class="label-row">
            <label for="ax25-maxframe">Window (k)</label>
            <Tooltip>
              <Tooltip.Trigger class="info-trigger" aria-label="What is the window?">i</Tooltip.Trigger>
              <Tooltip.Content side="right">
                Maximum outstanding I-frames waiting for ack (LAPB k value). Higher = better throughput on long-RTT links but unfair on shared channels. 0 picks 2 for mod-8 or 32 for mod-128.
              </Tooltip.Content>
            </Tooltip>
          </div>
          <Input id="ax25-maxframe" type="number" min="0" max="127" bind:value={maxframe} />
        </div>

        <div class="field tight">
          <div class="label-row">
            <label for="ax25-n2">Max retries</label>
            <Tooltip>
              <Tooltip.Trigger class="info-trigger" aria-label="What is the retry count?">i</Tooltip.Trigger>
              <Tooltip.Content side="right">
                N2: how many times graywolf re-sends a frame before declaring the link dead. 0 picks 10. Lower fails faster on a one-way path; higher tolerates noisier channels.
              </Tooltip.Content>
            </Tooltip>
          </div>
          <Input id="ax25-n2" type="number" min="0" max="100" bind:value={n2} />
        </div>

        <div class="field tight">
          <div class="label-row">
            <label for="ax25-t1">T1 (ms)</label>
            <Tooltip>
              <Tooltip.Trigger class="info-trigger" aria-label="What is T1?">i</Tooltip.Trigger>
              <Tooltip.Content side="right">
                Ack timer. How long to wait for the peer to acknowledge an I-frame before retransmitting. 0 picks 10000 ms (10 s). graywolf adapts T1 to measured RTT once the link is up.
              </Tooltip.Content>
            </Tooltip>
          </div>
          <Input id="ax25-t1" type="number" min="0" max="60000" bind:value={t1ms} />
        </div>

        <div class="field tight">
          <div class="label-row">
            <label for="ax25-t2">T2 (ms)</label>
            <Tooltip>
              <Tooltip.Trigger class="info-trigger" aria-label="What is T2?">i</Tooltip.Trigger>
              <Tooltip.Content side="right">
                Response / piggyback delay. After receiving an I-frame, wait up to T2 ms for an outbound frame to ride the ack on; otherwise emit a standalone RR. 0 picks 3000 ms.
              </Tooltip.Content>
            </Tooltip>
          </div>
          <Input id="ax25-t2" type="number" min="0" max="60000" bind:value={t2ms} />
        </div>

        <div class="field tight">
          <div class="label-row">
            <label for="ax25-t3">T3 (ms)</label>
            <Tooltip>
              <Tooltip.Trigger class="info-trigger" aria-label="What is T3?">i</Tooltip.Trigger>
              <Tooltip.Content side="right">
                Idle-link probe. After T3 ms with no traffic, send an RR(P=1) to confirm the peer is still there. 0 picks 300000 ms (5 min).
              </Tooltip.Content>
            </Tooltip>
          </div>
          <Input id="ax25-t3" type="number" min="0" max="600000" bind:value={t3ms} />
        </div>

        <div class="field tight">
          <div class="label-row">
            <label for="ax25-backoff">Backoff</label>
            <Tooltip>
              <Tooltip.Trigger class="info-trigger" aria-label="What is backoff?">i</Tooltip.Trigger>
              <Tooltip.Content side="right">
                How T1 grows on retries. Linear (default) matches the Linux kernel; Exponential doubles each retry up to 8x; None keeps T1 fixed at 2x RTT.
              </Tooltip.Content>
            </Tooltip>
          </div>
          <Select id="ax25-backoff" bind:value={backoff} options={[
            { value: 'none', label: 'None' },
            { value: 'linear', label: 'Linear' },
            { value: 'exponential', label: 'Exponential' },
          ]} />
        </div>
      </div>
    </div>
  </details>

  {#if formError}
    <div class="err form-err" role="alert">{formError}</div>
  {/if}

  <footer class="actions">
    <Button type="submit" variant="primary" size="lg">
      {selectedIsAPRSOnly ? 'View raw packet feed' : 'Connect'}
    </Button>
  </footer>
</form>

<style>
  .preconnect {
    display: flex;
    flex-direction: column;
    gap: 16px;
    max-width: 640px;
    margin: 0 auto;
  }
  .page-header {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 16px;
  }
  .page-header h2 {
    margin: 0 0 4px;
    font-size: 18px;
    font-weight: 600;
  }
  .subtitle {
    margin: 0;
    color: var(--color-text-muted, #666);
    font-size: 13px;
  }
  .transcripts-link {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    color: var(--color-accent, #0a84ff);
    font-size: 13px;
    text-decoration: none;
    white-space: nowrap;
  }
  .transcripts-link:hover { text-decoration: underline; }

  .profile-lists {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .profile-group {
    border: 1px solid var(--color-border, #ddd);
    padding: 8px 12px;
    border-radius: 6px;
    background: var(--color-surface, #fafafa);
  }
  .profile-group strong {
    display: block;
    margin-bottom: 6px;
    font-size: 11px;
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
    gap: 2px;
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
    padding: 6px 8px;
    cursor: pointer;
    font: inherit;
    color: var(--color-text, #222);
    border-radius: 4px;
  }
  .profile-link:hover { background: var(--color-bg, #fff); }

  .card {
    border: 1px solid var(--color-border, #ddd);
    background: var(--color-surface, #fafafa);
    border-radius: 8px;
    padding: 16px 20px;
    display: flex;
    flex-direction: column;
    gap: 14px;
  }
  .field {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .field.tight { gap: 4px; }
  .field label {
    font-size: 13px;
    font-weight: 500;
    color: var(--color-text, #222);
  }
  .field .optional {
    font-weight: 400;
    color: var(--color-text-muted, #888);
  }
  .hint {
    margin: 0;
    color: var(--color-text-muted, #888);
    font-size: 12px;
  }
  .hint code {
    font-family: var(--font-mono, ui-monospace, SFMono-Regular, Menlo, monospace);
    background: var(--color-bg, #fff);
    border: 1px solid var(--color-border, #ddd);
    padding: 0 4px;
    border-radius: 3px;
  }
  .err { color: var(--color-danger, #c41010); margin: 0; font-size: 12px; }
  .form-err {
    padding: 10px 12px;
    border: 1px solid var(--color-danger, #c41010);
    background: var(--color-danger-bg, #fff5f5);
    border-radius: 6px;
  }

  .advanced { border: 1px solid var(--color-border, #ddd); border-radius: 6px; }
  .advanced-trigger {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 10px 14px;
    cursor: pointer;
    font-size: 13px;
    color: var(--color-text, #222);
    user-select: none;
    list-style: none;
  }
  /* Hide the default <details> disclosure marker on every browser. */
  .advanced-trigger::-webkit-details-marker { display: none; }
  .advanced-trigger::marker { content: ''; }
  .advanced-trigger:hover { background: var(--color-surface, #f4f4f4); }
  .advanced[open] .advanced-trigger { border-bottom: 1px solid var(--color-border, #ddd); }
  .chev {
    display: inline-block;
    width: 12px;
    color: var(--color-text-muted, #888);
    font-size: 11px;
  }
  .advanced-body {
    padding: 14px 16px;
    background: var(--color-surface, #fafafa);
    display: flex;
    flex-direction: column;
    gap: 12px;
  }
  .advanced-grid {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 14px 20px;
  }
  .check {
    grid-column: 1 / -1;
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 13px;
  }
  .label-row {
    display: flex;
    align-items: center;
    gap: 6px;
  }
  :global(.info-trigger) {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 16px;
    height: 16px;
    border-radius: 50%;
    border: 1px solid var(--color-text-muted, #888);
    background: transparent;
    color: var(--color-text-muted, #888);
    font-size: 11px;
    font-weight: 600;
    line-height: 1;
    font-style: italic;
    cursor: help;
    padding: 0;
  }
  :global(.info-trigger:hover) {
    color: var(--color-text, #222);
    border-color: var(--color-text, #222);
  }
  .info-glyph {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 14px;
    height: 14px;
    border-radius: 50%;
    border: 1px solid currentColor;
    font-size: 9px;
    font-weight: 600;
    font-style: italic;
    line-height: 1;
  }

  .actions {
    display: flex;
    gap: 8px;
    justify-content: flex-end;
    padding-top: 4px;
  }
</style>
