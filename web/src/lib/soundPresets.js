// Built-in notification sounds. `aprs-message`/`aprs-bulletin` (wav) and
// `emergency-mp3` (mp3) are shipped audio files in web/public/sounds/ —
// the station operator's own recordings, used as the shipped defaults
// for message/bulletin/station-emergency sound respectively. The rest
// are synthesized via the Web Audio API rather than bundled as assets,
// so they cost nothing to keep in the repo.

// Louder/more urgent presets (siren, klaxon, urgent-beeps) override the
// default per-tone gain (0.25, see playTones) and, for klaxon, the
// oscillator waveform -- 'sawtooth' carries more high-frequency
// harmonic content than 'sine' at the same peak amplitude, which reads
// as louder/harsher on typical laptop/phone speakers. Built with small
// generator helpers rather than hand-written arrays so the repeat
// counts stay easy to tune.

/** Alternates between two tones `count` times -- a classic two-tone siren wail. */
function alternating(freqA, freqB, count, dur, gain) {
  const tones = [];
  for (let i = 0; i < count; i++) {
    tones.push({ freq: i % 2 === 0 ? freqA : freqB, dur, gain });
  }
  return tones;
}

/** Repeats a single tone `count` times -- rapid urgent beeping. */
function repeated(freq, dur, gain, count, type) {
  const tones = [];
  for (let i = 0; i < count; i++) {
    tones.push({ freq, dur, gain, type });
  }
  return tones;
}

export const SOUND_PRESETS = [
  { id: 'aprs-message', label: 'APRS Message', url: '/sounds/aprs-message.wav' },
  { id: 'aprs-bulletin', label: 'APRS Bulletin', url: '/sounds/aprs-bulletin.wav' },
  { id: 'emergency-mp3', label: 'Emergency', url: '/sounds/emergency.mp3' },
  { id: 'chime', label: 'Chime', tones: [{ freq: 880, dur: 0.09 }, { freq: 1318.5, dur: 0.16 }] },
  { id: 'ping', label: 'Ping', tones: [{ freq: 1046.5, dur: 0.14 }] },
  { id: 'alert', label: 'Alert', tones: [{ freq: 660, dur: 0.08 }, { freq: 660, dur: 0.1 }] },
  // Louder/more urgent options -- suited to Emergency alerts, but
  // available for any notification type.
  { id: 'siren', label: 'Siren (loud)', tones: alternating(900, 1300, 14, 0.13, 0.6) },
  { id: 'klaxon', label: 'Klaxon (loud)', tones: repeated(220, 0.35, 0.6, 3, 'sawtooth') },
  { id: 'urgent-beeps', label: 'Urgent Beeps (loud)', tones: repeated(1400, 0.09, 0.6, 8) },
];

const GAP_SEC = 0.05;
const DEFAULT_GAIN = 0.25;

/** @type {AudioContext|null} */
let ctx = null;

function audioCtx() {
  if (typeof window === 'undefined') return null;
  const Ctor = window.AudioContext || window.webkitAudioContext;
  if (!Ctor) return null;
  if (!ctx) ctx = new Ctor();
  if (ctx.state === 'suspended') ctx.resume().catch(() => {});
  return ctx;
}

function playTones(tones) {
  const ac = audioCtx();
  if (!ac) return;
  let t = ac.currentTime;
  for (const tone of tones) {
    const peakGain = tone.gain ?? DEFAULT_GAIN;
    const osc = ac.createOscillator();
    const gain = ac.createGain();
    osc.type = tone.type || 'sine';
    osc.frequency.value = tone.freq;
    gain.gain.setValueAtTime(0.0001, t);
    gain.gain.exponentialRampToValueAtTime(peakGain, t + 0.01);
    gain.gain.exponentialRampToValueAtTime(0.0001, t + tone.dur);
    osc.connect(gain).connect(ac.destination);
    osc.start(t);
    osc.stop(t + tone.dur + 0.02);
    t += tone.dur + GAP_SEC;
  }
}

/** @param {string} id one of SOUND_PRESETS' ids; falls back to the first preset */
export function resolvePreset(id) {
  return SOUND_PRESETS.find((p) => p.id === id) || SOUND_PRESETS[0];
}

/**
 * @param {string} id one of SOUND_PRESETS' ids; falls back to the first preset.
 * No-ops outside a browser (no `window`/`Audio`) rather than throwing, so
 * callers (and their tests) don't need to feature-detect first.
 */
export function playPreset(id) {
  if (typeof window === 'undefined') return;
  const preset = resolvePreset(id);
  if (preset.url) {
    if (typeof Audio === 'undefined') return;
    new Audio(preset.url).play().catch(() => {});
    return;
  }
  playTones(preset.tones);
}
