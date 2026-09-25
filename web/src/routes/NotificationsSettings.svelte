<script>
  import { onMount } from 'svelte';
  import { Toggle, Box, Select, Button, Input, Icon } from '@chrissnell/chonky-ui';
  import PageHeader from '../components/PageHeader.svelte';
  import {
    notificationPrefsState,
    STATION_NEW_THRESHOLDS,
    isPresetThresholdSecs,
    secsToCustomInput,
    customInputToSecs,
  } from '../lib/settings/notification-prefs-store.svelte.js';
  import { notificationSoundState } from '../lib/settings/notification-sound-store.svelte.js';
  import { notifications } from '../lib/notificationsStore.svelte.js';
  import { fireOsNotification } from '../lib/osNotify.js';
  import { SOUND_PRESETS } from '../lib/soundPresets.js';
  import { toasts } from '../lib/stores.js';
  import { api } from '../lib/api.js';
  import { favoriteStationsStore } from '../lib/favoriteStationsStore.svelte.js';
  import { excludedStationsStore } from '../lib/excludedStationsStore.svelte.js';

  // Used so "Send test emergency notification" can deep-link to a real
  // station's map popup (see below) rather than a fake callsign that
  // wouldn't resolve to anything -- lets the operator verify the full
  // click-through (map focus + popup + Message action) without a real
  // Emergency packet going out over RF. lat/lon are required alongside
  // the callsign: LiveMapV2.svelte's parseFocusFromHash() discards the
  // whole deep-link (even the callsign) if either is missing, since
  // it also drives the initial camera fly-to before the station is
  // loaded from a poll. Best-effort: an unset callsign or unavailable
  // position just falls back to a plain #/map link.
  let stationCallsign = $state('');
  let myLat = $state(null);
  let myLon = $state(null);
  onMount(async () => {
    try {
      const st = await api.get('/station/config');
      stationCallsign = (st && st.callsign) || '';
    } catch {
      stationCallsign = '';
    }
    try {
      const pos = await api.get('/position');
      if (pos && pos.valid) {
        myLat = pos.lat;
        myLon = pos.lon;
      }
    } catch {
      myLat = null;
      myLon = null;
    }
    favoriteStationsStore.load();
    excludedStationsStore.load();
  });

  // "Custom…" lets the operator type an exact hours/weeks value instead of
  // picking one of the fixed presets. thresholdMode is local UI state (not
  // derived from the store) so picking "Custom…" visibly switches the
  // Select and reveals the count/unit inputs even before a value has been
  // applied -- deriving it from the persisted threshold would otherwise
  // snap back to the last real preset the instant "Custom…" is clicked.
  const CUSTOM_THRESHOLD = 'custom';
  const stationNewThresholdOptions = [
    ...STATION_NEW_THRESHOLDS.map((o) => ({ value: o.value, label: o.label })),
    { value: CUSTOM_THRESHOLD, label: 'Custom…' },
  ];
  const customUnitOptions = [
    { value: 'hours', label: 'hours' },
    { value: 'weeks', label: 'weeks' },
  ];

  function initialThresholdMode() {
    const secs = notificationPrefsState.stationNewThresholdSecs;
    return isPresetThresholdSecs(secs) ? secs : CUSTOM_THRESHOLD;
  }
  let thresholdMode = $state(initialThresholdMode());
  const initialCustom = secsToCustomInput(notificationPrefsState.stationNewThresholdSecs);
  let customCount = $state(initialCustom.count);
  let customUnit = $state(initialCustom.unit);

  function handleThresholdSelect(v) {
    if (v === CUSTOM_THRESHOLD) {
      // Re-seed from whatever's currently persisted so switching to
      // Custom on an already-custom value shows the real number rather
      // than resetting it.
      const seeded = secsToCustomInput(notificationPrefsState.stationNewThresholdSecs);
      customCount = seeded.count;
      customUnit = seeded.unit;
      thresholdMode = CUSTOM_THRESHOLD;
      return;
    }
    thresholdMode = Number(v);
    notificationPrefsState.setStationNewThresholdSecs(Number(v));
  }

  function applyCustomThreshold() {
    notificationPrefsState.setStationNewThresholdSecs(customInputToSecs(customCount, customUnit));
  }

  const notificationModeOptions = $derived([
    { value: 'toast', label: 'In-app popup' },
    ...(notificationPrefsState.supported
      ? [
          { value: 'os', label: 'OS notification' },
          { value: 'both', label: 'Both' },
        ]
      : []),
  ]);

  // Plain operator-facing preview — not a dev-only debug affordance.
  // Fires through whichever mode(s) are currently selected, plus
  // whichever sound(s) are currently enabled, so the operator hears and
  // sees the exact combination they'd get for a real message or
  // bulletin before relying on it. Message and bulletin sounds are
  // staggered slightly so both are audible rather than overlapping.
  function sendTestNotification() {
    const title = 'Test notification';
    const body = 'This is what a new message or bulletin notification looks like.';
    if (notificationPrefsState.toastEnabled) {
      notifications.push({ kind: 'test', title, body, href: '' });
    }
    if (notificationPrefsState.osEnabled) {
      fireOsNotification(title, body, null, { force: true });
    }
    if (notificationPrefsState.messageEnabled) notificationSoundState.message.play();
    if (notificationPrefsState.bulletinEnabled) {
      setTimeout(() => notificationSoundState.bulletin.play(), 350);
    }
  }

  // Deep-links to the operator's own station on the map (real Emergency
  // alerts do the same: #/map?focus=<callsign>, see
  // stationAlertsTransport.js) rather than a fake callsign, so clicking
  // this test notification exercises the exact same click-through --
  // camera fly-to, popup open, and the popup's real Message/Logs/QRZ
  // action row -- without a real Emergency packet going out over RF.
  // Falls back to a plain map link if the station callsign isn't set.
  function sendTestEmergencyNotification() {
    const title = `EMERGENCY: ${stationCallsign || 'TEST-1'}`;
    const body = 'This is what a station emergency notification looks like. Clicking it takes you to this station on the map.';
    const hasFocusTarget = stationCallsign && Number.isFinite(myLat) && Number.isFinite(myLon);
    const href = hasFocusTarget
      ? `#/map?focus=${encodeURIComponent(stationCallsign)}&lat=${myLat}&lon=${myLon}`
      : '#/map';
    if (notificationPrefsState.toastEnabled) {
      notifications.push({ kind: 'station-emergency', title, body, href });
    }
    if (notificationPrefsState.osEnabled) {
      fireOsNotification(title, body, () => {
        window.location.hash = href;
      }, { force: true });
    }
    if (notificationPrefsState.stationEmergencyEnabled) notificationSoundState.stationEmergency.play();
  }

  // Same deep-link shape as the real thing (stationNewTransport.js's
  // `#/map?focus=<callsign>&lat=…&lon=…`), reusing the operator's own
  // station/position the same way the emergency test does.
  function sendTestNewStationNotification() {
    const callsign = stationCallsign || 'TEST-9';
    const title = `New station: ${callsign}`;
    const body = `${callsign} heard on RF`;
    const hasFocusTarget = stationCallsign && Number.isFinite(myLat) && Number.isFinite(myLon);
    const href = hasFocusTarget
      ? `#/map?focus=${encodeURIComponent(stationCallsign)}&lat=${myLat}&lon=${myLon}`
      : '#/map';
    if (notificationPrefsState.toastEnabled) {
      notifications.push({ kind: 'station-new', title, body, href });
    }
    if (notificationPrefsState.osEnabled) {
      fireOsNotification(title, body, () => {
        window.location.hash = href;
      }, { force: true });
    }
    if (notificationPrefsState.stationNewEnabled) notificationSoundState.stationNew.play();
  }

  // Same deep-link shape as sendTestNewStationNotification, reusing the
  // operator's own station/position.
  function sendTestFavoriteNotification() {
    const callsign = stationCallsign || 'TEST-9';
    const title = `Favorite heard: ${callsign}`;
    const body = `${callsign} heard on RF`;
    const hasFocusTarget = stationCallsign && Number.isFinite(myLat) && Number.isFinite(myLon);
    const href = hasFocusTarget
      ? `#/map?focus=${encodeURIComponent(stationCallsign)}&lat=${myLat}&lon=${myLon}`
      : '#/map';
    if (notificationPrefsState.toastEnabled) {
      notifications.push({ kind: 'station-favorite', title, body, href });
    }
    if (notificationPrefsState.osEnabled) {
      fireOsNotification(title, body, () => {
        window.location.hash = href;
      }, { force: true });
    }
    if (notificationPrefsState.stationFavoriteEnabled) notificationSoundState.stationFavorite.play();
  }

  // --- Favorite stations ---
  let newFavCall = $state('');
  let newFavNote = $state('');
  let favError = $state('');
  let addingFav = $state(false);

  function onNewFavCallInput(e) {
    newFavCall = (e.target.value || '').toUpperCase();
    if (e.target.value !== newFavCall) e.target.value = newFavCall;
    favError = '';
  }

  function validateFavCall(call) {
    if (!call) return 'Call sign is required.';
    if (!/^[A-Z0-9-]{1,9}$/.test(call)) return 'Invalid format — up to 9 characters: A-Z, 0-9, -.';
    if (favoriteStationsStore.has(call)) return `${call} is already a favorite.`;
    return '';
  }

  async function addFavorite() {
    const call = (newFavCall || '').trim().toUpperCase();
    const err = validateFavCall(call);
    if (err) { favError = err; return; }
    addingFav = true;
    try {
      await favoriteStationsStore.add(call, (newFavNote || '').trim());
      toasts.success(`Added ${call} to favorites`);
      newFavCall = '';
      newFavNote = '';
    } catch (e) {
      favError = e?.message || 'Could not add favorite.';
    } finally {
      addingFav = false;
    }
  }

  async function removeFavorite(row) {
    try {
      await favoriteStationsStore.remove(row.id);
      toasts.success(`Removed ${row.callsign} from favorites`);
    } catch (e) {
      toasts.error(e?.message || 'Delete failed');
    }
  }

  // --- Excluded stations --- (mirrors Favorite stations above; the inverse concept)
  let newExclCall = $state('');
  let newExclNote = $state('');
  let exclError = $state('');
  let addingExcl = $state(false);

  function onNewExclCallInput(e) {
    newExclCall = (e.target.value || '').toUpperCase();
    if (e.target.value !== newExclCall) e.target.value = newExclCall;
    exclError = '';
  }

  function validateExclCall(call) {
    if (!call) return 'Call sign is required.';
    if (!/^[A-Z0-9-]{1,9}$/.test(call)) return 'Invalid format — up to 9 characters: A-Z, 0-9, -.';
    if (excludedStationsStore.has(call)) return `${call} is already excluded.`;
    return '';
  }

  async function addExclusion() {
    const call = (newExclCall || '').trim().toUpperCase();
    const err = validateExclCall(call);
    if (err) { exclError = err; return; }
    addingExcl = true;
    try {
      await excludedStationsStore.add(call, (newExclNote || '').trim());
      toasts.success(`Excluded ${call} from notifications`);
      newExclCall = '';
      newExclNote = '';
    } catch (e) {
      exclError = e?.message || 'Could not add exclusion.';
    } finally {
      addingExcl = false;
    }
  }

  async function removeExclusion(row) {
    try {
      await excludedStationsStore.remove(row.id);
      toasts.success(`Removed ${row.callsign} from exclusions`);
    } catch (e) {
      toasts.error(e?.message || 'Delete failed');
    }
  }

  function soundOptions(state) {
    const opts = SOUND_PRESETS.map((p) => ({ value: p.id, label: p.label }));
    if (state.hasCustom) opts.push({ value: 'custom', label: `Custom: ${state.customName}` });
    return opts;
  }

  let messageFileInput = $state(null);
  let bulletinFileInput = $state(null);
  let stationEmergencyFileInput = $state(null);
  let stationNewFileInput = $state(null);
  let stationFavoriteFileInput = $state(null);
  let messageUploadError = $state('');
  let bulletinUploadError = $state('');
  let stationEmergencyUploadError = $state('');
  let stationNewUploadError = $state('');
  let stationFavoriteUploadError = $state('');

  async function handleUpload(state, input, setError) {
    const file = input.files?.[0];
    input.value = '';
    if (!file) return;
    setError('');
    try {
      await state.upload(file);
      toasts.success(`Uploaded ${file.name}`);
    } catch (e) {
      setError(e?.message || 'Upload failed.');
    }
  }

  async function handleRemove(state) {
    try {
      await state.removeCustom();
    } catch {
      toasts.error('Could not remove custom sound.');
    }
  }
