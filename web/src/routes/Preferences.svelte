<script>
  import { onMount } from 'svelte';
  import { Toggle, Box, Select } from '@chrissnell/chonky-ui';
  import { unitsState } from '../lib/settings/units-store.svelte.js';
  import { updates } from '../lib/updatesStore.svelte.js';
  import { messagesPreferencesState } from '../lib/settings/messages-preferences-store.svelte.js';
  import { themeState } from '../lib/settings/theme-store.svelte.js';
  import { THEMES } from '../lib/themes/registry.js';
  import PageHeader from '../components/PageHeader.svelte';

  const themeOptions = THEMES.map((t) => ({ value: t.id, label: t.name }));

  onMount(() => {
    updates.fetchConfig();
    unitsState.fetchConfig();
    messagesPreferencesState.fetchPreferences();
    themeState.fetchConfig();
  });

  let themeDescription = $derived(
    THEMES.find((t) => t.id === themeState.theme)?.description ?? '',
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

<Box title="Messages">
  <Toggle
    checked={messagesPreferencesState.allowLong}
    onCheckedChange={(v) => messagesPreferencesState.setAllowLong(v)}
    label="Allow long APRS messages"
    disabled={!messagesPreferencesState.loaded || messagesPreferencesState.saving}
  />
  <p class="messages-hint">
    Lets you send messages up to 200 characters. Some receivers cannot
    decode longer messages and will truncate or drop them. Leave off
    unless you know your contacts support it.
  </p>
</Box>

<style>
  .theme-hint,
  .unit-hint,
  .update-hint,
  .messages-hint {
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
</style>
