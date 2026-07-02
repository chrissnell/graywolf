# Live Map direct-RX heatmap layer — design

**Status:** proposed (issue GRA-232 thread, 2026-07-02)

## Goal

Add a Live Map layer that renders a heatmap of the RF energy the station has
directly received during the selected time interval. The heat answers one
question: *where does this station actually hear signals from, on the air, and
how much?*

"Directly received" means the last transmitter in the packet's used path — the
one whose RF our antenna actually decoded. Concretely:

- **Direct packet** (`direction == RX`, `hops == 0`): the last transmitter is
  the originating station itself, so the heat is placed at that station's
  position.
- **Digipeated packet we heard on RF** (`direction == RX`, `hops > 0`): the last
  transmitter is the digipeater that repeated it to us, so the heat is placed at
  **that digipeater's location**, not the originating station's. (Confirmed with
  the requester: "if we hear a digipeater directly and that digi is repeating
  another station, we count that heat as coming from the digipeater's location.")
- **APRS-IS / Internet-to-RF gated** (`direction == IS`, or `gated`): never
  contributes heat. Nothing was heard on our radio.

The intensity at each location is driven by **total packet count**. A location
we have heard once is nearly cold; a location we hear constantly is hot. This is
the core requirement and the main reason for a dedicated backend endpoint.

## Approach: dedicated backend endpoint (approach B, approved)

The requester chose a dedicated API endpoint over the client-side-only option.
Two hard reasons make this the right call and rule out reusing `/api/stations`:

1. **Counts are not available today.** There is no packet counter anywhere in
   the codebase. The `positions` table *dedups* static re-beacons: a fixed
   station beaconing a thousand times from one spot collapses to a single row
   (`pkg/historydb/historydb.go`, the `posEpsilon` static-re-beacon branch).
   Counting stored rows would therefore under-count exactly the "heard a lot"
   stations the heatmap is meant to highlight.
2. **Digipeater attribution needs the whole used path per packet**, remapped to
   the digi's own coordinates — a query the station roster endpoint is not
   shaped to answer.

So this design adds (a) a persisted per-packet reception record, (b) a
`GET /api/heatmap` endpoint that aggregates those records into weighted points,
and (c) a MapLibre heatmap layer on the Live Map.

## Data model — reception events

APRS RF packet volume at a single station is modest (thousands to tens of
thousands per day), so an append-only per-packet table is affordable and is the
most flexible basis for arbitrary time windows (15 min … 7 days) and exact
counts.

New table `rx_events` in the history DB (`pkg/historydb`):

| column        | type    | meaning                                                        |
|---------------|---------|----------------------------------------------------------------|
| `id`          | INTEGER | PK                                                             |
| `timestamp`   | DATETIME| reception time (UTC)                                          |
| `attr_key`    | TEXT    | attributed transmitter key: origin `stn:CALL` for direct, `stn:DIGI` for digipeated |
| `hops`        | INTEGER | H-bit hop count of the received copy (0 = direct)             |
| `lat`,`lon`   | REAL    | attributed coordinates **if known at ingest**, else null      |
| `has_pos`     | INTEGER | whether `lat`/`lon` were resolvable at ingest                 |

Index on `(timestamp)` for window scans; the aggregation groups by rounded
coordinate.

**Why store `attr_key` and resolve position at query time when `has_pos` is
false:** a digipeater's position may be unknown at the moment we hear it repeat
someone (we have not yet decoded the digi's own beacon). Storing the key lets
the query fill in coordinates later from the current station roster / positions,
so early events are not permanently unlocatable. Events whose `attr_key`
position is still unknown at query time are counted as **unlocatable** and
reported as an aggregate (see endpoint response) rather than silently dropped.

**Ingest.** Every `direction == RX` packet is recorded on the existing packet
receive path (the same fan-out that already feeds the station cache and history
DB — `pkg/app` RX wiring / the `packetlog.Hook` seam). The attribution rule:

- `hops == 0` → `attr_key = stn:<source>`; coordinates = the packet's own
  position if it carried one, else resolved from the source station's known
  position.
- `hops > 0` → `attr_key = stn:<Via>` where `Via` is the last H-bit digipeater
  (`pkg/stationcache/extract.go deriveVia`); coordinates resolved from that
  digi's known position (same `digiPos` lookup that
  `pkg/webapi/stations.go resolvePathPositions` already uses).

Positionless packet types (messages, status, telemetry) still generate a
reception event, attributed to the source/digi's known location — the packet
counts as "heard from there" even though the frame itself carried no lat/lon.
If neither a packet position nor a known station position exists, the event is
stored with `has_pos = 0` and resolved later or counted as unlocatable.

**Retention.** Prune `rx_events` older than the longest supported interval plus
a margin (7 days → prune at, say, 8 days), on the same schedule the history DB
already prunes. Bounded, self-cleaning.

## Endpoint — `GET /api/heatmap`

Registered in `pkg/webapi` alongside the existing map endpoints, backed by the
history DB.

Request:

```
GET /api/heatmap?bbox=sw_lat,sw_lon,ne_lat,ne_lon&timerange=<seconds>
```

- `bbox` (required): same format and parser as `/api/stations`
  (`parseBBox`).
- `timerange` (seconds, default 3600): lookback window; reuses the Live Map's
  existing interval dropdown value.

Aggregation: select `rx_events` in `[now-timerange, now]`, resolve any
`has_pos = 0` rows against the current station positions, drop events outside
`bbox`, bucket by coordinate (rounded to a grid fine enough for the map yet
coarse enough to merge repeat hits from one fixed station — e.g. ~4–5 decimal
places, revisited during implementation), and sum counts per bucket.

Response (GeoJSON `FeatureCollection`, so the frontend feeds it straight into a
MapLibre source):

```json
{
  "type": "FeatureCollection",
  "features": [
    { "type": "Feature",
      "geometry": { "type": "Point", "coordinates": [lon, lat] },
      "properties": { "count": 1234 } }
  ],
  "properties": { "max_count": 1234, "unlocatable": 17 }
}
```

- `count` — total RX packets attributed to that location in the window.
- `max_count` — the largest bucket count, so the client can normalize
  `heatmap-weight` regardless of absolute volume.
- `unlocatable` — packets heard in the window whose attributed transmitter had
  no known position (reported, not hidden).

## Frontend — heatmap layer

Mirrors the existing layer modules (`mount*Layer(map, getData, opts)` →
`{ refresh, setVisible, setDataFilter, destroy }`) and the radar/fronts pattern
of re-adding the source+layer after a basemap `setStyle()`.

### `web/src/lib/map/sources/heatmap-source.js` (new)

Fetches `GET /api/heatmap` for the current `bbox` + `timerange`, returns the
GeoJSON plus `max_count`. Polls on the same cadence as the station poll (or a
slightly slower one — heat drifts slowly) and refetches when the interval
dropdown or the viewport changes.

### `web/src/lib/map/layers/direct-rx-heatmap.js` (new)

Mounts one MapLibre `heatmap` layer over a `geojson` source, inserted **below**
the station markers and trails so markers stay readable. Paint:

- `heatmap-weight`: `count / max_count` (normalized), interpolated so a
  single-packet location is faint and a high-count location saturates.
- `heatmap-intensity` / `heatmap-radius`: zoom-interpolated (larger radius when
  zoomed out) following MapLibre's standard heatmap idiom.
- `heatmap-color`: a transparent-to-hot ramp; exact palette settled during
  implementation to fit the basemap.
- `heatmap-opacity`: driven by the opacity control (below).

Exposes `refresh(geojson, maxCount)`, `setVisible(v)`, `setOpacity(v)`,
`destroy()`. `ensure()` re-adds source+layer on style reloads, like `radar.js`.

### UI (`LiveMapV2.svelte` + `layer-toggles-core.js`)

- New toggle `directRxHeatmap` in `LAYER_TOGGLES_DEFAULTS`, **default off**,
  rendered in the APRS section of the layer card / bottom sheet.
- An **opacity slider** next to the toggle, mirroring the radar opacity control,
  persisted with the other toggles under `gw_map_layer_toggles`.
- Reuses the existing `timerangeSec` interval dropdown — no new interval control.
- The heatmap is a standalone visual layer; it does **not** change the existing
  `directRxOnly` / `rfOnly` marker filters.

## Testing

- **Go, ingest attribution** (`pkg/historydb` / ingest wiring): direct packet →
  event at source; digipeated packet → event at `Via` digi; IS / gated packet →
  no event; positionless-but-known-station packet → event at known location;
  unknown position → `has_pos = 0`.
- **Go, endpoint** (`pkg/webapi`): time-window boundary inclusion, bbox
  filtering, per-bucket count summation, `max_count`, `unlocatable` accounting,
  late position resolution for previously-`has_pos = 0` events. Follow the
  existing `handlers_test.go` table style.
- **Go, retention:** events past the retention horizon are pruned.
- **JS, pure cores** (`node --test`): source URL building from bbox/timerange;
  weight normalization from `count`/`max_count`; toggle persistence merge (the
  new key defaults off and survives round-trips), matching the existing
  `layer-toggles-core` test approach.

## Out of scope (possible follow-ups)

- Signal-strength weighting (RSSI / dBFS from the packet log's `AudioLevel`).
  The requester asked for **packet-count** weighting; strength weighting can
  layer on later by carrying a level field into `rx_events`.
- Historical playback / time-scrubbing of the heatmap.
- A separate "TX heard-by-others" or coverage-prediction layer.

## Wiki

On completion, add a Live Map layers page entry (or extend the existing one)
pointing future agents at `layers/direct-rx-heatmap.js`, `sources/heatmap-source.js`,
the `/api/heatmap` handler, and the `rx_events` table.
