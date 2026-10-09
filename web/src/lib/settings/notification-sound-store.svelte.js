// Device-local notification sound preferences, one instance per kind
// ('message' | 'bulletin'). Like notification-prefs-store.svelte.js,
// NOT synced to the server — an operator's chosen sound (built-in
// preset or an uploaded custom file) lives in this browser only.
//
// Settings (enabled + which preset/custom is selected) persist to
// localStorage; an uploaded custom file's bytes live in IndexedDB
// (see notificationSoundsDb.js) since localStorage can't hold Blobs.
//
// `play()` is the real trigger path — it no-ops when the operator has
// disabled sound for this kind. `preview()` always plays, bypassing the
// enabled check, so the "Test sound" button in NotificationsSettings.svelte
// can preview a sound before turning it on (mirrors the "force" test
// notification convention in osNotify.js).

import { playPreset } from '../soundPresets.js';
import { getCustomSound, putCustomSound, deleteCustomSound } from '../notificationSoundsDb.js';
import {
  MAX_SOUND_BYTES,
  isValidPresetId,
  parsePresetId,
  parseEnabledFlag,
  validateUploadFile,
  fallbackPresetId,
} from './notification-sound-core.js';

// stationNew/stationFavorite default to the quietest built-in presets
// (ping/chime, both peak gain 0.25) rather than one of the shipped
// wav/mp3 files -- a new-station notification can fire far more often
// than a message/bulletin/Emergency on a busy network, so it's meant to
// be a subtle aside, not something that competes with the louder shipped
// defaults. The two use different presets purely so a favorite is
// audibly distinguishable from a plain new-station hit without either
// one being loud -- favorite gets the (slightly more present) two-tone
// chime since it's the rarer, more-wanted event of the two.
const DEFAULT_PRESET = {
  message: 'aprs-message',
  bulletin: 'aprs-bulletin',
  stationEmergency: 'emergency-mp3',
  stationNew: 'ping',
  stationFavorite: 'chime',
};
const LS_PREFIX = 'gw-notification-sound-';

export { MAX_SOUND_BYTES };

function readEnabled(key) {
  try {
    return parseEnabledFlag(localStorage.getItem(key));
  } catch {
    return true;
  }
}

function readPreset(key, defaultId) {
  try {
    return parsePresetId(localStorage.getItem(key), defaultId);
  } catch {
    return defaultId;
  }
}

function write(key, value) {
  try {
    localStorage.setItem(key, value);
  } catch {
    /* ignore */
  }
}

function makeKindState(kind) {
  const enabledKey = `${LS_PREFIX}${kind}-enabled`;
  const presetKey = `${LS_PREFIX}${kind}-preset`;
  const defaultId = DEFAULT_PRESET[kind];

  let enabled = $state(readEnabled(enabledKey));
  let presetId = $state(readPreset(presetKey, defaultId));
  let hasCustom = $state(false);
  let customName = $state('');
  let cachedUrl = null;

  // Hydrate custom-sound presence from IndexedDB — async, so the UI
  // starts without it and updates once the lookup resolves.
  getCustomSound(kind)
    .then((rec) => {
      if (rec) {
        hasCustom = true;
        customName = rec.name || 'Custom sound';
      } else if (presetId === 'custom') {
        // Preference pointed at a custom sound that no longer exists
        // (e.g. cleared browser storage) — fall back to the preset default.
        presetId = fallbackPresetId(presetId, defaultId);
        write(presetKey, presetId);
      }
    })
    .catch(() => {});

  async function doPlay() {
    if (presetId === 'custom') {
      try {
        const rec = await getCustomSound(kind);
        if (rec?.blob) {
          if (!cachedUrl) cachedUrl = URL.createObjectURL(rec.blob);
          const audio = new Audio(cachedUrl);
          audio.play().catch(() => {});
          return;
        }
      } catch {
        /* fall through to preset */
      }
    }
    playPreset(fallbackPresetId(presetId, defaultId));
  }

  return {
    get enabled() {
      return enabled;
    },
    get presetId() {
      return presetId;
    },
    get hasCustom() {
      return hasCustom;
    },
    get customName() {
      return customName;
    },
    /** True when the current selection is already this kind's shipped default (not a custom upload or a different preset). */
    get isDefault() {
      return presetId === defaultId;
    },
    setEnabled(v) {
      enabled = !!v;
      write(enabledKey, enabled ? '1' : '0');
    },
    setPreset(id) {
      if (id !== 'custom' && !isValidPresetId(id)) return;
      presetId = id;
      write(presetKey, id);
    },
    /**
     * Switch back to this kind's shipped default preset (`aprs-message`,
     * `aprs-bulletin`, `siren`). Only changes which sound is active --
     * does not delete an uploaded custom sound, so the operator can
     * still switch back to "Custom" from the dropdown afterward.
     */
    resetToDefault() {
      presetId = defaultId;
      write(presetKey, defaultId);
    },
    /**
     * @param {File} file must be an audio/* MIME type and under MAX_SOUND_BYTES.
     */
    async upload(file) {
      const err = validateUploadFile(file);
      if (err) throw new Error(err);
      await putCustomSound(kind, file, file.name);
      hasCustom = true;
      customName = file.name;
      if (cachedUrl) {
        URL.revokeObjectURL(cachedUrl);
        cachedUrl = null;
      }
      presetId = 'custom';
      write(presetKey, 'custom');
    },
    async removeCustom() {
      await deleteCustomSound(kind);
      hasCustom = false;
      customName = '';
      if (cachedUrl) {
        URL.revokeObjectURL(cachedUrl);
        cachedUrl = null;
      }
      presetId = fallbackPresetId(presetId, defaultId);
      write(presetKey, presetId);
    },
    /** Real trigger path — no-ops when sound is disabled for this kind. */
    play() {
      if (!enabled) return;
      return doPlay();
    },
    /** Always plays, regardless of the enabled toggle — for the Test sound button. */
    preview() {
      return doPlay();
    },
  };
}

export const notificationSoundState = {
  message: makeKindState('message'),
  bulletin: makeKindState('bulletin'),
  stationEmergency: makeKindState('stationEmergency'),
  stationNew: makeKindState('stationNew'),
  stationFavorite: makeKindState('stationFavorite'),
};
