// Device-local APRS Logs display preferences. Like ui-scale, these are
// intentionally NOT synced to the server: whether an operator wants the
// packet log to keep refreshing and auto-scroll is a per-device reading
// choice (e.g. freeze the log on a phone to study a packet path without
// it shifting underneath them — graywolf #373).
//
//   autoRefresh — poll the server for new packets every few seconds.
//                 Off freezes the displayed list so the content the
//                 operator is reading stops changing.
//   autoScroll  — follow new packets to the bottom of the viewer.
//                 Off keeps the scroll position put as packets arrive.
//   showNonPrintable — surface non-printable bytes in the raw packet line
//                 as styled <0x7f> hex tokens (GH #376). Off by default:
//                 ordinary operators see clean text, and only those
//                 diagnosing a malformed packet opt into the noise.
//
// autoRefresh / autoScroll default on, preserving the prior always-live
// behavior; showNonPrintable defaults off.

const LS_AUTO_REFRESH = 'aprs-log-auto-refresh';
const LS_AUTO_SCROLL = 'aprs-log-auto-scroll';
const LS_SHOW_NONPRINTABLE = 'aprs-log-show-nonprintable';

function readBool(key, dflt) {
  try {
    const v = localStorage.getItem(key);
    return v === null ? dflt : v === 'true';
  } catch {
    return dflt;
  }
}

function writeBool(key, v) {
  try { localStorage.setItem(key, String(v)); } catch {}
}

export const logPrefsState = (() => {
  let autoRefresh = $state(readBool(LS_AUTO_REFRESH, true));
  let autoScroll = $state(readBool(LS_AUTO_SCROLL, true));
  let showNonPrintable = $state(readBool(LS_SHOW_NONPRINTABLE, false));

  return {
    get autoRefresh() { return autoRefresh; },
    get autoScroll() { return autoScroll; },
    get showNonPrintable() { return showNonPrintable; },
    setAutoRefresh(v) {
      autoRefresh = !!v;
      writeBool(LS_AUTO_REFRESH, autoRefresh);
    },
    setAutoScroll(v) {
      autoScroll = !!v;
      writeBool(LS_AUTO_SCROLL, autoScroll);
    },
    setShowNonPrintable(v) {
      showNonPrintable = !!v;
      writeBool(LS_SHOW_NONPRINTABLE, showNonPrintable);
    },
  };
})();
