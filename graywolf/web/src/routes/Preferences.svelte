<script>
  import { onMount } from 'svelte';
  import { Toggle, Box } from '@chrissnell/chonky-ui';
  import { unitsState } from '../lib/settings/units-store.svelte.js';
  import { updates } from '../lib/updatesStore.svelte.js';
  import { messagesPreferencesState } from '../lib/settings/messages-preferences-store.svelte.js';
  import PageHeader from '../components/PageHeader.svelte';

  onMount(() => {
    updates.fetchConfig();
    unitsState.fetchConfig();
    messagesPreferencesState.fetchPreferences();
  });

  // Per Phase 2 plan: native <input type="checkbox" role="switch">, NOT
  // a custom toggle div. bind:checked tracks the store projection; on
  // change we fire setAllowLong which performs the PUT. Optimistic
  // update + rollback happens inside the store; the checkbox reflects
  // whatever the store currently holds.
  function onAllowLongChange(e) {
    const next = /** @type {HTMLInputElement} */ (e.currentTarget).checked;
    messagesPreferencesState.setAllowLong(next);
  }
</script>

<PageHeader title="Preferences" subtitle="Display and formatting options" />

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
  <!--
    Native <input type="checkbox" role="switch"> per the plan. The
    <label> wraps the control so the click target includes the label
    text; aria-describedby points at the help paragraph below so screen
    readers announce the help copy when the switch is focused.
  -->
  <label class="switch-row">
    <input
      type="checkbox"
      role="switch"
      class="native-switch"
      aria-describedby="allow-long-help"
      checked={messagesPreferencesState.allowLong}
      disabled={!messagesPreferencesState.loaded || messagesPreferencesState.saving}
      onchange={onAllowLongChange}
    />
    <span class="switch-label-text">
      Allow long APRS messages
      <span class="advanced-badge" aria-label="Advanced setting">advanced</span>
    </span>
  </label>
  <p id="allow-long-help" class="messages-hint">
    Lets you send messages up to 200 characters. Some receivers cannot
    decode longer messages and will truncate or drop them. Leave off
    unless you know your contacts support it.
  </p>
</Box>

<style>
  .unit-hint,
  .update-hint,
  .messages-hint {
    margin-top: 12px;
    font-size: 13px;
    color: var(--text-muted);
  }

  .switch-row {
    display: inline-flex;
    align-items: center;
    gap: 10px;
    cursor: pointer;
    user-select: none;
  }

  /*
   * Native checkbox styled as a switch. We keep the input focusable and
   * keyboard-operable (space flips it — that's browser default for
   * checkbox), hide the default square, and render a pill + thumb via
   * CSS background and ::before. role="switch" on the checkbox makes
   * assistive tech announce it as a switch with the correct
   * aria-checked state derived from the `checked` property.
   */
  .native-switch {
    appearance: none;
    -webkit-appearance: none;
    width: 36px;
    height: 20px;
    margin: 0;
    padding: 0;
    border-radius: 10px;
    background: var(--surface-2, #3a3a3a);
    border: 1px solid var(--border, #555);
    position: relative;
    cursor: pointer;
    transition: background 120ms ease, border-color 120ms ease;
    flex-shrink: 0;
  }
  .native-switch::before {
    content: '';
    position: absolute;
    top: 2px;
    left: 2px;
    width: 14px;
    height: 14px;
    border-radius: 50%;
    background: var(--text, #e8e8e8);
    transition: transform 120ms ease;
  }
  .native-switch:checked {
    background: var(--accent, #2e7dd7);
    border-color: var(--accent, #2e7dd7);
  }
  .native-switch:checked::before {
    transform: translateX(16px);
  }
  .native-switch:focus-visible {
    outline: 2px solid var(--accent, #2e7dd7);
    outline-offset: 2px;
  }
  .native-switch:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .switch-label-text {
    font-size: 14px;
    color: var(--text, inherit);
    display: inline-flex;
    align-items: center;
    gap: 8px;
  }

  .advanced-badge {
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    padding: 2px 6px;
    border-radius: 4px;
    background: var(--warning-bg, rgba(255, 176, 32, 0.15));
    color: var(--warning, #e0a020);
    border: 1px solid var(--warning, rgba(255, 176, 32, 0.35));
    font-weight: 600;
  }
</style>
