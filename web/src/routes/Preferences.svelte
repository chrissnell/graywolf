<script>
  import { onMount } from 'svelte';
  import { Toggle, Box, Select, Button } from '@chrissnell/chonky-ui';
  import { unitsState } from '../lib/settings/units-store.svelte.js';
  import { updates } from '../lib/updatesStore.svelte.js';
  import { themeState } from '../lib/settings/theme-store.svelte.js';
  import { uiScaleState, UI_SCALE_OPTIONS } from '../lib/settings/ui-scale-store.svelte.js';
  import { compactMenuState } from '../lib/settings/compact-menu-store.svelte.js';
  import { storageState } from '../lib/settings/storage-store.svelte.js';
  import { THEMES } from '../lib/themes/registry.js';
  import { Platform } from '../lib/platform.js';
  import PageHeader from '../components/PageHeader.svelte';
  import Modal, { Header, Body } from '../components/Modal.svelte';

  const themeOptions = THEMES.map((t) => ({ value: t.id, label: t.name }));

  // Storage migration state machine: idle → confirming → migrating → idle
  let migrationDialog = $state('idle'); // 'idle' | 'confirming' | 'migrating'
  let confirmOpen = $state(false);
  let pendingUseSDCard = $state(false);
  // Incremented on cancel to force a Toggle remount with the correct server value.
  let toggleKey = $state(0);
  let migrationProgress = $state({ state: 'idle', progress: 0, message: '', bytes_done: 0, total_bytes: 0 });
  let migrationPollInterval = $state(null);

  function confirmMessage(useSDCard) {
    const dest = useSDCard
      ? (storageState.sdCardPath || 'SD card')
      : (storageState.internalPath || 'internal storage');
    return `This will move your maps, history, and configuration databases to:\n\n${dest}\n\nThe app will restart briefly while files are being moved. This may take a few minutes if you have large map downloads.`;
  }

  function handleStorageToggle(newValue) {
    pendingUseSDCard = newValue;
    migrationDialog = 'confirming';
    confirmOpen = true;
  }

  function cancelMigration() {
    migrationDialog = 'idle';
    confirmOpen = false;
    toggleKey++; // force Toggle remount so it resets to the server's current value
  }

  function startMigration() {
    confirmOpen = false;
    migrationDialog = 'migrating';
    migrationProgress = { state: 'starting', progress: 0, message: 'Preparing...', bytes_done: 0, total_bytes: 0 };
    window.GraywolfWebInterface?.initiateStorageMigration(pendingUseSDCard);
    migrationPollInterval = setInterval(pollMigrationState, 500);
  }

  function pollMigrationState() {
    const raw = window.GraywolfWebInterface?.getStorageMigrationState?.();
    if (!raw) return;
    try {
      migrationProgress = JSON.parse(raw);
    } catch { return; }
    if (migrationProgress.state === 'complete' || migrationProgress.state === 'error') {
      clearInterval(migrationPollInterval);
      migrationPollInterval = null;
      // On complete the WebView reloads; on error we let the user retry.
      if (migrationProgress.state === 'error') {
        migrationDialog = 'migrating'; // keep dialog open showing error
      }
      // On success Kotlin reloads the WebView — dialog will disappear naturally.
    }
  }

  function retryMigration() {
    startMigration();
  }

  onMount(() => {
    updates.fetchConfig();
    unitsState.fetchConfig();
    themeState.fetchConfig();
    if (Platform.isAndroid) storageState.fetchConfig();
    return () => {
      if (migrationPollInterval) clearInterval(migrationPollInterval);
    };
  });

  let themeDescription = $derived(
    THEMES.find((t) => t.id === themeState.theme)?.description ?? '',
  );

  let migrationProgressPct = $derived(
    migrationProgress.total_bytes > 0
      ? migrationProgress.bytes_done / migrationProgress.total_bytes
      : migrationProgress.progress ?? 0,
  );
</script>

<PageHeader title="Preferences" subtitle="Display and formatting options" />

<Box title="Theme">
  <Select
    value={themeState.theme}
    onValueChange={(v) => themeState.setTheme(v)}
    options={themeOptions}
  />
  <p class="theme-hint">{themeDescription}</p>
  <p class="theme-contrib-hint">
    Want your own theme? See
    <code>graywolf/web/themes/README.md</code>
    for how to add one in a pull request.
  </p>
</Box>

<Box title="Display size">
  <Select
    value={String(uiScaleState.scale)}
    onValueChange={(v) => uiScaleState.setScale(v)}
    options={UI_SCALE_OPTIONS}
  />
  <p class="scale-hint">
    Scales the whole interface — text, buttons, and switches. Saved on this
    device only, so it won't change the size on your other screens.
  </p>
</Box>

