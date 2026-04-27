<script>
  import { onMount } from 'svelte';
  import { Button, Input, Select, Toggle, Box, Radio, RadioGroup } from '@chrissnell/chonky-ui';
  import { api } from '../lib/api.js';
  import { toasts } from '../lib/stores.js';
  import PageHeader from '../components/PageHeader.svelte';
  import DataTable from '../components/DataTable.svelte';
  import Modal from '../components/Modal.svelte';
  import ConfirmDialog from '../components/ConfirmDialog.svelte';
  import FormField from '../components/FormField.svelte';
  import StationCallsignBanner from '../components/StationCallsignBanner.svelte';
  import ChannelListbox from '../lib/components/ChannelListbox.svelte';
  import { channelsStore, start as startChannelsStore, invalidate as refreshChannels, getChannel as lookupChannel } from '../lib/stores/channels.svelte.js';
  import { txPredicate, TX_REASON_FALLBACK } from '../lib/channelBacking.js';
  import {
    channelRefStatus,
    buildChannelsById,
    STATUS_OK,
    STATUS_DELETED,
  } from '../lib/channelRefStatus.js';
  import { isStationCallsignMissing } from '../lib/callsign.js';

  const DEFAULT_DEDUPE_SECONDS = 30;

  // Preset definitions. The `rule` object is spread into the save
  // payload verbatim when a preset is chosen, so these must stay
  // aligned with what detectPreset() recognizes. Only the two
  // commonly-deployed roles are offered; anything else is "Custom".
  const PRESETS = {
    fillin: {
      label: 'Fill-in digi (home / urban)',
      description:
        'Responds only to WIDE1-1. Plugs local coverage gaps without extending range. Safe default for home stations and low sites.',
      rule: { alias: 'WIDE', alias_type: 'widen', max_hops: 1, action: 'repeat' },
    },
    widearea: {
      label: 'Wide-area digi (mountain top)',
      description:
        'Responds to WIDE1-1 and WIDE2-2. Use only at high sites with real geographic coverage — otherwise you just add QRM.',
      rule: { alias: 'WIDE', alias_type: 'widen', max_hops: 2, action: 'repeat' },
    },
    custom: {
      label: 'Custom…',
      description: 'Define your own alias, alias type, and hop limit.',
      rule: null,
    },
  };

  const PRESET_OPTIONS = Object.entries(PRESETS).map(([k, v]) => ({
    value: k, label: v.label,
  }));

  const ALIAS_TYPE_OPTIONS = [
    { value: 'widen', label: 'WIDEn-N (widen)' },
    { value: 'trace', label: 'TRACEn-N (trace, inserts my callsign)' },
    { value: 'exact', label: 'Exact callsign match' },
  ];

  const ACTION_OPTIONS = [
    { value: 'repeat', label: 'Repeat' },
    { value: 'drop', label: 'Drop (suppress)' },
  ];

  let config = $state({
    enabled: false,
    my_call: '',
    dedupe_window_seconds: String(DEFAULT_DEDUPE_SECONDS),
  });

  // Callsign override UI state (Phase 4B / plan D3). Decoupled from
  // config.my_call so the text input is never blown away by a
  // radio-group flip. `callsignMode` is derived from the stored
  // my_call at load (empty → 'inherit', non-empty → 'override') and
  // is the sole authority on what the save path sends:
  //   inherit   → my_call: ""   (explicit clear on the *string DTO)
  //   override  → my_call: <uppercased trimmed myCallInput>
  // We never send `undefined` / omit the field (the nil-preserve
  // semantic isn't surfaced from this page).
  let callsignMode = $state('inherit'); // 'inherit' | 'override'
  let myCallInput = $state('');

  let rules = $state([]);

  // Station callsign (read-only on this page). Loaded on mount so the
  // radio group can show "Station callsign: <value>" as helper text
  // and the banner can render when the station callsign is unset.
  let stationCallsign = $state('');
  let stationCallsignMissing = $derived(isStationCallsignMissing(stationCallsign));
  // Subscribed from the shared channelsStore (D9). Any save that
  // changes a channel invalidates the store so backing state stays
  // in sync across tabs.
  let channels = $derived(channelsStore.list);
  // Map<id, channel> powering the rule-list pill treatment (plan D4).
  // Each rule row has from_channel and to_channel soft-FKs; we check
  // each independently so a half-broken bridge rule highlights only
  // the broken end.
  let channelsById = $derived(buildChannelsById(channels));
  let modalOpen = $state(false);
  let editing = $state(null);

  // Delete-confirmation state (bound to ConfirmDialog)
  let confirmOpen = $state(false);
  let confirmMessage = $state('');
  let pendingDeleteId = $state(null);
  // D10: rule-type radio group controls whether to_channel is hidden
  // (same-channel repeat, default) or revealed (bridge to another
  // channel). Backend DTO already has to_channel; the UI change is
  // purely about framing.
  let form = $state({
    preset: 'fillin',
    rule_type: 'same', // 'same' | 'bridge' — D10
    from_channel: '',
    to_channel: '',
    alias: 'WIDE',
    alias_type: 'widen',
    max_hops: '1',
    action: 'repeat',
    priority: 100,
    enabled: true,
  });
  let savingConfig = $state(false);

  let fromCh = $derived(lookupChannel(parseInt(form.from_channel, 10)));
  let toCh = $derived(lookupChannel(parseInt(form.to_channel, 10)));
  // Backing-diff inline warning: when bridging from one backing kind
  // to another, make the routing implication explicit (D10).
  // Independent of TX-capability: preserved verbatim.
  let bridgeBackingDiff = $derived.by(() => {
    if (form.rule_type !== 'bridge') return null;
    if (!fromCh?.backing || !toCh?.backing) return null;
    if (fromCh.backing.summary === toCh.backing.summary) return null;
    return `Bridging from ${fromCh.backing.summary} to ${toCh.backing.summary}: frames crossing this rule will change backend.`;
  });
  // TX-capability block (Phase 2, plan D3). Replaces the prior
  // non-blocking unbound warning. Keys off the worst of the two
  // endpoints: if either from_channel or to_channel is not
  // TX-capable, we block Save. For "same-channel" rules there's
  // effectively one endpoint (from == to); we surface the
  // from_channel's reason. Both endpoints are implicit TX paths in
  // a digipeater rule -- if from is broken the engine can't receive,
  // if to is broken the engine can't transmit.
  let txBlockTarget = $derived.by(() => {
    const fromCap = fromCh?.backing?.tx;
    const toCap = toCh?.backing?.tx;
    if (form.rule_type === 'bridge') {
      if (fromCh && fromCap && !fromCap.capable) {
        return { channel: fromCh, field: 'from_channel', reason: fromCap.reason || TX_REASON_FALLBACK };
      }
      if (toCh && toCap && !toCap.capable) {
        return { channel: toCh, field: 'to_channel', reason: toCap.reason || TX_REASON_FALLBACK };
      }
      return null;
    }
    if (fromCh && fromCap && !fromCap.capable) {
      return { channel: fromCh, field: 'channel', reason: fromCap.reason || TX_REASON_FALLBACK };
    }
    return null;
  });
  let txBlock = $derived(txBlockTarget ? { reason: txBlockTarget.reason, field: txBlockTarget.field } : null);
  // Escape hatch: the rule's own `enabled` flag. A disabled rule is
  // harmless (the engine won't apply it), so Save is allowed even
  // on a broken channel.
  let txBlockAllowsSave = $derived(form.enabled === false);
  let saveBlocked = $derived(!!txBlock && !txBlockAllowsSave);
  const TX_CALLOUT_ID = 'digi-tx-callout';
  let calloutEl = $state(null);
  // Scroll the callout into view on modal open when it's already
  // active, so the user sees the block before reaching Save.
  let prevModalOpen = false;
  $effect(() => {
    const isOpen = modalOpen;
    if (isOpen && !prevModalOpen) {
      queueMicrotask(() => {
        if (txBlock && calloutEl && typeof calloutEl.scrollIntoView === 'function') {
          calloutEl.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
        }
      });
    }
    prevModalOpen = isOpen;
  });

  function channelName(id) {
    const c = channels.find(c => c.id === id);
    if (c) return c.name;
    if (id) return `Channel #${id}`;
    return '—';
  }

  // Human-friendly label for an existing rule row, used in the list
  // and in the delete confirmation prompt.
  function describePreset(r) {
    const base = (r.alias || '').toUpperCase();
    if (r.action === 'repeat' && r.alias_type === 'widen' && base === 'WIDE') {
      if (r.max_hops === 1) return 'Fill-in digi';
      if (r.max_hops === 2) return 'Wide-area digi';
    }
    if (r.action === 'drop') return `Drop ${r.alias}`;
    return `${r.alias_type} ${r.alias} (max ${r.max_hops})`;
  }

  // Inverse of the preset -> rule mapping: given an existing row, pick
  // the preset key that would reproduce it, falling back to 'custom'.
  function detectPreset(r) {
    const base = (r.alias || '').toUpperCase();
    if (r.action === 'repeat' && r.alias_type === 'widen' && base === 'WIDE') {
      if (r.max_hops === 1) return 'fillin';
      if (r.max_hops === 2) return 'widearea';
    }
    return 'custom';
  }

  let displayRules = $derived(
    rules.map(r => ({
      ...r,
      // `channel_label` is the raw string fallback used when the
      // custom cell snippet below is not in play (shouldn't happen,
      // but DataTable reads this key for default rendering). The
      // pill treatment lives in the `channelCell` snippet.
      channel_label: channelName(r.from_channel),
      preset_label: describePreset(r),
      action_label: r.action === 'drop' ? 'Drop' : 'Repeat',
    }))
  );

  let hasEnabledRule = $derived(rules.some(r => r.enabled));
  let showNoRulesWarning = $derived(config.enabled && !hasEnabledRule);

  const columns = [
    { key: 'channel_label', label: 'Channel' },
    { key: 'preset_label', label: 'Rule' },
    { key: 'action_label', label: 'Action' },
    { key: 'enabled', label: 'Enabled' },
  ];

  onMount(async () => {
    // GET /digipeater always returns 200 with defaults on a fresh
    // install; the zero-value DTO produces enabled=false, my_call="",
    // dedupe_window_seconds=0 → fall back to DEFAULT_DEDUPE_SECONDS.
    // Station callsign is loaded in parallel; a failed load is treated
    // as "missing" so the enable guard kicks in rather than letting
    // the user hit a 400 on save.
    const [data] = await Promise.all([
      api.get('/digipeater'),
      (async () => {
        try {
          const s = await api.get('/station/config');
          stationCallsign = s?.callsign ?? '';
        } catch {
          stationCallsign = '';
        }
      })(),
    ]);
    const storedMyCall = data.my_call || '';
    config = {
      enabled: !!data.enabled,
      my_call: storedMyCall,
      dedupe_window_seconds: String(data.dedupe_window_seconds || DEFAULT_DEDUPE_SECONDS),
    };
    // Derive the radio-group state from the loaded value: a stored
    // empty string means "inherit from station callsign"; non-empty
    // means the operator set an explicit override (and we want to
    // round-trip it verbatim until they change it).
    callsignMode = storedMyCall ? 'override' : 'inherit';
    myCallInput = storedMyCall;
    rules = await api.get('/digipeater/rules') || [];
    startChannelsStore();
  });

  async function saveConfig(e) {
    e.preventDefault();
    const seconds = parseInt(config.dedupe_window_seconds);
    if (!Number.isFinite(seconds) || seconds <= 0) {
      toasts.error('Dedupe window must be a positive integer');
      return;
    }
    // Resolve the on-wire `my_call` from the radio-group state. The
    // DTO field is `*string`: "" = inherit, non-empty = override. We
    // never omit — see Phase 3B handoff's three-state pointer notes.
    let myCallForSave;
    if (callsignMode === 'override') {
      const v = myCallInput.trim().toUpperCase();
      if (!v) {
        toasts.error('Enter a callsign for the override, or choose "Use station callsign"');
        return;
      }
      myCallForSave = v;
    } else {
      myCallForSave = '';
    }
    savingConfig = true;
    try {
      await api.put('/digipeater', {
        enabled: config.enabled,
        my_call: myCallForSave,
        dedupe_window_seconds: seconds,
      });
      // Mirror the saved value back into the form so the input shows
      // the canonicalized (trimmed / uppercased) string.
      config.my_call = myCallForSave;
      myCallInput = myCallForSave;
      toasts.success('Digipeater config saved');
    } catch (err) {
      // ApiError.message already pulls body.error, so the backend's
      // "station callsign is not set..." string comes through verbatim.
      toasts.error(err.message || 'Failed to save Digipeater config');
    } finally {
      savingConfig = false;
    }
  }

  // Toggle guard (plan D7): block enabling while the station callsign
  // is missing. Same pattern as iGate — preventDefault on the raw
  // click/keydown short-circuits bits-ui's composed handler chain so
  // the checked state never flips. Turning OFF is always allowed.
  function handleEnableToggleClick(e) {
    if (stationCallsignMissing && !config.enabled) {
      e.preventDefault();
    }
  }
  function handleEnableToggleKeydown(e) {
    if (!stationCallsignMissing || config.enabled) return;
    if (e.key === ' ' || e.key === 'Enter') {
      e.preventDefault();
    }
  }

  function openCreate() {
    if (channels.length === 0) {
      toasts.error('Create a channel first on the Channels page');
      return;
    }
    editing = null;
    Object.assign(form, {
      preset: 'fillin',
      rule_type: 'same',
      from_channel: String(channels[0].id),
      to_channel: String(channels[0].id),
      alias: 'WIDE',
      alias_type: 'widen',
      max_hops: '1',
      action: 'repeat',
      priority: 100,
      enabled: true,
    });
    modalOpen = true;
  }

  function openEdit(row) {
    editing = row;
    const isBridge = row.to_channel && row.to_channel !== row.from_channel;
    Object.assign(form, {
      preset: detectPreset(row),
      rule_type: isBridge ? 'bridge' : 'same',
      from_channel: String(row.from_channel || ''),
      to_channel: String(row.to_channel || row.from_channel || ''),
      alias: row.alias || 'WIDE',
      alias_type: row.alias_type || 'widen',
      max_hops: String(row.max_hops ?? 1),
      action: row.action || 'repeat',
      priority: row.priority ?? 100,
      enabled: row.enabled ?? true,
    });
    modalOpen = true;
  }

  function buildRulePayload() {
    const fromChN = parseInt(form.from_channel);
    if (!Number.isFinite(fromChN) || fromChN <= 0) {
      toasts.error('From channel required');
      return null;
    }
    let toChN = fromChN;
    if (form.rule_type === 'bridge') {
      toChN = parseInt(form.to_channel);
      if (!Number.isFinite(toChN) || toChN <= 0) {
        toasts.error('To channel required for bridge rule');
        return null;
      }
    }
    const base = {
      from_channel: fromChN,
      to_channel: toChN,
      priority: form.priority || 100,
      enabled: form.enabled,
    };
    if (form.preset !== 'custom') {
      return { ...base, ...PRESETS[form.preset].rule };
    }
    const alias = (form.alias || '').trim();
    if (!alias) { toasts.error('Alias required'); return null; }
    const maxHops = parseInt(form.max_hops);
    if (!Number.isFinite(maxHops) || maxHops < 1) {
      toasts.error('Max hops must be a positive integer');
      return null;
    }
    return {
      ...base,
      alias,
      alias_type: form.alias_type,
      max_hops: maxHops,
      action: form.action,
    };
  }

  async function handleSaveRule() {
    const payload = buildRulePayload();
    if (!payload) return;
    try {
      if (editing) {
        await api.put(`/digipeater/rules/${editing.id}`, payload);
        toasts.success('Rule updated');
      } else {
        await api.post('/digipeater/rules', payload);
        toasts.success('Rule created');
      }
      modalOpen = false;
      rules = await api.get('/digipeater/rules') || [];
      refreshChannels();
    } catch (err) {
      toasts.error(err.message);
    }
  }

  function handleDelete(row) {
    pendingDeleteId = row.id;
    confirmMessage = `Delete “${describePreset(row)}” rule on ${channelName(row.from_channel)}?`;
    confirmOpen = true;
  }

  async function confirmDelete() {
    const id = pendingDeleteId;
    pendingDeleteId = null;
    if (id == null) return;
    try {
      await api.delete(`/digipeater/rules/${id}`);
      toasts.success('Rule deleted');
      rules = await api.get('/digipeater/rules') || [];
    } catch (err) {
      toasts.error(err.message);
    }
  }