</script>

<PageHeader title="Notifications" subtitle="Popup and sound alerts for new messages, bulletins, station emergencies, newly heard stations, and favorite stations" />

<Box title="Popup notifications">
  <Toggle
    checked={notificationPrefsState.messageEnabled}
    onCheckedChange={(v) => notificationPrefsState.setMessageEnabled(v)}
    label="Notify me about new messages"
  />
  <Toggle
    checked={notificationPrefsState.bulletinEnabled}
    onCheckedChange={(v) => notificationPrefsState.setBulletinEnabled(v)}
    label="Notify me about new bulletins"
  />
  <Toggle
    checked={notificationPrefsState.stationEmergencyEnabled}
    onCheckedChange={(v) => notificationPrefsState.setStationEmergencyEnabled(v)}
    label="Notify me when a station broadcasts Emergency status"
  />
  <Toggle
    checked={notificationPrefsState.stationNewEnabled}
    onCheckedChange={(v) => notificationPrefsState.setStationNewEnabled(v)}
    label="Notify me when a new station is heard"
  />
  <Toggle
    checked={notificationPrefsState.stationFavoriteEnabled}
    onCheckedChange={(v) => notificationPrefsState.setStationFavoriteEnabled(v)}
    label="Notify me when a favorite station is heard"
  />
  <p class="notif-hint">
    Turn all five off to disable notifications entirely, or just one to
    mute that type while keeping the others. Applies to popups, OS
    notifications, and sounds together. Station emergency alerts fire
    only for Emergency status, matching what real APRS radios alarm on
    -- Priority/Special/other statuses show as a badge on the map
    instead of interrupting you. New-station notifications are off by
    default -- a busy APRS-IS-gated system can hear a new callsign
    often -- and respect the Live Map's RF Only / Direct RX Only
    filters, so a station that wouldn't show up on the map with your
    current filter doesn't interrupt you either. They also never fire
    for digipeaters or repeaters (detected best-effort by icon/path/
    comment -- infrastructure, not an operator) -- that's unconditional,
    not a toggle. Favorite-station notifications (below) always fire
    regardless of any of that.
  </p>
  <p class="sound-label">Notify again after a station has gone unheard for</p>
  <Select
    value={thresholdMode}
    onValueChange={handleThresholdSelect}
    options={stationNewThresholdOptions}
    aria-label="New/favorite station re-notify threshold"
  />
  {#if thresholdMode === CUSTOM_THRESHOLD}
    <div class="threshold-custom">
      <input
        class="threshold-count-input"
        type="number"
        min="1"
        step="1"
        bind:value={customCount}
        onchange={applyCustomThreshold}
        aria-label="Custom threshold count"
      />
      <Select
        value={customUnit}
        onValueChange={(v) => { customUnit = v; applyCustomThreshold(); }}
        options={customUnitOptions}
        aria-label="Custom threshold unit"
      />
    </div>
  {/if}
  <p class="notif-hint">
    Shared by new-station and favorite-station notifications. A station
    only counts as "new" (or a favorite as "heard again") once it's gone
    unheard on this device for at least this long -- so you're not
    pinged on every beacon from something already on the air, but you
    do get a notification if you pick a station up today and it's still
    quiet again by the time you check tomorrow. "Never" notifies once
    per call sign, ever.
  </p>
  <Toggle
    checked={notificationPrefsState.stationNewIncludeWeather}
    onCheckedChange={(v) => notificationPrefsState.setStationNewIncludeWeather(v)}
    label="Notify me about weather stations"
  />
  <p class="notif-hint">
    Off by default: fixed weather stations (like a WXTrak setup) are
    excluded from new-station notifications, since they're infrastructure
    you'll typically hear regularly rather than an operator. Doesn't
    affect favorites -- you can still favorite a specific weather station
    and be notified for it.
  </p>
  <p class="sound-label">How to notify</p>
  <Select
    value={notificationPrefsState.mode}
    onValueChange={(v) => notificationPrefsState.setMode(v)}
    options={notificationModeOptions}
  />
  <p class="notif-hint">
    {#if !notificationPrefsState.supported}
      OS notifications aren't available in this environment (e.g. the
      Android app) — in-app popups always work.
    {:else if notificationPrefsState.permission === 'denied'}
      OS notifications are blocked in your browser's site settings —
      re-enable there, or stick with in-app popups.
    {:else}
      In-app popups appear inside graywolf; OS notifications also show
      up outside the browser tab, once you grant permission.
    {/if}
  </p>
  <div class="sound-actions">
    <Button variant="secondary" onclick={sendTestNotification}>
      Send test notification
    </Button>
    <Button variant="secondary" onclick={sendTestEmergencyNotification}>
      Send test emergency notification
    </Button>
    <Button variant="secondary" onclick={sendTestNewStationNotification}>
      Send test new-station notification
    </Button>
    <Button variant="secondary" onclick={sendTestFavoriteNotification}>
      Send test favorite notification
    </Button>
  </div>
</Box>

<Box title="Message sounds">
  <Toggle
    checked={notificationSoundState.message.enabled}
    onCheckedChange={(v) => notificationSoundState.message.setEnabled(v)}
    label="Play a sound for new messages"
  />
  <p class="sound-label">Sound</p>
  <Select
    value={notificationSoundState.message.presetId}
    onValueChange={(v) => notificationSoundState.message.setPreset(v)}
    options={soundOptions(notificationSoundState.message)}
    aria-label="Message notification sound"
  />
  <div class="sound-actions">
    <Button variant="secondary" onclick={() => messageFileInput.click()}>
      Upload custom sound
    </Button>
    {#if notificationSoundState.message.hasCustom}
      <Button variant="ghost" onclick={() => handleRemove(notificationSoundState.message)}>
        Remove custom sound
      </Button>
    {/if}
    {#if !notificationSoundState.message.isDefault}
      <Button variant="ghost" onclick={() => notificationSoundState.message.resetToDefault()}>
        Reset to default
      </Button>
    {/if}
    <Button variant="ghost" onclick={() => notificationSoundState.message.preview()}>
      Test sound
    </Button>
  </div>
  <input
    class="file-input"
    type="file"
    accept="audio/*"
    bind:this={messageFileInput}
    onchange={() => handleUpload(notificationSoundState.message, messageFileInput, (e) => (messageUploadError = e))}
  />
  {#if messageUploadError}
    <p class="err" role="alert">{messageUploadError}</p>
  {/if}
  <p class="sound-hint">
    Plays when a new directed message or tactical message arrives while
    it wouldn't otherwise raise a popup (muted threads never play a
    sound). Custom uploads are stored in this browser only — up to 2 MB.
  </p>
</Box>

<Box title="Bulletin sounds">
  <Toggle
    checked={notificationSoundState.bulletin.enabled}
    onCheckedChange={(v) => notificationSoundState.bulletin.setEnabled(v)}
    label="Play a sound for new bulletins"
  />
  <p class="sound-label">Sound</p>
  <Select
    value={notificationSoundState.bulletin.presetId}
    onValueChange={(v) => notificationSoundState.bulletin.setPreset(v)}
    options={soundOptions(notificationSoundState.bulletin)}
    aria-label="Bulletin notification sound"
  />
  <div class="sound-actions">
    <Button variant="secondary" onclick={() => bulletinFileInput.click()}>
      Upload custom sound
    </Button>
    {#if notificationSoundState.bulletin.hasCustom}
      <Button variant="ghost" onclick={() => handleRemove(notificationSoundState.bulletin)}>
        Remove custom sound
      </Button>
    {/if}
    {#if !notificationSoundState.bulletin.isDefault}
      <Button variant="ghost" onclick={() => notificationSoundState.bulletin.resetToDefault()}>
        Reset to default
      </Button>
    {/if}
    <Button variant="ghost" onclick={() => notificationSoundState.bulletin.preview()}>
      Test sound
    </Button>
  </div>
  <input
    class="file-input"
    type="file"
    accept="audio/*"
    bind:this={bulletinFileInput}
    onchange={() => handleUpload(notificationSoundState.bulletin, bulletinFileInput, (e) => (bulletinUploadError = e))}
  />
  {#if bulletinUploadError}
    <p class="err" role="alert">{bulletinUploadError}</p>
  {/if}
  <p class="sound-hint">
    Plays when a new inbound bulletin arrives while the Bulletins page
    isn't open. Custom uploads are stored in this browser only — up to
    2 MB.
  </p>
</Box>

<Box title="Station emergency sounds">
  <Toggle
    checked={notificationSoundState.stationEmergency.enabled}
    onCheckedChange={(v) => notificationSoundState.stationEmergency.setEnabled(v)}
    label="Play a sound when a station broadcasts Emergency status"
  />
  <p class="sound-label">Sound</p>
  <Select
    value={notificationSoundState.stationEmergency.presetId}
    onValueChange={(v) => notificationSoundState.stationEmergency.setPreset(v)}
    options={soundOptions(notificationSoundState.stationEmergency)}
    aria-label="Station emergency notification sound"
  />
  <div class="sound-actions">
    <Button variant="secondary" onclick={() => stationEmergencyFileInput.click()}>
      Upload custom sound
    </Button>
    {#if notificationSoundState.stationEmergency.hasCustom}
      <Button variant="ghost" onclick={() => handleRemove(notificationSoundState.stationEmergency)}>
        Remove custom sound
      </Button>
    {/if}
    {#if !notificationSoundState.stationEmergency.isDefault}
      <Button variant="ghost" onclick={() => notificationSoundState.stationEmergency.resetToDefault()}>
        Reset to default
      </Button>
    {/if}
    <Button variant="ghost" onclick={() => notificationSoundState.stationEmergency.preview()}>
      Test sound
    </Button>
  </div>
  <input
    class="file-input"
    type="file"
    accept="audio/*"
    bind:this={stationEmergencyFileInput}
    onchange={() => handleUpload(notificationSoundState.stationEmergency, stationEmergencyFileInput, (e) => (stationEmergencyUploadError = e))}
  />
  {#if stationEmergencyUploadError}
    <p class="err" role="alert">{stationEmergencyUploadError}</p>
  {/if}
  <p class="sound-hint">
    Plays when a heard station's Mic-E status changes to Emergency
    (APRS101 ch 10 table 8) -- not Priority, Special, or other tactical
    statuses, which only show as a badge on the map. Custom uploads are
    stored in this browser only — up to 2 MB.
  </p>
</Box>

<Box title="New station sounds">
  <Toggle
    checked={notificationSoundState.stationNew.enabled}
    onCheckedChange={(v) => notificationSoundState.stationNew.setEnabled(v)}
    label="Play a sound when a new station is heard"
  />
  <p class="sound-label">Sound</p>
  <Select
    value={notificationSoundState.stationNew.presetId}
    onValueChange={(v) => notificationSoundState.stationNew.setPreset(v)}
    options={soundOptions(notificationSoundState.stationNew)}
    aria-label="New station notification sound"
  />
  <div class="sound-actions">
    <Button variant="secondary" onclick={() => stationNewFileInput.click()}>
      Upload custom sound
    </Button>
    {#if notificationSoundState.stationNew.hasCustom}
      <Button variant="ghost" onclick={() => handleRemove(notificationSoundState.stationNew)}>
        Remove custom sound
      </Button>
    {/if}
    {#if !notificationSoundState.stationNew.isDefault}
      <Button variant="ghost" onclick={() => notificationSoundState.stationNew.resetToDefault()}>
        Reset to default
      </Button>
    {/if}
    <Button variant="ghost" onclick={() => notificationSoundState.stationNew.preview()}>
      Test sound
    </Button>
  </div>
  <input
    class="file-input"
    type="file"
    accept="audio/*"
    bind:this={stationNewFileInput}
    onchange={() => handleUpload(notificationSoundState.stationNew, stationNewFileInput, (e) => (stationNewUploadError = e))}
  />
  {#if stationNewUploadError}
    <p class="err" role="alert">{stationNewUploadError}</p>
  {/if}
  <p class="sound-hint">
    Plays when a station is heard again after going unheard for the
    threshold set above. Defaults to a quieter chime than the other
    notification types, since this one can fire more often on a busy
    network. Custom uploads are stored in this browser only — up to 2 MB.
  </p>
</Box>

<Box title="Favorite station sounds">
  <Toggle
    checked={notificationSoundState.stationFavorite.enabled}
    onCheckedChange={(v) => notificationSoundState.stationFavorite.setEnabled(v)}
    label="Play a sound when a favorite station is heard"
  />
  <p class="sound-label">Sound</p>
  <Select
    value={notificationSoundState.stationFavorite.presetId}
    onValueChange={(v) => notificationSoundState.stationFavorite.setPreset(v)}
    options={soundOptions(notificationSoundState.stationFavorite)}
    aria-label="Favorite station notification sound"
  />
  <div class="sound-actions">
    <Button variant="secondary" onclick={() => stationFavoriteFileInput.click()}>
      Upload custom sound
    </Button>
    {#if notificationSoundState.stationFavorite.hasCustom}
      <Button variant="ghost" onclick={() => handleRemove(notificationSoundState.stationFavorite)}>
        Remove custom sound
      </Button>
    {/if}
    {#if !notificationSoundState.stationFavorite.isDefault}
      <Button variant="ghost" onclick={() => notificationSoundState.stationFavorite.resetToDefault()}>
        Reset to default
      </Button>
    {/if}
    <Button variant="ghost" onclick={() => notificationSoundState.stationFavorite.preview()}>
      Test sound
    </Button>
  </div>
  <input
    class="file-input"
    type="file"
    accept="audio/*"
    bind:this={stationFavoriteFileInput}
    onchange={() => handleUpload(notificationSoundState.stationFavorite, stationFavoriteFileInput, (e) => (stationFavoriteUploadError = e))}
  />
  {#if stationFavoriteUploadError}
    <p class="err" role="alert">{stationFavoriteUploadError}</p>
  {/if}
  <p class="sound-hint">
    Plays when a favorite station (below) is heard again after going
    unheard for the threshold set above. Defaults to a different quiet
    preset than New station sounds, so a favorite is distinguishable by
    ear. Custom uploads are stored in this browser only — up to 2 MB.
  </p>
</Box>

<Box title="Favorite stations">
  <p class="notif-hint fav-intro">
    A favorite always raises a notification when heard again (subject to
    the re-notify threshold above), regardless of the Live Map's RF Only
    / Direct RX Only filters or digipeater/repeater status. Shared across every
    device pointed at this graywolf instance. Enter a bare call sign
    (like <code>N0CALL</code>) to match every SSID of that station, or
    an SSID-qualified call sign (like <code>N0CALL-7</code>) to match
    only that exact one -- a bare call sign never matches an unrelated
    one that merely shares letters (e.g. <code>N0CALL</code> won't match
    <code>N0CALLX</code>).
  </p>

  <form class="fav-add" onsubmit={(e) => { e.preventDefault(); addFavorite(); }}>
    <Input
      type="text"
      value={newFavCall}
      oninput={onNewFavCallInput}
      placeholder="N0CALL"
      aria-label="Call sign to favorite"
    />
    <Input
      type="text"
      bind:value={newFavNote}
      placeholder="Note (optional)"
      aria-label="Note"
    />
    <Button variant="primary" type="submit" disabled={addingFav}>
      <Icon name="star" size="sm" />
      Add
    </Button>
  </form>
  {#if favError}
    <p class="err" role="alert">{favError}</p>
  {/if}

  {#if favoriteStationsStore.entries.length === 0}
    <p class="notif-hint fav-empty">No favorite stations yet.</p>
  {:else}
    <ul class="fav-rows">
      {#each favoriteStationsStore.entries as row (row.id)}
        <li class="fav-row">
          <div class="fav-text">
            <span class="fav-call">{row.callsign}</span>
            {#if row.note}<span class="fav-note">{row.note}</span>{/if}
          </div>
          <Button variant="ghost" size="sm" onclick={() => removeFavorite(row)} aria-label={`Remove ${row.callsign} from favorites`}>
            <Icon name="trash-2" size="sm" />
          </Button>
        </li>
      {/each}
    </ul>
  {/if}
</Box>

<Box title="Excluded stations">
  <p class="notif-hint fav-intro">
    An excluded station never raises a new-station or favorite
    notification -- checked before everything else, so it wins even over
    a favorite. For a station the digipeater/repeater/weather-station
    detection doesn't catch (or anything else you just don't want to
    hear about). Same bare-vs-SSID-qualified matching as favorites above.
    Shared across every device pointed at this graywolf instance.
  </p>

  <form class="fav-add" onsubmit={(e) => { e.preventDefault(); addExclusion(); }}>
    <Input
      type="text"
      value={newExclCall}
      oninput={onNewExclCallInput}
      placeholder="N0CALL"
      aria-label="Call sign to exclude"
    />
    <Input
      type="text"
      bind:value={newExclNote}
      placeholder="Note (optional)"
      aria-label="Note"
    />
    <Button variant="primary" type="submit" disabled={addingExcl}>
      <Icon name="ban" size="sm" />
      Add
    </Button>
  </form>
  {#if exclError}
    <p class="err" role="alert">{exclError}</p>
  {/if}

  {#if excludedStationsStore.entries.length === 0}
    <p class="notif-hint fav-empty">No excluded stations.</p>
  {:else}
    <ul class="fav-rows">
      {#each excludedStationsStore.entries as row (row.id)}
        <li class="fav-row">
          <div class="fav-text">
            <span class="fav-call">{row.callsign}</span>
            {#if row.note}<span class="fav-note">{row.note}</span>{/if}
          </div>
          <Button variant="ghost" size="sm" onclick={() => removeExclusion(row)} aria-label={`Remove ${row.callsign} from exclusions`}>
            <Icon name="trash-2" size="sm" />
          </Button>
        </li>
      {/each}
    </ul>
  {/if}
</Box>

<style>
  .notif-hint,
  .sound-hint {
    margin-top: 12px;
    font-size: 13px;
    color: var(--text-muted);
  }
  .sound-label {
    margin-top: 16px;
    margin-bottom: 6px;
    font-size: 13px;
    font-weight: 500;
    color: var(--text-default);
  }
  .sound-actions {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
    margin-top: 12px;
  }
  .threshold-custom {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-top: 8px;
  }
  .threshold-count-input {
    width: 80px;
    padding: 6px 8px;
    border-radius: 6px;
    border: 1px solid var(--border-color, #444);
    background: var(--bg-input, transparent);
    color: var(--text-default);
    font-size: 13px;
  }
  .file-input {
    display: none;
  }
  .err {
    margin-top: 8px;
    color: var(--color-danger, #d33);
    font-size: 13px;
  }
  .fav-intro { margin-top: 0; margin-bottom: 16px; }
  .fav-intro code {
    font-family: var(--font-mono);
    font-size: 12px;
    padding: 1px 4px;
    border-radius: 4px;
    background: var(--color-bg-subtle, rgba(127, 127, 127, 0.12));
  }
  .fav-add {
    display: flex;
    gap: 8px;
    align-items: center;
    flex-wrap: wrap;
  }
  /* chonky inputs carry a 1rem bottom margin (stacked-form default); in
     this single row it inflates the wrapper so align-items:center drops
     the Add button below the fields. Zero it so input and button align. */
  .fav-add :global(input) { min-width: 0; margin-bottom: 0; }
  .fav-empty { font-style: italic; }
  .fav-rows {
    list-style: none;
    margin: 16px 0 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .fav-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: 8px 10px;
    border-radius: 6px;
    background: var(--color-bg-subtle, rgba(127, 127, 127, 0.06));
  }
  .fav-text {
    display: flex;
    align-items: baseline;
    gap: 10px;
    min-width: 0;
  }
  .fav-call {
    font-family: var(--font-mono);
    font-weight: 600;
    font-size: 14px;
  }
  .fav-note {
    font-size: 12px;
    color: var(--text-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
</style>
