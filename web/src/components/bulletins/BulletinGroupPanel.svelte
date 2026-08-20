<script>
  // Right pane of the Bulletins shell. Shows the selected group's
  // settings and 10-slot bulletin table.
  //
  // Props:
  //   group       — full BulletinGroupResponse with items[] (or null)
  //   onSaveGroup — called with updated group settings object
  //   onItemSave  — called with (slot, { text, active })
  //   onSendNow   — called with slot number
  //   onClearItem — called with slot number

  import { Button, Input, Toggle, Box, Table } from '@chrissnell/chonky-ui';
  import FormField from '../FormField.svelte';

  let { group = null, onSaveGroup, onItemSave, onSendNow, onClearItem } = $props();

  // ---------------------------------------------------------------------------
  // Group settings form
  // ---------------------------------------------------------------------------

  let settingsForm = $state({
    digi_path:    '',
    initial_rate: 60,
    decay_factor: 1.5,
    stable_rate:  600,
  });

  let settingsError = $state('');
  let settingsSaving = $state(false);

  $effect(() => {
    if (!group) return;
    settingsForm = {
      digi_path:    group.digi_path    ?? '',
      initial_rate: group.initial_rate ?? 60,
      decay_factor: group.decay_factor ?? 1.5,
      stable_rate:  group.stable_rate  ?? 600,
    };
    settingsError = '';
  });

  function validateSettings() {
    if (settingsForm.initial_rate < 30) return 'Initial repeat rate must be at least 30 seconds.';
    if (settingsForm.decay_factor < 1.0) return 'Decay factor must be ≥ 1.0.';
    if (settingsForm.stable_rate < settingsForm.initial_rate) {
      return 'Stable rate must be ≥ initial rate.';
    }
    return '';
  }

  async function handleSaveSettings() {
    const err = validateSettings();
    if (err) { settingsError = err; return; }
    settingsError = '';
    settingsSaving = true;
    try {
      await onSaveGroup?.({
        name:         group.id === 1 ? '' : (settingsForm.name ?? group.name ?? ''),
        send_path:    group.send_path ?? 'rf',
        digi_path:    settingsForm.digi_path,
        initial_rate: Number(settingsForm.initial_rate),
        decay_factor: Number(settingsForm.decay_factor),
        stable_rate:  Number(settingsForm.stable_rate),
        active:       group.active,
      });
    } finally {
      settingsSaving = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Group active toggle
  // ---------------------------------------------------------------------------

  const hasActiveItem = $derived.by(() => {
    if (!group?.items) return false;
    return group.items.some((it) => it.active);
  });

  async function handleGroupActiveToggle(next) {
    if (next && !hasActiveItem) return; // guard: no active items
    await onSaveGroup?.({
      name:         group.id === 1 ? '' : (group.name ?? ''),
      send_path:    group.send_path    ?? 'rf',
      digi_path:    group.digi_path    ?? '',
      initial_rate: group.initial_rate ?? 60,
      decay_factor: group.decay_factor ?? 1.5,
      stable_rate:  group.stable_rate  ?? 600,
      active:       next,
    });
  }

  // ---------------------------------------------------------------------------
  // Per-item editing (local state keyed by slot)
  // ---------------------------------------------------------------------------

  // Mirror group.items into a local editable array so toggling or typing
  // doesn't require a round-trip before showing optimistic state.
  /** @type {Array<{slot:number, text:string, active:boolean, send_count:number}>} */
  let localItems = $state([]);

  $effect(() => {
    if (!group?.items) { localItems = []; return; }
    // Build a slot-indexed array of 10 items.
    const map = new Map(group.items.map((it) => [it.slot, it]));
    localItems = Array.from({ length: 10 }, (_, i) => {
      const src = map.get(i);
      return { slot: i, text: src?.text ?? '', active: src?.active ?? false, send_count: src?.send_count ?? 0 };
    });
  });

  async function handleItemActiveToggle(slot, next) {
    const item = localItems[slot];
    if (!item) return;
    // Optimistic: update locally first.
    localItems[slot] = { ...item, active: next };
    await onItemSave?.(slot, { text: item.text, active: next });
    // If all items are now inactive, also deactivate the group.
    const anyActive = localItems.some((it) => it.active);
    if (!anyActive && group.active) {
      await handleGroupActiveToggle(false);
    }
  }

  async function handleItemTextChange(slot) {
    const item = localItems[slot];
    if (!item) return;
    await onItemSave?.(slot, { text: item.text, active: item.active });
  }

  function displayName(g) {
    if (!g) return '';
    return g.name === '' ? 'Global' : g.name;
  }
</script>

{#if !group}
  <div class="empty-shell">
    <div class="empty-inner">
      <span class="empty-icon" aria-hidden="true">📻</span>
      <h3>Select a bulletin group</h3>
      <p>Choose a group from the list or create a new one.</p>
    </div>
  </div>
{:else}
  <div class="panel">
    <!-- ── Group header ── -->
    <div class="panel-header">
      <h2 class="group-title">{displayName(group)}</h2>
      <div class="header-right">
        <span class="active-label">Active</span>
        <Toggle
          checked={group.active}
          disabled={!hasActiveItem && !group.active}
          onCheckedChange={handleGroupActiveToggle}
          aria-label="Toggle group active"
          title={!hasActiveItem && !group.active ? 'Enable at least one bulletin item first' : ''}
        />
      </div>
    </div>

    <!-- ── Group Settings ── -->
    <Box title="Group Settings">
      <div class="settings-grid">
        <!-- Digi Path -->
        <FormField
          label="Digipeat Path"
          id="bltn-digi-path"
          hint="Leave blank for direct RF only (no digipeating). Example: WIDE1-1,WIDE2-1"
        >
          <Input id="bltn-digi-path" bind:value={settingsForm.digi_path} placeholder="(no digi)" />
        </FormField>

        <div class="rate-row">
          <!-- Initial Rate -->
          <FormField label="Initial Rate (sec)" id="bltn-init-rate" hint="Min 30">
            <Input id="bltn-init-rate" type="number" min="30" bind:value={settingsForm.initial_rate} />
          </FormField>

          <!-- Decay Factor -->
          <FormField label="Decay Factor" id="bltn-decay" hint="≥ 1.0">
            <Input id="bltn-decay" type="number" min="1" step="0.1" bind:value={settingsForm.decay_factor} />
          </FormField>

          <!-- Stable Rate -->
          <FormField label="Stable Rate (sec)" id="bltn-stable-rate">
            <Input id="bltn-stable-rate" type="number" min="30" bind:value={settingsForm.stable_rate} />
          </FormField>
        </div>

        {#if settingsError}
          <p class="settings-error" role="alert">{settingsError}</p>
        {/if}

        <div class="settings-actions">
          <Button variant="primary" onclick={handleSaveSettings} disabled={settingsSaving}>
            {settingsSaving ? 'Saving…' : 'Save Settings'}
          </Button>
        </div>
      </div>
    </Box>

    <!-- ── Bulletin Slots Table ── -->
    <Box title="Bulletins">
      <div class="table-wrap">
        <Table>
          <thead>
            <tr>
              <th class="col-active">Active</th>
              <th class="col-slot">Slot</th>
              <th class="col-text">Contents</th>
              <th class="col-count">Sent</th>
              <th class="col-actions">Actions</th>
            </tr>
          </thead>
          <tbody>
            {#each localItems as item (item.slot)}
              <tr class:row-active={item.active}>
                <td class="col-active">
                  <Toggle
                    checked={item.active}
                    onCheckedChange={(v) => handleItemActiveToggle(item.slot, v)}
                    aria-label={`Slot ${item.slot} active`}
                  />
                </td>
                <td class="col-slot">
                  <span class="slot-badge">BLN{item.slot}{group.name ? group.name : ''}</span>
                </td>
                <td class="col-text">
                  <Input
                    type="text"
                    bind:value={item.text}
                    placeholder="Bulletin text (max 67 chars)…"
                    maxlength="67"
                    onchange={() => handleItemTextChange(item.slot)}
                    aria-label={`Slot ${item.slot} text`}
                  />
                </td>
                <td class="col-count">{item.send_count}</td>
                <td class="col-actions">
                  <Button
                    variant="primary"
                    size="sm"
                    disabled={!item.text}
                    onclick={() => onSendNow?.(item.slot)}
                    title="Send this bulletin now"
                  >Send</Button>
                  <Button
                    variant="ghost"
                    size="sm"
                    onclick={() => onClearItem?.(item.slot)}
                    title="Clear this slot"
                  >Clear</Button>
                </td>
              </tr>
            {/each}
          </tbody>
        </Table>
      </div>
    </Box>
  </div>
{/if}

<style>
  .empty-shell {
    display: flex;
    align-items: center;
    justify-content: center;
    height: 100%;
    color: var(--color-text-muted);
  }
  .empty-inner {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 8px;
    text-align: center;
  }
  .empty-icon { font-size: 48px; }
  .empty-inner h3 { margin: 8px 0 0; font-size: 16px; font-weight: 600; }
  .empty-inner p  { margin: 0; font-size: 13px; }

  .panel {
    display: flex;
    flex-direction: column;
    gap: 16px;
    padding: 16px 20px;
    overflow-y: auto;
    height: 100%;
  }

  .panel-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
  }
  .group-title {
    margin: 0;
    font-size: 18px;
    font-weight: 700;
    font-family: var(--font-mono);
    letter-spacing: 0.5px;
  }
  .header-right {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .active-label {
    font-size: 13px;
    color: var(--color-text-muted);
    font-weight: 500;
  }

  .settings-grid {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }
  .rate-row {
    display: grid;
    grid-template-columns: 1fr 1fr 1fr;
    gap: 12px;
  }
  @media (max-width: 640px) {
    .rate-row { grid-template-columns: 1fr; }
  }
  .settings-error {
    margin: 0;
    color: var(--color-danger);
    font-size: 12px;
  }
  .settings-actions { display: flex; justify-content: flex-end; }
  /* Flatten Chonky input margin in table cells */
  .col-text :global(input) { margin: 0 !important; }

  .table-wrap { overflow-x: auto; }

  .col-active  { width: 60px;  text-align: center; }
  .col-slot    { width: 120px; font-family: var(--font-mono); font-size: 12px; }
  .col-count   { width: 60px;  text-align: right;  font-variant-numeric: tabular-nums; }
  .col-actions { width: 130px; text-align: right;  white-space: nowrap; }
  .col-actions :global(button) { margin-left: 4px; }

  .row-active { background: var(--color-success-muted, rgba(46, 160, 67, 0.07)); }

  .slot-badge {
    font-family: var(--font-mono);
    font-size: 11px;
    font-weight: 600;
    color: var(--color-text-muted);
    letter-spacing: 0.3px;
  }
</style>
