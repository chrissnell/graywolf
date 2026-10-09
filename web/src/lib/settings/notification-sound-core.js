// Pure logic for notification-sound-store.svelte.js — kept runes-free so
// it can be unit-tested under `node --test` without a Svelte compile
// step (mirrors notification-prefs-core.js / channelsCore.js).

import { SOUND_PRESETS } from '../soundPresets.js';

export const MAX_SOUND_BYTES = 2 * 1024 * 1024; // 2 MB

export function isValidPresetId(id) {
  return SOUND_PRESETS.some((p) => p.id === id);
}

/**
 * @param {unknown} stored value read back from localStorage
 * @param {string} defaultId the kind's default preset id
 */
export function parsePresetId(stored, defaultId) {
  return stored === 'custom' || isValidPresetId(stored) ? stored : defaultId;
}

/** @param {unknown} stored '1'/'0' from localStorage, or null if never set */
export function parseEnabledFlag(stored) {
  return stored === null || stored === undefined ? true : stored === '1';
}

/**
 * @param {{type?: string, size: number}} file
 * @param {number} [maxBytes]
 * @returns {string|null} an error message, or null if the file is acceptable
 */
export function validateUploadFile(file, maxBytes = MAX_SOUND_BYTES) {
  if (!file) return 'No file selected.';
  if (file.type && !file.type.startsWith('audio/')) return 'Please choose an audio file.';
  if (file.size > maxBytes) return 'Sound file is too large (2 MB max).';
  return null;
}

/**
 * Which preset id should actually be synthesized/played when the
 * "custom" slot can't be used — either because the operator's current
 * selection is a real preset (passed through unchanged) or because
 * 'custom' was selected but the uploaded sound is no longer available
 * (e.g. IndexedDB was cleared independently of localStorage).
 * @param {string} presetId
 * @param {string} defaultId the kind's default preset id
 */
export function fallbackPresetId(presetId, defaultId) {
  if (presetId === 'custom') return defaultId;
  return isValidPresetId(presetId) ? presetId : defaultId;
}
