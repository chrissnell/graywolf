<script>
  import { onMount } from 'svelte';
  import { Button, Input, Toggle, Select, Box, Icon, Badge } from '@chrissnell/chonky-ui';
  import { api } from '../lib/api.js';
  import { toasts } from '../lib/stores.js';
  import PageHeader from '../components/PageHeader.svelte';
  import FormField from '../components/FormField.svelte';
  import DataTable from '../components/DataTable.svelte';
  import Modal from '../components/Modal.svelte';
  import ConfirmDialog from '../components/ConfirmDialog.svelte';
  import StationCallsignBanner from '../components/StationCallsignBanner.svelte';
  import ChannelListbox from '../lib/components/ChannelListbox.svelte';
  import { channelsStore, start as startChannelsStore, invalidate as refreshChannels, getChannel as lookupChannel } from '../lib/stores/channels.svelte.js';
  import { unboundWarning, SUMMARY_UNBOUND } from '../lib/channelBacking.js';
  import { isStationCallsignMissing } from '../lib/callsign.js';

  let activeTab = $state('config');

  // Config state — round-trip all API fields so saves don't clobber
  // unshown ones. `callsign` and `passcode` are intentionally absent:
  // Phase 3B removed them from the iGate DTO, and the PUT decoder uses
  // DisallowUnknownFields so sending them triggers a 400.
  let form = $state({
    enabled: true, server: 'rotate.aprs2.net', port: '14580',
    server_filter: '', tx_channel: 0,
    simulation_mode: false, gate_rf_to_is: true, gate_is_to_rf: false,
    rf_channel: 1, max_msg_hops: 2, software_name: 'graywolf', software_version: '0.1',
  });
  let loading = $state(false);

  // Station callsign (read-only on this page). Loaded alongside the
  // iGate config; failure is non-fatal — we treat a failed load as
  // "missing" so the banner renders and the toggle is aria-disabled,
  // rather than silently letting the user try to enable a feature that
  // will 400 on save.
  let stationCallsign = $state('');
  let stationCallsignMissing = $derived(isStationCallsignMissing(stationCallsign));
  // Shared channelsStore — form.tx_channel is integer-valued (see
  // validation matrix above); the ChannelListbox honors that via
  // valueType="number".
  let channels = $derived(channelsStore.list);
  // D17 — unbound save warning for the iGate TX channel picker.
  let selectedTxChannel = $derived(lookupChannel(form.tx_channel));
  let txUnbound = $derived(
    selectedTxChannel?.backing?.summary === SUMMARY_UNBOUND,
  );

  // Filters state
  let filters = $state([]);
  let modalOpen = $state(false);
  let editing = $state(null);
  let filterForm = $state({ type: 'prefix', pattern: '', action: 'allow', priority: 100, enabled: true });

  // Delete-confirmation state (bound to ConfirmDialog)
  let confirmOpen = $state(false);
  let confirmMessage = $state('');
  let pendingDeleteId = $state(null);

  // Broad-pattern confirmation state (separate from delete confirm so they
  // can't stomp each other).
  let broadConfirmOpen = $state(false);
  let broadConfirmMessage = $state('');

  const columns = [
    { key: 'type', label: 'Type' },
    { key: 'pattern', label: 'Pattern' },
    { key: 'action', label: 'Action' },
    { key: 'priority', label: 'Priority' },
    { key: 'enabled', label: 'Enabled' },
  ];

  const typeOptions = [
    { value: 'callsign', label: 'Callsign' },
    { value: 'prefix', label: 'Prefix' },
    { value: 'message_dest', label: 'Message Dest' },
    { value: 'object', label: 'Object' },
  ];

  const actionOptions = [
    { value: 'allow', label: 'Allow' },
    { value: 'deny', label: 'Deny' },
  ];

  // ------------------------------------------------------------------
  // Per-type UX helpers for the Pattern field.
  //
  // Keep these table-driven instead of ad-hoc {#if}s so adding a rule type
  // is one place to edit, and so placeholder/hint stay in lock-step with
  // the validation rules below.
  // ------------------------------------------------------------------

  function placeholderFor(type) {
    switch (type) {
      case 'callsign':     return 'NW5W-7';
      case 'prefix':       return 'W5';
      case 'message_dest': return 'NW5W-*';
      // Showcase the new wildcard syntax rather than a literal example —
      // the wildcard form is what this phase is introducing, and a
      // literal like "WX-001" would imply objects are exact-match only.
      case 'object':       return 'WX-*';
      default:             return '';
    }
  }

  function hintFor(type) {
    switch (type) {
      case 'callsign':
        return 'Exact match on the source callsign including SSID (e.g. NW5W-7). ' +
               '`*` is not supported here and will be rejected on save.';
      case 'prefix':
        return 'Case-insensitive prefix match on the source base callsign; the ' +
               'SSID is stripped before comparison (so NW5W-7 matches prefix NW5W). ' +
               '`*` is not supported here and will be rejected on save.';
      case 'message_dest':
        return 'Matches the addressee of a message packet. Exact match by ' +
               'default, or use a trailing `*` as a prefix wildcard ' +
               '(e.g. NW5W-* matches any SSID of NW5W). See warning above.';
      case 'object':
        return 'Matches the object or item name. Exact match by default, or use ' +
               'a trailing `*` as a prefix wildcard (e.g. WX-* matches all WX- ' +
               'objects). See warning above.';
      default:
        return '';
    }
  }

  // ------------------------------------------------------------------
  // Live Pattern validation — client-side mirror of the server-side
  // rules in dto.validateIGateRfFilterPattern. Same three checks, same
  // order. Server is authoritative; this exists so the user gets
  // feedback before hitting Save.
  //
  // Copy differs intentionally: UI uses sentence case with a trailing
  // period (idiomatic for UI labels / error slots); the Go side uses
  // lowercase, no period (idiomatic Go errors per ST1005). If the user
  // bypasses client validation, the toast will show the Go wording.
  // ------------------------------------------------------------------

  function validatePattern(type, pattern) {
    const trimmed = (pattern ?? '').trim();
    if (trimmed === '' || trimmed === '*') {
      return 'Pattern must not be empty or a bare wildcard.';
    }
    if (trimmed.includes('*') && (type === 'callsign' || type === 'prefix')) {
      return '`*` wildcard is only supported for Message Dest and Object types.';
    }
    const starIdx = trimmed.indexOf('*');
    if (starIdx !== -1 && starIdx !== trimmed.length - 1) {
      return '`*` is only supported as a trailing wildcard.';
    }
    return '';
  }

  // Silence the "empty pattern" error on an untouched, brand-new form so
  // the user isn't greeted with a red error the moment the modal opens.
  // Any keystroke (or type change with non-empty pattern) re-runs
  // validation through the same rules.
  let patternTouched = $state(false);

  let patternError = $derived.by(() => {
    if (!patternTouched && (filterForm.pattern ?? '').trim() === '') return '';
    return validatePattern(filterForm.type, filterForm.pattern);
  });

  // ------------------------------------------------------------------
  // Broad-pattern heuristic — the user is about to gate a large slice of
  // APRS-IS traffic to RF. Require an explicit confirmation so they
  // don't flood their local frequency with a rushed Save.
  //
  // - Prefix ≤ BROAD_PATTERN_MAX_STATIC_CHARS chars  →  K / W / K5
  // - Wildcard rule whose static prefix (pattern without trailing *)
  //   is ≤ BROAD_PATTERN_MAX_STATIC_CHARS chars  →  B* / BL*
  // ------------------------------------------------------------------

  const BROAD_PATTERN_MAX_STATIC_CHARS = 2;

  function isBroadPattern(form) {
    if (form.action !== 'allow') return false;
    const p = (form.pattern ?? '').trim();
    if (form.type === 'prefix') {
      return p.length > 0 && p.length <= BROAD_PATTERN_MAX_STATIC_CHARS;
    }
    if (form.type === 'message_dest' || form.type === 'object') {
      if (!p.endsWith('*')) return false;
      const staticPrefix = p.slice(0, -1);
      return staticPrefix.length > 0 && staticPrefix.length <= BROAD_PATTERN_MAX_STATIC_CHARS;
    }
    return false;
  }

  onMount(async () => {
    // GET /igate/config always returns 200 with defaults on a fresh
    // install. The DTO constructor server-side seeds non-empty defaults
    // for string/numeric fields (server, port, software_name, etc.) so
    // no UI-side || fallbacks are needed. Booleans still use ??
    // because the server can't distinguish "unset" from "explicit
    // false", and tx_channel falls back to the first available channel
    // since the default only makes sense relative to the live list.
    startChannelsStore();
    // Invalidate synchronously so the default-channel fallback below
    // sees a fresh list; then the poller keeps it current.
    await refreshChannels();
    // Load station callsign in parallel with the iGate config. A
    // failed load is treated as "missing" so the banner renders — we
    // don't want to lull the user into thinking the feature is
    // enable-able when the next save would 400.
    await Promise.all([
      (async () => {
        try {
          const s = await api.get('/station/config');
          stationCallsign = s?.callsign ?? '';
        } catch {
          stationCallsign = '';
        }
      })(),
      (async () => {
        const data = await api.get('/igate/config');
        const defaultCh = channels.length ? Math.min(...channels.map(c => c.id)) : 0;
        form = {
          enabled: data.enabled ?? false,
          server: data.server,
          port: String(data.port),
          server_filter: data.server_filter ?? '',
          tx_channel: data.tx_channel || defaultCh,
          simulation_mode: data.simulation_mode ?? false,
          gate_rf_to_is: data.gate_rf_to_is ?? true,
          gate_is_to_rf: data.gate_is_to_rf ?? false,
          rf_channel: data.rf_channel,
          max_msg_hops: data.max_msg_hops,
          software_name: data.software_name,
          software_version: data.software_version,
        };
        filters = await api.get('/igate/filters') || [];
      })(),
    ]);
  });

  // Config handlers
  function validateConfig() {
    // Callsign-required validation moved to the backend per Phase 3B:
    // enabling iGate without a station callsign returns HTTP 400 with
    // a human-readable message that we surface verbatim. The UI's job
    // is now to pre-empt that path via the aria-disabled toggle guard
    // below (handleEnableToggleClick / handleEnableToggleKeydown).
    return true;
  }

  async function handleSave(e) {
    e.preventDefault();
    if (!validateConfig()) return;
    loading = true;
    try {
      // Build the save body explicitly — do NOT spread `form`. The
      // iGate PUT decoder uses DisallowUnknownFields (Phase 3B), so an
      // unexpected `callsign`/`passcode` key would return 400 even
      // though both were removed from the form state above. Being
      // explicit also makes future DTO drift a compile-time concern.
      const body = {
        enabled: form.enabled,
        server: form.server,
        port: parseInt(form.port),
        server_filter: form.server_filter,
        tx_channel: parseInt(form.tx_channel),
        simulation_mode: form.simulation_mode,
        gate_rf_to_is: form.gate_rf_to_is,
        gate_is_to_rf: form.gate_is_to_rf,
        rf_channel: form.rf_channel,
        max_msg_hops: form.max_msg_hops,
        software_name: form.software_name,
        software_version: form.software_version,
      };
      await api.put('/igate/config', body);
      toasts.success('iGate config saved');
      refreshChannels();
    } catch (err) {
      // ApiError.message already prefers body.error (see lib/api.js),
      // so the backend's "station callsign is not set..." string is
      // surfaced verbatim when the enable-guard fires.
      toasts.error(err.message || 'Failed to save iGate config');
    } finally {
      loading = false;
    }
  }

  // Toggle guard: when the station callsign is missing, flipping the
  // Enable toggle ON is blocked at the event boundary. Turning OFF is
  // always allowed. We intercept `onclick` and `onkeydown` (Space /
  // Enter) and call preventDefault, which short-circuits bits-ui's
  // composed handler chain (see composeHandlers in svelte-toolbelt).
  // Using aria-disabled (not the real `disabled` attribute) per the
  // plan's D7 so the control stays keyboard-focusable and screen
  // readers announce the linked banner via aria-describedby.
  function handleEnableToggleClick(e) {
    if (stationCallsignMissing && !form.enabled) {
      e.preventDefault();
    }
  }
  function handleEnableToggleKeydown(e) {
    if (!stationCallsignMissing || form.enabled) return;
    if (e.key === ' ' || e.key === 'Enter') {
      e.preventDefault();
    }
  }

  // Filter handlers
  function openCreate() {
    editing = null;
    filterForm = { type: 'prefix', pattern: '', action: 'allow', priority: 100, enabled: true };
    patternTouched = false;
    modalOpen = true;
  }

  function openEdit(row) {
    editing = row;
    filterForm = { ...row };
    // An existing rule's pattern has already been saved — if it's now
    // invalid under the new validation rules the user should see that
    // immediately rather than only after typing.
    patternTouched = true;
    modalOpen = true;
  }

  function validateFilter() {
    // Force live error to surface even if the user tabbed straight to
    // Save without touching Pattern.
    patternTouched = true;
    return !patternError;
  }

  async function persistFilter() {
    // Strip fields not in IGateRfFilterRequest DTO (backend rejects unknown fields)
    const { id: _id, ...data } = filterForm;
    try {
      if (editing) {
        await api.put(`/igate/filters/${editing.id}`, data);
        toasts.success('Filter updated');
      } else {
        await api.post('/igate/filters', data);
        toasts.success('Filter created');
      }
      modalOpen = false;
      filters = await api.get('/igate/filters') || [];
    } catch (err) {
      toasts.error(err.message);
    }
  }

  async function handleFilterSave() {
    if (!validateFilter()) return;
    if (isBroadPattern(filterForm)) {
      broadConfirmMessage =
        `This rule will gate a very large number of packets to RF and may flood ` +
        `your local APRS frequency. Consider a narrower pattern, or test in ` +
        `simulation mode first. Save anyway?`;
      broadConfirmOpen = true;
      return;
    }
    await persistFilter();
  }

  function handleDelete(row) {
    pendingDeleteId = row.id;
    confirmMessage = `Delete the ${row.type} rule for “${row.pattern}”?`;
    confirmOpen = true;
  }

  async function confirmDelete() {
    const id = pendingDeleteId;
    pendingDeleteId = null;
    if (id == null) return;
    try {
      await api.delete(`/igate/filters/${id}`);
      toasts.success('Rule deleted');
      filters = await api.get('/igate/filters') || [];
    } catch (err) {
      toasts.error(err.message);
    }
  }

  // Trailing-`*` detection for the DataTable pattern cell. Mirrors the
  // engine's definition of "is this a wildcard rule?" so the visual
  // grouping matches runtime behavior.
  function isWildcardPattern(p) {
    return typeof p === 'string' && p.trim().endsWith('*');
  }
