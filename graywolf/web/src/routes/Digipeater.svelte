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
  import ChannelListbox from '../lib/components/ChannelListbox.svelte';
  import { channelsStore, start as startChannelsStore, invalidate as refreshChannels, getChannel as lookupChannel } from '../lib/stores/channels.svelte.js';
  import { unboundWarning, SUMMARY_UNBOUND } from '../lib/channelBacking.js';

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
  let rules = $state([]);
  // Subscribed from the shared channelsStore (D9). Any save that
  // changes a channel invalidates the store so backing state stays
  // in sync across tabs.
  let channels = $derived(channelsStore.list);
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
  let bridgeBackingDiff = $derived.by(() => {
    if (form.rule_type !== 'bridge') return null;
    if (!fromCh?.backing || !toCh?.backing) return null;
    if (fromCh.backing.summary === toCh.backing.summary) return null;
    return `Bridging from ${fromCh.backing.summary} to ${toCh.backing.summary}: frames crossing this rule will change backend.`;
  });
  // Unbound save warning (D17): shown when either endpoint is
  // unbound (bridge) or the from_channel is unbound (same).
  let unboundTarget = $derived.by(() => {
    if (form.rule_type === 'bridge') {
      if (toCh?.backing?.summary === SUMMARY_UNBOUND) return toCh;
      if (fromCh?.backing?.summary === SUMMARY_UNBOUND) return fromCh;
      return null;
    }
    return fromCh?.backing?.summary === SUMMARY_UNBOUND ? fromCh : null;
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
    const data = await api.get('/digipeater');
    config = {
      enabled: !!data.enabled,
      my_call: data.my_call || '',
      dedupe_window_seconds: String(data.dedupe_window_seconds || DEFAULT_DEDUPE_SECONDS),
    };
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
    savingConfig = true;
    try {
      await api.put('/digipeater', {
        enabled: config.enabled,
        my_call: config.my_call.trim(),
        dedupe_window_seconds: seconds,
      });
      toasts.success('Digipeater config saved');
    } catch (err) {
      toasts.error(err.message);
    } finally {
      savingConfig = false;
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

<Box title="Settings">
  <form onsubmit={saveConfig}>
    <Toggle bind:checked={config.enabled} label="Enable Digipeater" />
    <div style="margin-top: 12px;">
      <FormField label="Callsign" id="digi-call"
        hint="The callsign this digipeater transmits under. Also used for preemptive digi when a packet's path explicitly names it.">
        <Input id="digi-call" bind:value={config.my_call} placeholder="N0CALL-1" />
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
  <DataTable {columns} rows={displayRules} onEdit={openEdit} onDelete={handleDelete} />
</div>

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
    {#if unboundTarget}
      <div class="unbound-warning" role="note">{unboundWarning(unboundTarget)}</div>
    {/if}
    <div class="modal-actions">
      <Button onclick={() => modalOpen = false}>Cancel</Button>
      <Button variant="primary" onclick={handleSaveRule}>{editing ? 'Save' : 'Create'}</Button>
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

  /* D17 — unbound-channel save warning, matching the amber callout
     pattern used in Beacons/iGate. */
  .unbound-warning {
    margin: 12px 0 0 0;
    padding: 10px 12px;
    border: 1px solid var(--color-warning, #d4a72c);
    border-left-width: 4px;
    border-radius: 4px;
    background: var(--color-warning-bg, rgba(212, 167, 44, 0.12));
    color: var(--text-primary);
    font-size: 13px;
    line-height: 1.45;
  }
</style>
