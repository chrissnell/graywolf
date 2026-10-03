// Device-local new-activity notification mode. Like ui-scale-store.svelte.js
// and log-prefs-store.svelte.js, this is NOT synced to the server: whether
// an operator wants OS-level browser notifications is a per-device
// browser-permission concept, not a station preference.
//
//   mode — 'toast' (in-app popup only, default) | 'os' (OS notification
//          only) | 'both'.
//
// Requesting 'os'/'both' triggers the browser's permission prompt via
// setMode(); a denial falls back to 'toast' so the operator never lands
// in a mode that silently does nothing. `supported` feature-detects the
// Notification API rather than hardcoding a platform list — it's false
// inside the Android build's in-process WebView (see
// docs/wiki/code-map.md's Android section), so NotificationsSettings.svelte
// hides the os/both options there while still offering plain toast popups.
//
// messageEnabled/bulletinEnabled/stationEmergencyEnabled are independent
// master switches, on by default — turning all off is "notifications
// off"; turning off just one mutes that type's popups (toast + OS) and
// sound while leaving the others alone. Gates messagesTransport.js's
// maybeNotifyInbound, bulletinsTransport.js's poll(), and
// stationAlertsTransport.js's poll() ahead of the toast/OS/sound calls.
//
// stationNewEnabled (new-station-heard) is the one exception: it defaults
// OFF. A busy APRS-IS-gated system can hear a genuinely new callsign every
// few minutes, so unlike a DM/bulletin/Emergency this type would otherwise
// start spamming every existing install the moment this feature ships.
// Gates stationNewTransport.js's poll().
//
// stationFavoriteEnabled is independent of stationNewEnabled -- an
// operator can turn the general "new station" firehose off and keep just
// favorite-station notifications on (or vice versa). Defaults ON: unlike
// stationNewEnabled, it's harmless by default since it only ever fires
// for a callsign the operator deliberately added to their favorites list
// (see favoriteStationsStore.svelte.js) -- an empty list means it never
// fires at all.
//
// stationNewThresholdSecs is shared by both: a station (favorite or not)
// only counts as "new" again after this much time has passed since this
// device last recorded hearing it -- see station-new-diff-core.js.
//
// stationNewIncludeWeather is a general-path-only filter, off by default:
// weather stations (StationRosterDTO.is_weather_station) are excluded
// from the general "new station" notification unless the operator opts
// in ("some may want to see weather stations", 2026-07-31 -- so this is
// a toggle, not an unconditional exclusion like the digipeater filter).
// Never applied to the favorites path -- an operator can still favorite
// a specific weather station.

import {
  parseMode,
  parseEnabledFlag,
  resolveModeAfterPermission,
  parseStationNewThresholdSecs,
  DEFAULT_STATION_NEW_THRESHOLD_SECS,
} from './notification-prefs-core.js';

export {
  STATION_NEW_THRESHOLDS,
  stationNewThresholdMs,
  isPresetThresholdSecs,
  secsToCustomInput,
  customInputToSecs,
  MAX_CUSTOM_THRESHOLD_SECS,
} from './notification-prefs-core.js';

const LS_MODE = 'gw-notification-mode';
const LS_MESSAGE_ENABLED = 'gw-notification-message-enabled';
const LS_BULLETIN_ENABLED = 'gw-notification-bulletin-enabled';
const LS_STATION_EMERGENCY_ENABLED = 'gw-notification-station-emergency-enabled';
const LS_STATION_NEW_ENABLED = 'gw-notification-station-new-enabled';
const LS_STATION_FAVORITE_ENABLED = 'gw-notification-station-favorite-enabled';
const LS_STATION_NEW_THRESHOLD_SECS = 'gw-notification-station-new-threshold-secs';
const LS_STATION_NEW_INCLUDE_WEATHER = 'gw-notification-station-new-include-weather';

function readMode() {
  try {
    return parseMode(localStorage.getItem(LS_MODE));
  } catch {
    return 'toast';
  }
}

function writeMode(v) {
  try {
    localStorage.setItem(LS_MODE, v);
  } catch {
    /* ignore */
  }
}

function readEnabled(key, def = true) {
  try {
    return parseEnabledFlag(localStorage.getItem(key), def);
  } catch {
    return def;
  }
}

