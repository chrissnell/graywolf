// Weather labels: the temperature chip. Rather than floating as its own
// map marker, the temp is written into a slot that lives inside the
// station marker (the `.wx-temp` element, see stations.js), stacked just
// below the callsign label and right-justified to it. That keeps the temp
// pinned to the callsign at every zoom/wind direction instead of drifting
// over the wind barb.
//
// This layer owns only the temp's *content*: the formatted value, unit
// conversion (unitsState.isMetric), visibility (Weather toggle) and the
// Direct RX filter. Layout/positioning is the station marker's job (CSS).
// The slot element is fetched per-callsign via the getTempSlot callback
// the host wires to the stations layer.
//
// Field name matches WeatherDTO in pkg/webapi/stations.go: temp_f. Wind
// is rendered separately as a barb (see wind-barbs.js).

import { unitsState } from '../../settings/units-store.svelte.js';

function formatTemp(wx, isMetric) {
  if (!wx || wx.temp_f == null) return '';
  const t = isMetric ? ((wx.temp_f - 32) * 5) / 9 : wx.temp_f;
  return `${Math.round(t)}°${isMetric ? 'C' : 'F'}`;
}

function hideSlot(slot) {
  if (slot && slot.isConnected) {
    slot.textContent = '';
    slot.style.display = 'none';
  }
}

export function mountWeatherLayer(map, getStations, { getTempSlot } = {}) {
  // callsign → { slot, label }
  const slots = new Map();

  // Optional per-station predicate. Stations failing it have no temp.
  // Driven by the Direct RX toggle.
  let filter = null;
  let visible = true;

  function refresh() {
    const stations = getStations();
    if (!stations) return;

    const isMetric = unitsState.isMetric;
    const seen = new Set();

    for (const [callsign, s] of stations) {
      if (filter && !filter(s)) continue;
      if (!s.weather) continue;
      const label = formatTemp(s.weather, isMetric);
      if (!label) continue;
      const slot = getTempSlot && getTempSlot(callsign);
      if (!slot) continue; // station marker not mounted yet; next pass

      seen.add(callsign);
      let entry = slots.get(callsign);
      if (!entry || entry.slot !== slot) {
        // First sight, or the station marker was recreated under us.
        entry = { slot, label: null };
        slots.set(callsign, entry);
      }
      if (entry.label !== label) {
        slot.textContent = label;
        entry.label = label;
      }
      slot.style.display = visible ? 'block' : 'none';
    }

    // Clear slots whose station no longer reports weather (or fell out of
    // the timerange / bbox / Direct RX filter). The station marker itself
    // may still exist, so we blank the slot rather than remove anything.
    for (const [callsign, entry] of slots) {
      if (!seen.has(callsign)) {
        hideSlot(entry.slot);
        slots.delete(callsign);
      }
    }
  }

  function setVisible(next) {
    visible = !!next;
    const display = visible ? 'block' : 'none';
    for (const { slot } of slots.values()) {
      if (slot && slot.isConnected) slot.style.display = display;
    }
  }

  function setFilter(pred) {
    filter = typeof pred === 'function' ? pred : null;
    refresh();
  }

  function destroy() {
    for (const { slot } of slots.values()) hideSlot(slot);
    slots.clear();
  }

  return { refresh, destroy, setVisible, setFilter };
}