<Box title="Units">
  <Toggle
    checked={unitsState.isMetric}
    onCheckedChange={(v) => unitsState.setSystem(v ? 'metric' : 'imperial')}
    label="Use metric units"
  />
  <p class="unit-hint">
    {#if unitsState.isMetric}
      Altitude in meters, distance in m/km, speed in km/h.
    {:else}
      Altitude in feet, distance in ft/mi, speed in mph.
    {/if}
  </p>
</Box>

<Box title="Updates">
  <Toggle
    checked={updates.enabled}
    onCheckedChange={(v) => updates.setEnabled(v)}
    label="Check for updates from GitHub"
  />
  <p class="update-hint">
    Contacts github.com once a day. Turn off for offline stations
    or to avoid sharing your IP.
  </p>
</Box>

<Box title="Menu">
  <Toggle
    checked={compactMenuState.forceCompact}
    onCheckedChange={(v) => compactMenuState.setForceCompact(v)}
    label="Force small side menu"
  />
  <p class="menu-hint">
    This will force the small screen side menu to be active.
  </p>
</Box>

{#if Platform.isAndroid}
  <Box title="Storage">
    {#key toggleKey}
      <Toggle
        checked={storageState.useSDCard}
        onCheckedChange={handleStorageToggle}
        label="Store data on SD card"
        disabled={!storageState.sdCardAvailable && !storageState.useSDCard}
      />
    {/key}
    <p class="storage-hint">
      Moves your maps, history, and configuration databases from internal storage
      to the SD card. The app will restart briefly during the move.
      {#if !storageState.sdCardAvailable && !storageState.useSDCard}
        <br /><strong>No removable SD card detected.</strong>
      {/if}
    </p>
    {#if storageState.activePath}
      <p class="storage-location-hint">
        Data location: <code>{storageState.activePath}</code>
      </p>
    {/if}
  </Box>
{/if}

<!-- Confirmation dialog: fires before any migration begins -->
{#if confirmOpen}
  <Modal bind:open={confirmOpen} title={pendingUseSDCard ? 'Move data to SD card?' : 'Move data to internal storage?'} onClose={cancelMigration}>
    <p class="confirm-message">{confirmMessage(pendingUseSDCard)}</p>
    <div class="modal-actions">
      <Button onclick={cancelMigration}>Cancel</Button>
      <Button variant="primary" onclick={startMigration}>Move files</Button>
    </div>
  </Modal>
{/if}

<!-- Migration progress modal: non-dismissible while migration runs -->
{#if migrationDialog === 'migrating'}
  <Modal open={true} title={migrationProgress.state === 'error' ? 'Migration failed' : 'Moving files…'}>
    <div class="migration-body">
      {#if migrationProgress.state === 'error'}
        <p class="confirm-message migration-error">{migrationProgress.message || 'An error occurred.'}</p>
        <div class="modal-actions">
          <Button onclick={() => { migrationDialog = 'idle'; }}>Close</Button>
          <Button variant="primary" onclick={retryMigration}>Retry</Button>
        </div>
      {:else if migrationProgress.state === 'complete'}
        <p class="confirm-message">Done — restarting app…</p>
      {:else}
        {#if migrationProgress.message}
          <p class="confirm-message migration-file">{migrationProgress.message}</p>
        {/if}
        <progress
          class="migration-progress"
          value={migrationProgressPct}
          max={1}
        ></progress>
        <p class="migration-pct">{Math.round(migrationProgressPct * 100)}%</p>
      {/if}
    </div>
  </Modal>
{/if}

<style>
  .theme-hint,
  .scale-hint,
  .unit-hint,
  .update-hint,
  .menu-hint,
  .storage-hint {
    margin-top: 12px;
    font-size: 13px;
    color: var(--text-muted);
  }
  .theme-contrib-hint {
    margin-top: 6px;
    font-size: 12px;
    color: var(--text-muted);
    opacity: 0.75;
  }
  .theme-contrib-hint code {
    font-family: var(--font-mono);
    font-size: 11px;
  }
  .storage-location-hint {
    margin-top: 8px;
    font-size: 13px;
    color: var(--text-muted);
  }
  .storage-location-hint code {
    font-family: var(--font-mono);
    font-size: 11px;
    padding: 1px 5px;
    background: var(--bg-secondary);
    border-radius: 3px;
    word-break: break-all;
  }
  /* Migration modal contents reuse ConfirmDialog's class names for consistency */
  .migration-body {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }
  .migration-file {
    font-family: var(--font-mono);
    font-size: 11px;
    word-break: break-all;
  }
  .migration-error {
    color: var(--color-danger, #e05252);
  }
  .migration-progress {
    width: 100%;
    height: 6px;
    border-radius: 3px;
    appearance: none;
    border: none;
    background: var(--border-color);
  }
  .migration-progress::-webkit-progress-bar {
    background: var(--border-color);
    border-radius: 3px;
  }
  .migration-progress::-webkit-progress-value {
    background: var(--accent);
    border-radius: 3px;
  }
  .migration-progress::-moz-progress-bar {
    background: var(--accent);
    border-radius: 3px;
  }
  .migration-pct {
    font-size: 12px;
    color: var(--text-muted);
    text-align: right;
    margin: 0;
  }
  /* Reuse ConfirmDialog layout for the error/actions row */
  .confirm-message {
    font-size: 13px;
    color: var(--text-primary);
    line-height: 1.5;
    margin: 0;
    white-space: pre-wrap;
  }
  .modal-actions {
    display: flex;
    gap: 8px;
    justify-content: flex-end;
  }
</style>