function writeEnabled(key, v) {
  try {
    localStorage.setItem(key, v ? '1' : '0');
  } catch {
    /* ignore */
  }
}

function readThresholdSecs() {
  try {
    return parseStationNewThresholdSecs(localStorage.getItem(LS_STATION_NEW_THRESHOLD_SECS));
  } catch {
    return DEFAULT_STATION_NEW_THRESHOLD_SECS;
  }
}

export const notificationPrefsState = (() => {
  let mode = $state(readMode());
  let messageEnabled = $state(readEnabled(LS_MESSAGE_ENABLED));
  let bulletinEnabled = $state(readEnabled(LS_BULLETIN_ENABLED));
  let stationEmergencyEnabled = $state(readEnabled(LS_STATION_EMERGENCY_ENABLED));
  let stationNewEnabled = $state(readEnabled(LS_STATION_NEW_ENABLED, false));
  let stationFavoriteEnabled = $state(readEnabled(LS_STATION_FAVORITE_ENABLED, true));
  let stationNewThresholdSecs = $state(readThresholdSecs());
  let stationNewIncludeWeather = $state(readEnabled(LS_STATION_NEW_INCLUDE_WEATHER, false));
  const supported = typeof window !== 'undefined' && typeof Notification !== 'undefined';

  return {
    get mode() {
      return mode;
    },
    get supported() {
      return supported;
    },
    get permission() {
      return supported ? Notification.permission : 'unsupported';
    },
    get toastEnabled() {
      return mode === 'toast' || mode === 'both';
    },
    get osEnabled() {
      return supported && (mode === 'os' || mode === 'both') && Notification.permission === 'granted';
    },
    get messageEnabled() {
      return messageEnabled;
    },
    get bulletinEnabled() {
      return bulletinEnabled;
    },
    get stationEmergencyEnabled() {
      return stationEmergencyEnabled;
    },
    get stationNewEnabled() {
      return stationNewEnabled;
    },
    get stationFavoriteEnabled() {
      return stationFavoriteEnabled;
    },
    get stationNewThresholdSecs() {
      return stationNewThresholdSecs;
    },
    get stationNewIncludeWeather() {
      return stationNewIncludeWeather;
    },
    setMessageEnabled(v) {
      messageEnabled = !!v;
      writeEnabled(LS_MESSAGE_ENABLED, messageEnabled);
    },
    setBulletinEnabled(v) {
      bulletinEnabled = !!v;
      writeEnabled(LS_BULLETIN_ENABLED, bulletinEnabled);
    },
    setStationEmergencyEnabled(v) {
      stationEmergencyEnabled = !!v;
      writeEnabled(LS_STATION_EMERGENCY_ENABLED, stationEmergencyEnabled);
    },
    setStationNewEnabled(v) {
      stationNewEnabled = !!v;
      writeEnabled(LS_STATION_NEW_ENABLED, stationNewEnabled);
    },
    setStationFavoriteEnabled(v) {
      stationFavoriteEnabled = !!v;
      writeEnabled(LS_STATION_FAVORITE_ENABLED, stationFavoriteEnabled);
    },
    setStationNewThresholdSecs(secs) {
      stationNewThresholdSecs = parseStationNewThresholdSecs(String(secs));
      try {
        localStorage.setItem(LS_STATION_NEW_THRESHOLD_SECS, String(stationNewThresholdSecs));
      } catch {
        /* ignore */
      }
    },
    setStationNewIncludeWeather(v) {
      stationNewIncludeWeather = !!v;
      writeEnabled(LS_STATION_NEW_INCLUDE_WEATHER, stationNewIncludeWeather);
    },
    /**
     * Called from the Preferences mode picker. Requesting 'os'/'both'
     * triggers the permission prompt when not yet decided; a denial
     * falls back to 'toast'.
     * @param {'toast'|'os'|'both'} next
     */
    async setMode(next) {
      if ((next === 'os' || next === 'both') && supported) {
        let perm = Notification.permission;
        if (perm === 'default') perm = await Notification.requestPermission();
        if (perm !== 'granted') {
          mode = 'toast';
          writeMode('toast');
          return;
        }
      }
      mode = next;
      writeMode(next);
    },
  };
})();