</script>

<PageHeader title="Digipeater" subtitle="Digital repeater configuration and rules" />

{#if stationCallsignMissing}
  <StationCallsignBanner feature="Digipeater" id="digi-station-banner" />
{/if}

<Box title="Settings">
  <form onsubmit={saveConfig}>
    <Toggle
      bind:checked={config.enabled}
      label="Enable Digipeater"
      aria-disabled={stationCallsignMissing ? 'true' : undefined}
      aria-describedby={stationCallsignMissing ? 'digi-station-banner' : undefined}
      onclick={handleEnableToggleClick}
      onkeydown={handleEnableToggleKeydown}
    />
    <div style="margin-top: 12px;">
      <FormField label="Callsign" id="digi-call-mode"
        hint="The callsign this digipeater transmits under. Also used for preemptive digi when a packet's path explicitly names it.">
        <RadioGroup bind:value={callsignMode}>
          <div class="callsign-mode">
            <Radio value="inherit" label="Use station callsign" />
            <div class="callsign-mode-helper">
              Station callsign:
              <span class="callsign-mode-value" class:is-empty={!stationCallsign}>
                {stationCallsign || '(not set)'}
              </span>
            </div>
            <Radio value="override" label="Use a different callsign" />
            {#if callsignMode === 'override'}
              <div class="callsign-override-input">
                <Input
                  id="digi-call"
                  class="callsign-input"
                  bind:value={myCallInput}
                  placeholder="e.g. MTNTOP-1"
                  autocomplete="off"
                  spellcheck={false}
                  aria-label="Override callsign"
                />
              </div>
            {/if}
          </div>
        </RadioGroup>
      </FormField>
      <FormField label="Dedupe window (seconds)" id="digi-dedup"
        hint="Identical frames heard within this window are dropped so the same packet isn't repeated twice. 30s is the APRS convention.">
        <Input id="digi-dedup" type="number" bind:value={config.dedupe_window_seconds} placeholder="30" />
      </FormField>
    </div>
    <div class="form-actions">
      <Button variant="primary" type="submit" disabled={savingConfig}>Save Settings</Button>
    </div>
  </form>
</Box>

<div style="margin-top: 20px;">
  <PageHeader title="Digipeater Rules">
    <Button variant="primary" onclick={openCreate}>+ Add Rule</Button>
  </PageHeader>
  {#if showNoRulesWarning}
    <div class="no-rules-warning" role="status">
      <strong>No rules configured.</strong>
      The digipeater is enabled but will not repeat any packets until at least one
      enabled rule is added below. Use the <em>Fill-in digi</em> preset for a home
      station, or <em>Wide-area digi</em> for a true mountaintop site.
    </div>
  {/if}
  <DataTable
    {columns}
    rows={displayRules}
    onEdit={openEdit}
    onDelete={handleDelete}
    cells={{ channel_label: channelCell }}
  />
</div>

{#snippet channelCell(_value, row)}
  {@const isBridge = row.to_channel && row.to_channel !== row.from_channel}
  {@const fromStatus = channelRefStatus(row.from_channel, channelsById)}
  {@const toStatus = isBridge ? channelRefStatus(row.to_channel, channelsById) : null}
  <span class="rule-channel-cell">
    {@render channelPill(row.from_channel, fromStatus, isBridge ? 'From' : 'Channel')}
    {#if isBridge}
      <span class="rule-channel-arrow" aria-hidden="true">&rarr;</span>
      {@render channelPill(row.to_channel, toStatus, 'To')}
    {/if}
  </span>
{/snippet}

{#snippet channelPill(channelId, status, scope)}
  {@const broken = status.status !== STATUS_OK}
  {@const deleted = status.status === STATUS_DELETED}
  {@const displayName = status.channel?.name ?? `Channel #${channelId}`}
  {@const ariaLabel = broken
    ? (deleted
        ? `${scope} channel #${channelId} deleted`
        : `${scope} channel ${displayName} unreachable: ${status.reason}`)
    : `${scope} channel ${displayName}`}
  {@const title = broken
    ? (deleted
        ? `Channel #${channelId} deleted`
        : `Unreachable: ${status.reason}`)
    : ''}
  <span class="rule-channel-pill-wrap" aria-label={ariaLabel} {title}>
    <span
      class="rule-channel-pill"
      class:danger={broken}
      aria-hidden="true"
    >
      {#if deleted}
        Deleted
      {:else if broken}
        Unreachable
      {:else}
        {scope}
      {/if}
    </span>
    {#if !deleted}
      <span class="rule-channel-name" class:danger={broken}>
        {displayName}
      </span>
    {/if}
  </span>
{/snippet}

<Modal bind:open={modalOpen} title={editing ? 'Edit Rule' : 'New Rule'}>
    <FormField label="Rule type" id="rule-type"
      hint="Same-channel repeat is the default WIDEn-N digipeater behavior. Bridge forwards matching frames to a different channel (e.g. RF → KISS-TNC).">
      <RadioGroup bind:value={form.rule_type}>
        <div class="rule-type-row">
          <Radio value="same" label="Repeat on same channel" />
          <Radio value="bridge" label="Bridge to another channel" />
        </div>
      </RadioGroup>
    </FormField>
    <FormField label={form.rule_type === 'bridge' ? 'From channel' : 'Channel'} id="rule-channel"
      hint="Radio channel this rule applies to. Packets heard here are evaluated against the rule.">
      <ChannelListbox
        id="rule-channel"
        bind:value={form.from_channel}
        valueType="string"
        channels={channels}
        capabilityFilter={txPredicate}
      />
    </FormField>
    {#if form.rule_type === 'bridge'}
      <FormField label="To channel" id="rule-to-channel"
        hint="Matching frames are re-submitted on this channel.">
        <ChannelListbox
          id="rule-to-channel"
          bind:value={form.to_channel}
          valueType="string"
          channels={channels}
          capabilityFilter={txPredicate}
        />
      </FormField>
      {#if bridgeBackingDiff}
        <div class="bridge-diff" role="note">{bridgeBackingDiff}</div>
      {/if}
    {/if}
    <FormField label="Preset" id="rule-preset" hint={PRESETS[form.preset]?.description || ''}>
      <Select id="rule-preset" bind:value={form.preset} options={PRESET_OPTIONS} />
    </FormField>
    {#if form.preset === 'custom'}
      <FormField label="Alias" id="rule-alias"
        hint="Base alias for WIDEn-N / TRACEn-N matching (e.g. 'WIDE'), or a full callsign for exact match.">
        <Input id="rule-alias" bind:value={form.alias} placeholder="WIDE" />
      </FormField>
      <FormField label="Alias type" id="rule-alias-type">
        <Select id="rule-alias-type" bind:value={form.alias_type} options={ALIAS_TYPE_OPTIONS} />
      </FormField>
      <FormField label="Max hops (n)" id="rule-max-hops"
        hint="Largest WIDEn-N / TRACEn-N this digi will honor. 1 = fill-in, 2 = wide-area. Anything higher is discouraged.">
        <Input id="rule-max-hops" type="number" bind:value={form.max_hops} />
      </FormField>
      <FormField label="Action" id="rule-action">
        <Select id="rule-action" bind:value={form.action} options={ACTION_OPTIONS} />
      </FormField>
    {/if}
    <Toggle bind:checked={form.enabled} label="Enabled" />
    {#if txBlock}
      <div
        bind:this={calloutEl}
        id={TX_CALLOUT_ID}
        class="tx-block-callout"
        class:disabled-ok={txBlockAllowsSave}
        role="alert"
      >
        <strong>
          {#if txBlock.field === 'to_channel'}To channel{:else if txBlock.field === 'from_channel'}From channel{:else}Channel{/if}
          not TX-capable:
        </strong>
        {txBlock.reason}.
        {#if txBlockAllowsSave}
          Save allowed because this rule is disabled.
        {:else}
          Pick a different channel or fix the channel's backend on the Channels page before saving.
        {/if}
      </div>
    {/if}
    <div class="modal-actions">
      <Button onclick={() => modalOpen = false}>Cancel</Button>
      <Button
        variant="primary"
        onclick={handleSaveRule}
        disabled={saveBlocked}
        aria-describedby={txBlock ? TX_CALLOUT_ID : undefined}
      >{editing ? 'Save' : 'Create'}</Button>
    </div>
</Modal>

<ConfirmDialog
  bind:open={confirmOpen}
  title="Delete Rule"
  message={confirmMessage}
  confirmLabel="Delete"
  onConfirm={confirmDelete}
/>

<style>
  .form-actions { display: flex; justify-content: flex-end; margin-top: 16px; }
  .modal-actions { display: flex; gap: 8px; justify-content: flex-end; margin-top: 16px; }
  .no-rules-warning {
    margin: 12px 0;
    padding: 12px 16px;
    border: 1px solid var(--color-warning, #d4a72c);
    border-left-width: 4px;
    border-radius: 4px;
    background: var(--color-warning-bg, rgba(212, 167, 44, 0.12));
    color: var(--text-primary, inherit);
    line-height: 1.45;
  }
  .no-rules-warning strong { margin-right: 6px; }

  .rule-type-row {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  /* D10 — inline note when bridging between different backings. Info
     toned (not warning): the flow is legitimate but we want the
     operator to notice the cross-backend implication. */
  .bridge-diff {
    margin: 0 0 12px 0;
    padding: 8px 12px;
    border: 1px solid var(--color-info, #3b82f6);
    border-left-width: 4px;
    border-radius: 4px;
    background: var(--color-info-muted, rgba(59, 130, 246, 0.12));
    color: var(--text-primary);
    font-size: 13px;
    line-height: 1.45;
  }

  /* Phase 2 — TX-capability blocking callout. Replaces the prior
     amber unbound warning. Uses chonky-ui danger tokens so it reads
     as "you cannot save this" (vs. the bridge-diff info callout,
     which is a neutral note). When the rule is being saved as
     disabled (escape hatch) we downshift to amber so the operator
     sees Save is still available. */
  .tx-block-callout {
    margin: 12px 0 0 0;
    padding: 10px 12px;
    border: 1px solid var(--color-danger, #f85149);
    border-left-width: 4px;
    border-radius: 4px;
    background: var(--color-danger-muted, rgba(248, 81, 73, 0.15));
    color: var(--text-primary);
    font-size: 13px;
    line-height: 1.45;
  }
  .tx-block-callout.disabled-ok {
    border-color: var(--color-warning, #d29922);
    background: var(--color-warning-muted, rgba(210, 153, 34, 0.15));
  }

  /* Phase 4B — radio-group layout for the station-callsign override
     pattern. Stacks the two radios with the station-callsign helper
     tucked under the "Use station callsign" option and the override
     input revealed under the "Use a different callsign" option. */
  .callsign-mode {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .callsign-mode-helper {
    margin: 0 0 4px 24px;
    font-size: 12px;
    color: var(--color-text-muted, var(--text-secondary, #888));
  }
  .callsign-mode-value {
    font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
    font-weight: 600;
    color: var(--text-primary);
  }
  .callsign-mode-value.is-empty {
    font-family: inherit;
    font-weight: normal;
    font-style: italic;
    color: var(--color-text-muted, var(--text-secondary, #888));
  }
  .callsign-override-input {
    margin: 4px 0 0 24px;
    max-width: 280px;
  }
  /* Visual uppercase for the override input; persisted value is
     uppercased at save time. Chonky's Input forwards `class` onto the
     underlying <input>, so :global is required to reach through
     Svelte's scoped-CSS boundary (mirrors Callsign.svelte). */
  :global(input.callsign-input) {
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }

  /* aria-disabled Enable toggle mirror (plan D7): keep the control
     focusable so screen readers can announce it along with the linked
     banner, but visually signal the inert state. bits-ui renders the
     Switch as <button role="switch">; :global reaches through the
     Chonky wrapper. */
  :global([role='switch'][aria-disabled='true']) {
    opacity: 0.55;
    cursor: not-allowed;
  }

  /* Rule-list Channel column pill treatment (Phase 3 / plan D4). On
     bridge rules, from_channel and to_channel are evaluated
     independently so a half-broken rule surfaces only the broken end
     in danger tokens. Same tokens as beacons / iGate for consistency. */
  .rule-channel-cell {
    display: inline-flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px;
  }
  .rule-channel-pill-wrap {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
  }
  .rule-channel-arrow {
    color: var(--color-text-muted, #888);
    font-weight: 700;
  }
  .rule-channel-pill {
    font-size: 11px;
    font-weight: 700;
    letter-spacing: 0.5px;
    color: var(--color-info);
    background: var(--color-info-muted);
    padding: 2px 6px;
    border-radius: 3px;
    flex-shrink: 0;
  }
  .rule-channel-pill.danger {
    color: var(--color-danger, #f85149);
    background: var(--color-danger-muted, rgba(248, 81, 73, 0.15));
  }
  .rule-channel-name {
    font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
    font-size: 12px;
    color: var(--text-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    min-width: 0;
  }
  .rule-channel-name.danger {
    color: var(--color-danger, #f85149);
  }
</style>
