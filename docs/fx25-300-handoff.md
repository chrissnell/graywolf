# Experimental 300-baud FX.25 agent handoff

## Scope and agreed decisions

- Branch: `feature/fx25-afsk300`, based on `feature/fx25-afsk1200`
  (`c2f6294d18b04ed41bee4690eaa768208faeb0a9`, upstream PR #687).
- Open a **draft in samwathegreat/graywolf**, not an upstream PR or release.
- Native AFSK 300 baud / 1600 and 1800 Hz: automatic experimental FX.25 RX;
  ordinary AX.25 remains supported. FX.25 TX is explicitly opt-in, OFF by default.
- Require an informed UI confirmation for experimental 300-baud TX. Warn about
  correlation tag, parity and padding airtime; quantify from actual framing.
  RX itself adds no airtime, but existing forwarding rules may transmit frames.
- Tester-only until real-world acceptance. No equipment is available to the
  operator; do not claim hardware, RF, PTT or channel-performance acceptance.
- Use generic callsigns in examples. Do not change running stations, combined
  release manifests, or the established 1200-baud feature branch.
- Proceed autonomously; stop only for an actual blocker or required decision.

## Work log

### Initial investigation

- Existing codec is independent of baud; production AFSK RX, TX, Go validation,
  web validation and Android dispatch explicitly restrict FX.25 to 1200 baud.
- Existing independent interop harness pins Dire Wolf
  `eda1383f5fa9d8ba3cb27f99db1d2c79494404c9`. Extend its offline tooling to 300 baud
  while retaining the 1200-baud checks; all waveform operations must stay offline.
- Initial reference lookup: FX.25 draft 0.01.06 (2006), Dire Wolf FX.25 source,
  `gen_packets` and FX.25 application note. Research and exact airtime results
  will be recorded separately; no unmeasured sensitivity or universal multiplier.

### Implementation checkpoint

- Shared Rust `fx25::supports_afsk`: 1200/1200/2200 and 300/1600/1800 only.
  Used by TX, RX and Android mapping. Go and web validation match this scope.
- Automatic RX, unchanged `fx25_encode` opt-in/default false, no new schema.
  One-time migration 31 clears legacy inert 300-baud TX flags (not 1200);
  explicit new opt-ins persist across restart. Channel editor confirmation is UI-only before saving a new 300 TX opt-in,
  changed parity or output device. Pure decision helper has regression tests.
- Extended 300-baud duplicate retention to bounded FEC latency. Deterministic
  clock-error stress exposed duplicate deliveries at +/-1%; nominal timing
  backdating was replaced by actual sampled-bit timestamps during 300-baud
  blocks (maximum 2040 timestamps per active slicer). 1200 keeps its old path.
- Independent interop passes at both rates: 33 wire vectors, 33 Graywolf→DW
  audio, four production modes, four oversized fallbacks, 33 DW→Graywolf audio,
  eleven damaged independent waveforms with expected correction metadata.
- Go configstore/DTO/web API race tests passed. Web 552 tests/build passed.
  Final focused results below supersede these intermediate checks.
- Broad Rust run: 226 passed; five known environment failures (audio output,
  three Unix sockets, absent reference FLAC), matching the inherited record.
- User-local toolchains/dependencies under `/tmp/graywolf-fx25-tools`;
  `source /tmp/graywolf-fx25-tools/env.sh`. Rust 1.100.0, Go 1.26.2.
  Debian dev headers/tools unpacked without root; no system changes or station I/O.
  Logs `/tmp/fx25-300-*.log`; these paths are transient and not prerequisites on CI.

## Current status / next steps

Implementation, documentation and local checks are complete. This is a
fork-only tester draft. Find the PR by
head `feature/fx25-afsk300`; its base is `feature/fx25-afsk1200` to keep this diff
separate from the inherited #687 implementation. Do not merge or submit upstream
until independent 300-baud on-air acceptance is reviewed.

- Research/operator guide: `docs/fx25-300.md`, with primary sources, exact airtime
  table, use-case caveats, scope and acceptance checklist.
- Reproducible synthetic observations: `docs/fx25-300-results.json`.
- Main paths: `fx25::supports_afsk`, `demod_afsk.rs`, `demod_afsk_multi.rs`,
  `tx/mod.rs`, `android/{mod,config_state}.rs`, and shared UI/config validation.
- `android::config_state::sync_rx_demod` now applies a coherent configured tuple
  between chunks instead of leaving the Android DSP fixed at 1200. Host-stub
  tests cover the production helper in both directions of reconfiguration.
- One-time migration **31** clears legacy inert `fx25_encode` on existing
  300-baud channels; it preserves 1200-baud flags and new 300 opt-ins on restart.
  When retargeting upstream, check that no intervening migration took number 31.
- Local focused results: Rust FX.25 **31**, Android host-stub **32**, IL2P **30**;
  native check/Clippy, Android aarch64 library check, both-rate independent
  interop and all deterministic stress assertions passed. Web **552**, build,
  API type/drift, OpenAPI generation drift and doc-ID lint passed.
- Final full Go race suite: all packages passed except
  `TestSupervisor_ConnectOnly_DialsExistingSocket` in modembridge (Unix sockets
  prohibited by the executor). Includes migration 31 and HTTP opt-in tests.
  Go vet, generated OpenAPI/client drift and doc-ID checks passed.
- Broad Rust: **226 passed, 5 environment failures**: unavailable sound output,
  three blocked Unix sockets, absent reference FLAC. Same failures appear in
  the inherited acceptance record; no tests disabled. No on-air acceptance.
- The draft adds a Linux Go/web/docs validation job as well as
  Linux/Windows/macOS focused and Android checks, plus independent two-rate
  interop and stress, are configured in `.github/workflows/fx25.yml`.
  Configured/queued jobs are not evidence of a pass; inspect actual results.
- Still unperformed: interactive confirmation dialog/visual testing; physical
  Android/radio/PTT/HF testing; long-term/shared-channel performance measurement.
- Transient environment: source `/tmp/graywolf-fx25-tools/env.sh` for local
  toolchains, protoc and unpacked ALSA dev files. Logs `/tmp/fx25-300-*.log`.
  Recreate these from official sources if the executor is reset. Reference DW:
  `/tmp/graywolf-fx25-tools/direwolf` at the pinned revision above. Never start
  its normal radio application; use only offline `atest`/`gen_packets`.
- Commands: use those in `docs/fx25-300.md`, then run
  `scripts/fx25/stress300.py`, Go race checks, web tests/build/API check and
  `make docs-check docs-lint api-client-check`.
- No combined release, installed-station update or upstream submission is
  authorized as part of this task. Use generic example callsigns.

## Informational airtime documentation follow-up

- The 1200-baud base PR #687 now includes its own Auto-mode airtime table in
  `docs/fx25.md`; that base documentation has been merged into this branch
  to retain the PR dependency cleanly (base commit `f66fe3a86f2ab85650d08ee020da06f4dc698892`).
- This guide includes the corresponding 300-baud table and explicitly separates
  total keyed time from packet-only percentage overhead. Identical frame/mode
  comparisons have equal packet-only percentages; added seconds are four times
  longer at 300 baud. Fixed delay/tail explains different total-time percentages.
- Eight fresh sample-count comparisons and these identities were verified with
  the offline production builder. Exact values/assumptions were appended to
  `docs/fx25-300-results.json`. This follow-up changes documentation only.