</script>

<PageHeader title="iGate" subtitle="Internet gateway configuration" />

<div class="tabs">
  <button class="tab" class:active={activeTab === 'config'} onclick={() => activeTab = 'config'}>Connection</button>
  <button class="tab" class:active={activeTab === 'filters'} onclick={() => activeTab = 'filters'}>APRS-IS Feed & TX Rules</button>
</div>

<div class="tab-panel" class:hidden={activeTab !== 'config'}>
  <p class="tab-doc">
    Connection settings for the APRS-IS network. When enabled, graywolf logs in to an
    APRS-IS server using the station callsign and gates eligible RF-heard traffic
    up to the internet.
  </p>
  {#if stationCallsignMissing}
    <StationCallsignBanner feature="iGate" id="igate-station-banner" />
  {/if}
  <Box>
    <form onsubmit={handleSave}>
      <!-- Discoverability row: the iGate identity now lives on the
           Station Callsign page. This row is purely informational so
           users migrating from the old iGate form know where to look. -->
      <div class="station-row">
        <span class="station-row-label">Station callsign:</span>
        <span class="station-row-value" class:is-empty={!stationCallsign}>
          {stationCallsign || '(not set)'}
        </span>
        <a class="station-row-link" href="#/callsign">[Change]</a>
      </div>
      <Toggle
        bind:checked={form.enabled}
        label="Enable iGate"
        aria-disabled={stationCallsignMissing ? 'true' : undefined}
        aria-describedby={stationCallsignMissing ? 'igate-station-banner' : undefined}
        onclick={handleEnableToggleClick}
        onkeydown={handleEnableToggleKeydown}
      />
      <div style="margin-top: 16px;">
        <FormField label="APRS-IS Server" id="ig-server">
          <Input id="ig-server" bind:value={form.server} placeholder="rotate.aprs2.net" />
        </FormField>
        <FormField label="Port" id="ig-port">
          <Input id="ig-port" bind:value={form.port} type="number" placeholder="14580" />
        </FormField>
      </div>
      <div class="form-actions">
        <Button variant="primary" type="submit" disabled={loading}>
          {loading ? 'Saving...' : 'Save'}
        </Button>
      </div>
    </form>
  </Box>
</div>

<div class="tab-panel" class:hidden={activeTab !== 'filters'}>
  <p class="tab-doc">
    Two independent controls: the <strong>server filter</strong> tells the APRS-IS
    server which packets to send you, and the <strong>IS → RF transmit rules</strong>
    decide which of those packets get re-transmitted on RF. Every packet the server
    sends you appears on the live map regardless of the transmit rules.
  </p>
  <Box>
    <form onsubmit={handleSave}>
      <FormField label="APRS-IS Server Filter" id="ig-filter" hint="Sent to the APRS-IS server at login to control what it forwards to you (e.g. r/35.0/-106.0/100 for a 100 km radius). Everything the server sends — including packets rejected by the transmit rules below — is shown on the live map. If empty, no packets are received.">
        {#snippet children(describedBy)}
          <Input id="ig-filter" bind:value={form.server_filter} placeholder="r/35.0/-106.0/100" aria-describedby={describedBy} />
        {/snippet}
      </FormField>
      <p class="field-note">
        Enabled <a href="#/messages/tactical">tactical</a> callsigns are automatically
        appended as <code>g/</code> clauses — you don't need to add them here.
      </p>
      <FormField label="TX Channel" id="ig-txch" hint="Radio channel used to transmit IS→RF gated packets.">
        {#snippet children(describedBy)}
          <ChannelListbox
            id="ig-txch"
            bind:value={form.tx_channel}
            valueType="number"
            channels={channels}
            ariaLabelledBy={describedBy}
          />
        {/snippet}
      </FormField>
      {#if txUnbound && selectedTxChannel}
        <div class="unbound-warning" role="note">
          {unboundWarning(selectedTxChannel)}
        </div>
      {/if}
      <div class="form-actions">
        <Button variant="primary" type="submit" disabled={loading}>
          {loading ? 'Saving...' : 'Save'}
        </Button>
      </div>
    </form>
  </Box>
  <h3 class="section-heading">IS → RF Transmit Rules</h3>
  <p class="section-doc">
    First matching rule wins; if none match, the packet is not transmitted.
    These rules only affect RF transmission — they do not hide stations from the map.
  </p>
  <div class="rf-danger-panel" role="note">
    <div class="rf-danger-icon" aria-hidden="true">
      <Icon name="alert-circle" size="md" />
    </div>
    <div class="rf-danger-body">
      <strong>This panel transmits packets on the air.</strong>
      Broad patterns — short prefixes like <code>K</code> or <code>W5</code>, or
      broad wildcards like <code>B*</code> — can flood your local APRS frequency
      with gated traffic. Use the most specific rule you can, pair it with a
      tight server filter above, and test in simulation mode first.
    </div>
  </div>
  <div class="filters-header">
    <Button variant="primary" onclick={openCreate}>+ Add Rule</Button>
  </div>
  <DataTable
    {columns}
    rows={filters}
    onEdit={openEdit}
    onDelete={handleDelete}
    cells={{ pattern: patternCell }}
  />
</div>

{#snippet patternCell(value, _row)}
  {#if isWildcardPattern(value)}
    <span class="wildcard-pattern">
      <code>{value}</code>
      <Badge variant="warning">wildcard</Badge>
    </span>
  {:else}
    <code class="literal-pattern">{value ?? '—'}</code>
  {/if}
{/snippet}

<Modal bind:open={modalOpen} title={editing ? 'Edit Rule' : 'New Rule'}>
    <FormField label="Type" id="flt-type">
      {#snippet children(describedBy)}
        <Select id="flt-type" bind:value={filterForm.type} options={typeOptions} aria-describedby={describedBy} />
      {/snippet}
    </FormField>
    <FormField
      label="Pattern"
      id="flt-pattern"
      hint={hintFor(filterForm.type)}
      error={patternError}
    >
      {#snippet children(describedBy)}
        <Input
          id="flt-pattern"
          bind:value={filterForm.pattern}
          placeholder={placeholderFor(filterForm.type)}
          aria-describedby={describedBy}
          oninput={() => { patternTouched = true; }}
        />
      {/snippet}
    </FormField>
    <FormField label="Action" id="flt-action">
      {#snippet children(describedBy)}
        <Select id="flt-action" bind:value={filterForm.action} options={actionOptions} aria-describedby={describedBy} />
      {/snippet}
    </FormField>
    <FormField label="Priority" id="flt-priority">
      {#snippet children(describedBy)}
        <Input id="flt-priority" bind:value={filterForm.priority} type="number" placeholder="100" aria-describedby={describedBy} />
      {/snippet}
    </FormField>
    <Toggle bind:checked={filterForm.enabled} label="Enabled" />
    <div class="modal-actions">
      <Button onclick={() => modalOpen = false}>Cancel</Button>
      <Button
        variant="primary"
        onclick={handleFilterSave}
        disabled={!!patternError}
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

<ConfirmDialog
  bind:open={broadConfirmOpen}
  title="Broad rule — confirm RF transmit"
  message={broadConfirmMessage}
  confirmLabel="Save anyway"
  cancelLabel="Go back"
  confirmVariant="danger"
  onConfirm={persistFilter}
/>

<style>
  .tabs {
    display: flex;
    gap: 0;
    margin-bottom: 16px;
    border-bottom: 1px solid var(--border-color);
  }
  .tab {
    padding: 8px 20px;
    background: none;
    border: none;
    border-bottom: 2px solid transparent;
    color: var(--text-secondary);
    font-size: 13px;
    font-weight: 500;
    cursor: pointer;
    transition: color 0.15s, border-color 0.15s;
  }
  .tab:hover {
    color: var(--text-primary);
  }
  .tab.active {
    color: var(--accent);
    border-bottom-color: var(--accent);
  }
  .section-heading {
    font-size: 14px;
    font-weight: 600;
    color: var(--text-primary);
    margin: 20px 0 8px;
  }
  .section-doc {
    font-size: 13px;
    color: var(--text-secondary);
    line-height: 1.5;
    margin: 0 0 12px;
    max-width: 720px;
  }
  /* Section-level warning that the rules below transmit on RF. Matches
     the amber look used in Digipeater.svelte's no-rules-warning so the
     app has one consistent "danger callout" pattern. */
  .rf-danger-panel {
    display: flex;
    align-items: flex-start;
    gap: 10px;
    margin: 0 0 16px;
    padding: 12px 14px;
    border: 1px solid var(--color-warning, #d4a72c);
    border-left-width: 4px;
    border-radius: 4px;
    background: var(--color-warning-bg, rgba(212, 167, 44, 0.12));
    color: var(--text-primary, inherit);
    line-height: 1.45;
    max-width: 720px;
  }
  .rf-danger-icon {
    color: var(--color-warning, #d4a72c);
    flex: 0 0 auto;
    display: flex;
    align-items: center;
    line-height: 1;
    padding-top: 1px;
  }
  .rf-danger-body {
    font-size: 13px;
  }
  .rf-danger-body code {
    font-size: 12px;
    padding: 1px 4px;
    background: rgba(0, 0, 0, 0.08);
    border-radius: 3px;
  }
  .filters-header {
    display: flex;
    justify-content: flex-end;
    margin-bottom: 12px;
  }
  .tab-panel.hidden { display: none; }
  .tab-doc {
    font-size: 13px;
    color: var(--text-secondary);
    line-height: 1.5;
    margin: 0 0 16px;
    max-width: 720px;
  }
  .form-actions { display: flex; justify-content: flex-end; margin-top: 16px; }
  .modal-actions { display: flex; gap: 8px; justify-content: flex-end; margin-top: 16px; }

  /* Wildcard vs. literal rendering in the rule DataTable.
     - Wildcard patterns get an accent-toned monospace value plus a
       `wildcard` badge so the user can scan their rule list for the
       high-impact rules at a glance.
     - Literal patterns get plain monospace so both render in the same
       type metrics and users can visually compare prefixes. */
  .wildcard-pattern {
    display: inline-flex;
    align-items: center;
    gap: 8px;
  }
  .wildcard-pattern code {
    font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
    font-size: 12px;
    color: var(--color-warning, #d4a72c);
    font-style: italic;
  }
  .literal-pattern {
    font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
    font-size: 12px;
    color: var(--text-primary);
  }

  /* Supplemental note rendered beneath a FormField when the hint itself
     can't carry markup (e.g. inline links). Visually matches the muted
     `.field-hint` style inside FormField.svelte so the two read as one
     block to the user. Sits flush under the field (no extra top margin)
     and keeps the 12px muted look. */
  .field-note {
    margin: -8px 0 12px;
    font-size: 12px;
    color: var(--color-text-muted, #888);
    line-height: 1.4;
  }
  .field-note a {
    color: var(--accent, #3b82f6);
    text-decoration: none;
  }
  .field-note a:hover,
  .field-note a:focus-visible {
    text-decoration: underline;
  }
  .field-note code {
    font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
    font-size: 11px;
    padding: 1px 4px;
    background: rgba(0, 0, 0, 0.08);
    border-radius: 3px;
  }

  /* D17 — unbound-channel save warning on the TX channel picker. */
  .unbound-warning {
    margin: 12px 0;
    padding: 10px 12px;
    border: 1px solid var(--color-warning, #d4a72c);
    border-left-width: 4px;
    border-radius: 4px;
    background: var(--color-warning-bg, rgba(212, 167, 44, 0.12));
    color: var(--text-primary);
    font-size: 13px;
    line-height: 1.45;
  }

  /* Station-callsign discoverability row (Phase 4B). Read-only echo of
     the current station callsign with a link to the page that owns it.
     Sized to sit comfortably above the Enable toggle without dominating
     the form. */
  .station-row {
    display: flex;
    align-items: baseline;
    gap: 8px;
    margin: 0 0 14px;
    font-size: 13px;
    color: var(--text-primary);
  }
  .station-row-label {
    color: var(--color-text-muted, var(--text-secondary, #888));
  }
  .station-row-value {
    font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
    font-weight: 600;
  }
  .station-row-value.is-empty {
    font-family: inherit;
    font-weight: normal;
    font-style: italic;
    color: var(--color-text-muted, var(--text-secondary, #888));
  }
  .station-row-link {
    color: var(--accent, #3b82f6);
    text-decoration: none;
  }
  .station-row-link:hover,
  .station-row-link:focus-visible {
    text-decoration: underline;
  }

  /* When the station callsign is missing, the Enable toggle is
     aria-disabled but NOT hard-disabled (plan D7 — hard `disabled`
     removes the control from the focus order in some browsers and
     screen readers can skip announcing it). Mirror that state visually
     so sighted users understand the control is inert. Bits-ui sets the
     aria attribute directly on the <button role="switch">; we target
     it with :global so Svelte's scoped CSS reaches past the Chonky
     wrapper. */
  :global([role='switch'][aria-disabled='true']) {
    opacity: 0.55;
    cursor: not-allowed;
  }
</style>
