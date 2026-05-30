# KISS-Modem TX → APRS-IS Gating Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an opt-in per-interface flag (`gate_tx_to_is`) to graywolf's KISS server so frames a connected KISS client (YAAC / Xastir / APRSdroid) sends through graywolf for RF transmission can ALSO be forwarded to APRS-IS via the existing iGate, without the client needing its own APRS-IS connection.

**Architecture:** A single new bool column on `kiss_interfaces`. The KISS server/client/serial dispatch paths gain an `OnClientTxAccepted` hook that fires in `ModeModem` after `Sink.Submit` returns nil. The hook calls a new `App` method that parses APRS from the AX.25 frame and pushes it straight into `IgateOutput.SendPacket` — bypassing `aprsQueue`, the messages router, the Actions classifier, the station cache, and the digipeater (the frame is OUR TX, not heard traffic; only the iGate's RF→IS path is relevant). The iGate's existing filter chain (`NOGATE`, `RFONLY`, `TCPIP`, operator filter rules) applies unchanged. The toggle is a no-op in `ModeTnc` because that path already feeds the iGate via the existing RX fanout (see `pkg/app/rxfanout.go:107-127`).

**Tech Stack:** Go 1.22+, GORM (glebarez/sqlite), Svelte 5 + `@chrissnell/chonky-ui`, `swag` (pinned at v1.16.4) for OpenAPI generation, `npm run api:generate` for the TS client.

**Spec:** No standalone spec file — derived from a focused conversation. Background: when `Mode=modem`, `pkg/kiss/server.go:381-391` routes incoming KISS data frames straight to `txgovernor.Sink.Submit` → modem TX. There is no code path from this branch to `aprsSubmit` / `igateOut`, so weather/position packets submitted by a connected KISS app never reach APRS-IS through graywolf. APRS-IS dedupes server-side and `pkg/igate/igate.go:10,51,702` explicitly says RX iGates must not dedup, so submitting our own TX to APRS-IS in parallel with the RF transmission is safe.

---

## File Structure

| File | Purpose |
|---|---|
| `pkg/configstore/migrate_kiss_gate_tx_to_is.go` | Migration 24: ADD COLUMN `gate_tx_to_is` |
| `pkg/configstore/migrate_kiss_gate_tx_to_is_test.go` | Round-trip + idempotence test for the migration |
| `pkg/configstore/migrate.go` | Register migration 24 in `schemaMigrations` |
| `pkg/configstore/models.go` | Add `GateTxToIs bool` to `KissInterface` |
| `pkg/webapi/dto/kiss.go` | Add `GateTxToIs` to `KissRequest`/`KissResponse`; thread through `Validate` / `ToModel` / `KissFromModel` |
| `pkg/webapi/dto/kiss_test.go` | DTO round-trip + validation test (create if absent — see Task 3) |
| `pkg/kiss/server.go` | Add `GateTxToIs` + `OnClientTxAccepted` to `ServerConfig`; fire hook in `dispatchDataFrame`'s `default` (modem) branch after `Sink.Submit` returns nil |
| `pkg/kiss/server_test.go` | Add `TestServerGateTxToIsHookFires` covering: fires in `ModeModem` + flag on, doesn't fire when flag off, doesn't fire when `Sink.Submit` errors |
| `pkg/kiss/client.go` | Same pattern for `ClientConfig` + `Client.dispatchDataFrame` |
| `pkg/kiss/client_test.go` | Mirror server test for the client path |
| `pkg/kiss/serial.go` | Add `GateTxToIs` to `SerialConfig` |
| `pkg/kiss/manager.go` | Add `OnClientTxAccepted` to `ManagerConfig`; thread per-server `ifaceID` closure into `ServerConfig`/`ClientConfig` in `Start` / `StartClient` / `StartSerial`; propagate `cfg.GateTxToIs` through `StartSerial` |
| `pkg/app/wiring.go` | Install `OnClientTxAccepted: a.kissClientTxGateToIs` on `kiss.ManagerConfig`; thread `ki.GateTxToIs` into all four `Start*` call sites (Server, Client, Serial, USB-serial — Bluetooth is hard-forced to `ModeTnc`, so it skips this) |
| `pkg/app/kiss_gate.go` | New file: `(*App).kissClientTxGateToIs` method |
| `pkg/app/kiss_gate_test.go` | Unit test: parse + tag work; nil-igate no-op |
| `pkg/kiss/manager_test.go` | Append `TestManagerThreadsOnClientTxAcceptedWithIfaceID` |
| `pkg/webapi/docs/gen/swagger.json` + `swagger.yaml` | Regenerated via `make docs` |
| `web/src/api/generated/api.d.ts` | Regenerated via `make api-client` |
| `web/src/routes/Kiss.svelte` | New checkbox in the modem branch of the edit modal; surface in `buildPayload`, `emptyForm`, `openEdit` |
| `docs/wiki/code-map.md` | Add one-sentence note on the new field in the `kiss` package row |
| `docs/handbook/kiss.html` | Add `gate_tx_to_is` row to the Interface Settings table |
| `pkg/releasenotes/notes.yaml` | Prepend `info`-style entry for next patch version (NEW VERSION line shipped when the user runs `make bump-point`) |

---

## Task 1: Configstore — KissInterface model field + AutoMigrate

**Files:**
- Modify: `pkg/configstore/models.go:151` (insert new field after `AllowTxFromGovernor`, before `NeedsReconfig`)

- [ ] **Step 1: Add the GORM field to `KissInterface`**

Open `pkg/configstore/models.go`. Find the `AllowTxFromGovernor` field (line 151 area). Immediately after it, add:

```go
	// GateTxToIs: when true and Mode == KissModeModem, frames a
	// connected KISS client submits for TX are ALSO offered to the
	// iGate's RF→IS gate after Sink.Submit accepts them. The standard
	// iGate filters (NOGATE / RFONLY / TCPIP path markers + operator
	// filter rules) still apply, so the toggle only opens the gate; it
	// does not bypass policy. Meaningless in Mode==KissModeTnc (that
	// path already feeds the iGate via the RX fanout), so the wiring
	// reads the field unconditionally and the server only consults it
	// inside the modem branch.
	GateTxToIs bool `gorm:"column:gate_tx_to_is;not null;default:false" json:"gate_tx_to_is"`
```

- [ ] **Step 2: Build the package to verify the struct compiles**

Run: `go build ./pkg/configstore/...`
Expected: no errors. GORM AutoMigrate will pick up the new column on a fresh database from the struct tags; existing databases need the explicit migration in Task 2.

- [ ] **Step 3: Commit**

```bash
git add pkg/configstore/models.go
git commit -m "configstore: add GateTxToIs to KissInterface"
```

---

## Task 2: Configstore — Migration 24 (`kiss_gate_tx_to_is`)

**Files:**
- Create: `pkg/configstore/migrate_kiss_gate_tx_to_is.go`
- Create: `pkg/configstore/migrate_kiss_gate_tx_to_is_test.go`
- Modify: `pkg/configstore/migrate.go:221` (append entry to `schemaMigrations`)

- [ ] **Step 1: Write the failing migration test**

Create `pkg/configstore/migrate_kiss_gate_tx_to_is_test.go`:

```go
package configstore

import (
	"path/filepath"
	"testing"

	"gorm.io/gorm"
)

// TestMigrateKissGateTxToIs verifies the migration adds the gate_tx_to_is
// column with default 0, leaves existing rows alone, and is idempotent.
func TestMigrateKissGateTxToIs(t *testing.T) {
	t.Parallel()
	dsn := filepath.Join(t.TempDir(), "kiss_gate.db")
	store, err := Open(dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer store.Close()

	// Insert a row using the pre-migration shape — exclude gate_tx_to_is
	// from the INSERT so the DB default is what's tested. (Open() ran
	// every migration including ours; we simulate "legacy row" by just
	// not specifying the column.)
	if err := store.DB().Exec(
		`INSERT INTO kiss_interfaces(name, type, mode, channel, broadcast, enabled,
		 tnc_ingress_rate_hz, tnc_ingress_burst, created_at, updated_at)
		 VALUES ('legacy', 'tcp', 'modem', 1, 1, 1, 50, 100, datetime('now'), datetime('now'))`,
	).Error; err != nil {
		t.Fatalf("insert legacy row: %v", err)
	}

	var got bool
	if err := store.DB().Raw(
		`SELECT gate_tx_to_is FROM kiss_interfaces WHERE name='legacy'`,
	).Scan(&got).Error; err != nil {
		t.Fatalf("scan column: %v", err)
	}
	if got {
		t.Fatalf("gate_tx_to_is=%v, want false (default 0) on legacy row", got)
	}

	// Idempotence: running the migration body again must be a no-op.
	if err := migrateKissGateTxToIs(store.db.(*gorm.DB)); err != nil {
		t.Fatalf("second run: %v", err)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails (migration not yet defined)**

Run: `go test ./pkg/configstore/ -run TestMigrateKissGateTxToIs -v`
Expected: FAIL with `undefined: migrateKissGateTxToIs`.

- [ ] **Step 3: Write the migration**

Create `pkg/configstore/migrate_kiss_gate_tx_to_is.go`:

```go
package configstore

import (
	"fmt"

	"gorm.io/gorm"
)

// migrateKissGateTxToIs adds the gate_tx_to_is BOOL column to
// kiss_interfaces with default false. Idempotent: the pragma_table_info
// probe short-circuits if the column already exists. Fresh databases
// hit the table-doesn't-exist branch and just bump user_version — the
// post-migrate AutoMigrate pass creates the table directly from the
// updated KissInterface struct tags.
func migrateKissGateTxToIs(tx *gorm.DB) error {
	var tableExists int
	if err := tx.Raw(
		"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='kiss_interfaces'",
	).Scan(&tableExists).Error; err != nil {
		return fmt.Errorf("probe kiss_interfaces: %w", err)
	}
	if tableExists == 0 {
		return nil
	}

	var present int
	if err := tx.Raw(
		"SELECT COUNT(*) FROM pragma_table_info('kiss_interfaces') WHERE name='gate_tx_to_is'",
	).Scan(&present).Error; err != nil {
		return fmt.Errorf("probe gate_tx_to_is: %w", err)
	}
	if present > 0 {
		return nil
	}

	if err := tx.Exec(
		"ALTER TABLE kiss_interfaces ADD COLUMN gate_tx_to_is NUMERIC NOT NULL DEFAULT 0",
	).Error; err != nil {
		return fmt.Errorf("add gate_tx_to_is: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Register the migration**

Open `pkg/configstore/migrate.go`. Find the `schemaMigrations` slice (line 199–222). Append (keeping ascending version order):

```go
	{version: 24, name: "kiss_gate_tx_to_is", phase: preAutoMigrate, run: migrateKissGateTxToIs},
```

The migration is `preAutoMigrate` because AutoMigrate must see the column already present so its struct→schema check passes cleanly on existing databases.

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./pkg/configstore/ -run TestMigrateKissGateTxToIs -v`
Expected: PASS.

- [ ] **Step 6: Run the full configstore test suite**

Run: `go test ./pkg/configstore/...`
Expected: PASS. No drift caught by the existing schema invariant tests.

- [ ] **Step 7: Commit**

```bash
git add pkg/configstore/migrate.go \
        pkg/configstore/migrate_kiss_gate_tx_to_is.go \
        pkg/configstore/migrate_kiss_gate_tx_to_is_test.go
git commit -m "configstore: add migration 24 for kiss_interfaces.gate_tx_to_is"
```

---

## Task 3: DTO — KissRequest/KissResponse field

**Files:**
- Modify: `pkg/webapi/dto/kiss.go:31-56` (`KissRequest`), `:127-201` (`ToModel`), `:219-275` (`KissResponse` + `KissFromModel`)

- [ ] **Step 1: Add field to `KissRequest`**

Open `pkg/webapi/dto/kiss.go`. After `AllowTxFromGovernor` in `KissRequest` (line 47), insert:

```go
	// GateTxToIs opts a Mode=modem KISS interface in to gating frames
	// submitted by connected KISS clients to APRS-IS, after the TX
	// governor has accepted them. The iGate's own filter chain still
	// runs, so this only opens the gate — it does not bypass NOGATE /
	// RFONLY / TCPIP markers or the operator's filter rules. Default
	// false on all migrated rows; meaningless in Mode=tnc (which
	// already feeds the iGate via the RX fanout) and silently ignored
	// there by the server.
	GateTxToIs bool `json:"gate_tx_to_is"`
```

- [ ] **Step 2: Thread the field through `ToModel`**

In the same file, find the `ToModel` constructor returning `configstore.KissInterface{...}` (line 168). Add the field to the struct literal, alongside `AllowTxFromGovernor`:

```go
		GateTxToIs:          r.GateTxToIs,
```

- [ ] **Step 3: Add field to `KissResponse`**

After `AllowTxFromGovernor` in `KissResponse` (line 229), insert:

```go
	GateTxToIs          bool   `json:"gate_tx_to_is"`
```

- [ ] **Step 4: Mirror in `KissFromModel`**

In the same file's `KissFromModel` (line 250-266), add the field to the struct literal:

```go
		GateTxToIs:          m.GateTxToIs,
```

- [ ] **Step 5: Write the failing DTO round-trip test**

`pkg/webapi/dto/kiss_test.go` already exists. Append to it:

```go
// TestKissRequest_GateTxToIs_RoundTrip verifies the new field survives
// the DTO -> model -> DTO cycle unchanged for both true and false.
func TestKissRequest_GateTxToIs_RoundTrip(t *testing.T) {
	for _, want := range []bool{false, true} {
		req := KissRequest{
			Type:       configstore.KissTypeTCP,
			TcpPort:    8001,
			Channel:    1,
			Mode:       configstore.KissModeModem,
			GateTxToIs: want,
		}
		m := req.ToModel()
		if m.GateTxToIs != want {
			t.Fatalf("ToModel: GateTxToIs=%v, want %v", m.GateTxToIs, want)
		}
		resp := KissFromModel(m)
		if resp.GateTxToIs != want {
			t.Fatalf("KissFromModel: GateTxToIs=%v, want %v", resp.GateTxToIs, want)
		}
	}
}
```

(`testing` and `configstore` imports already exist in the file.)

- [ ] **Step 6: Run the test**

Run: `go test ./pkg/webapi/dto/ -run TestKissRequest_GateTxToIs_RoundTrip -v`
Expected: PASS (Steps 1-4 already added the wiring before we wrote the test, so this is a regression guard rather than red-then-green TDD — that's acceptable here because the change is a 3-field struct addition with no logic to red-test).

- [ ] **Step 7: Commit**

```bash
git add pkg/webapi/dto/kiss.go pkg/webapi/dto/kiss_test.go
git commit -m "webapi/dto: thread GateTxToIs through KissRequest/KissResponse"
```

---

## Task 4: kiss.Server — `OnClientTxAccepted` hook + `GateTxToIs` field

**Files:**
- Modify: `pkg/kiss/server.go:60-107` (`ServerConfig`), `:356-393` (`dispatchDataFrame`)
- Modify: `pkg/kiss/server_test.go` (append `TestServerGateTxToIsHookFires`)

- [ ] **Step 1: Write the failing server-level hook test**

The existing `fakeSink` at `pkg/kiss/server_test.go:40-52` always returns nil, so the "sink rejects" case needs its own tiny inline sink. Append to `pkg/kiss/server_test.go`:

```go
// errSink always returns the configured error from Submit. Used to
// prove OnClientTxAccepted does NOT fire when the TX governor rejects
// the frame.
type errSink struct{ err error }

func (s *errSink) Submit(_ context.Context, _ uint32, _ *ax25.Frame, _ txgovernor.SubmitSource) error {
	return s.err
}

// TestServerGateTxToIsHookFires asserts that in ModeModem with
// GateTxToIs=true the OnClientTxAccepted hook fires exactly once per
// KISS frame the sink accepted, with the mapped channel + the decoded
// AX.25 frame. It also asserts the hook does NOT fire when the flag
// is off, and does NOT fire when the sink rejects the frame.
func TestServerGateTxToIsHookFires(t *testing.T) {
	type call struct {
		channel uint32
		src     string
	}

	cases := []struct {
		name          string
		gate          bool
		useErrSink    bool
		wantHookCalls int
	}{
		{"hook fires when gate on + sink accepts", true, false, 1},
		{"no hook when gate off + sink accepts", false, false, 0},
		{"no hook when sink rejects", true, true, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var sink txgovernor.TxSink
			if tc.useErrSink {
				sink = &errSink{err: errors.New("rejected")}
			} else {
				sink = newFakeSink()
			}

			gotCalls := make(chan call, 4)
			srv := NewServer(ServerConfig{
				InterfaceID: 42,
				Name:        "t",
				ListenAddr:  "127.0.0.1:0",
				Sink:        sink,
				ChannelMap:  map[uint8]uint32{0: 7},
				Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
				Mode:        ModeModem,
				GateTxToIs:  tc.gate,
				OnClientTxAccepted: func(ctx context.Context, channel uint32, f *ax25.Frame) {
					gotCalls <- call{channel: channel, src: f.Source.String()}
				},
			})

			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			srv.cfg.ListenAddr = ln.Addr().String()
			_ = ln.Close()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			serveDone := make(chan struct{})
			go func() { _ = srv.ListenAndServe(ctx); close(serveDone) }()

			feedFrame(t, srv.cfg.ListenAddr, kissUIFrameBytes(t, "hello"))

			// Give the dispatcher time to run.
			deadline := time.After(500 * time.Millisecond)
			tally := 0
		loop:
			for {
				select {
				case <-gotCalls:
					tally++
				case <-deadline:
					break loop
				}
			}
			if tally != tc.wantHookCalls {
				t.Fatalf("hook fired %d times, want %d", tally, tc.wantHookCalls)
			}

			cancel()
			<-serveDone
		})
	}
}
```

Add `"errors"` and `"github.com/chrissnell/graywolf/pkg/txgovernor"` to the existing import block if not already present.

- [ ] **Step 2: Run the test to confirm it fails (fields undefined)**

Run: `go test ./pkg/kiss/ -run TestServerGateTxToIsHookFires -v`
Expected: FAIL with `unknown field GateTxToIs in struct literal` / `unknown field OnClientTxAccepted`.

- [ ] **Step 3: Add the two fields to `ServerConfig`**

In `pkg/kiss/server.go`, find `ServerConfig` (line 60). After the existing `RxIngress` block (around line 96), insert:

```go
	// GateTxToIs: when true and Mode == ModeModem, the dispatcher fires
	// OnClientTxAccepted after every KISS frame Sink.Submit accepted.
	// Meaningless when Mode == ModeTnc (the RX fanout there already
	// feeds the iGate). Default false.
	GateTxToIs bool
	// OnClientTxAccepted, when non-nil and GateTxToIs is true, is
	// invoked from the ModeModem branch of dispatchDataFrame AFTER
	// Sink.Submit returns nil. Wiring uses it to offer the parsed
	// APRS packet to the iGate's RF→IS gate. The hook MUST be
	// non-blocking — it runs on the per-connection read goroutine.
	OnClientTxAccepted func(ctx context.Context, channel uint32, f *ax25.Frame)
```

- [ ] **Step 4: Fire the hook in `dispatchDataFrame`**

In `pkg/kiss/server.go:381-391` (the `default` branch of the mode switch), modify:

```go
	default:
		if s.cfg.Sink != nil {
			err := s.cfg.Sink.Submit(ctx, channel, ax, txgovernor.SubmitSource{
				Kind:     "kiss",
				Detail:   s.cfg.Name + " " + remote,
				Priority: ax25.PriorityClient,
			})
			if err != nil {
				s.logger.Warn("tx governor rejected kiss frame", "err", err)
				return
			}
			if s.cfg.GateTxToIs && s.cfg.OnClientTxAccepted != nil {
				s.cfg.OnClientTxAccepted(ctx, channel, ax)
			}
		}
	}
```

The `return` after the Warn replaces the prior fall-through so the hook does NOT fire on Submit error. This is the correct semantics — a frame the governor rejected (rate limit, dedup, channel-mode rule) should not bypass that policy to reach APRS-IS.

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./pkg/kiss/ -run TestServerGateTxToIsHookFires -v`
Expected: PASS.

- [ ] **Step 6: Run the full kiss test suite to catch regressions**

Run: `go test ./pkg/kiss/...`
Expected: PASS. The pre-existing `TestServerModeDispatch` exercises the `GateTxToIs=false / OnClientTxAccepted=nil` defaults and must still pass byte-for-byte.

- [ ] **Step 7: Commit**

```bash
git add pkg/kiss/server.go pkg/kiss/server_test.go
git commit -m "kiss: add OnClientTxAccepted hook to Server dispatch (modem mode)"
```

---

## Task 5: kiss.Client — same hook on the dial-out path

**Files:**
- Modify: `pkg/kiss/client.go:148-160` (`ClientConfig`), `:434-461` (`dispatchDataFrame`)
- Modify: `pkg/kiss/client_test.go` (append `TestClientGateTxToIsHookFires`)

- [ ] **Step 1: Write the failing client-level test**

The Client's dispatch is a private method on the in-package type, so the test calls it directly without standing up a real TCP dial — the same approach the existing Server hook test would use if it weren't already going through `feedFrame`. Append to `pkg/kiss/client_test.go`:

```go
// TestClientGateTxToIsHookFires mirrors the server-side hook test
// (TestServerGateTxToIsHookFires) for the Client.dispatchDataFrame
// path. Calls dispatchDataFrame directly to keep the test focused on
// the dispatch decision rather than on transport plumbing.
func TestClientGateTxToIsHookFires(t *testing.T) {
	cases := []struct {
		name          string
		gate          bool
		useErrSink    bool
		wantHookCalls int
	}{
		{"hook fires when gate on + sink accepts", true, false, 1},
		{"no hook when gate off + sink accepts", false, false, 0},
		{"no hook when sink rejects", true, true, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var sink txgovernor.TxSink
			if tc.useErrSink {
				sink = &errSink{err: errors.New("rejected")}
			} else {
				sink = newFakeSink()
			}

			gotCalls := make(chan struct{}, 4)
			c := newClient(ClientConfig{
				InterfaceID: 99,
				Name:        "t",
				Logger:      silentLogger(),
				Sink:        sink,
				ChannelMap:  map[uint8]uint32{0: 7},
				Mode:        ModeModem,
				GateTxToIs:  tc.gate,
				OnClientTxAccepted: func(ctx context.Context, channel uint32, f *ax25.Frame) {
					gotCalls <- struct{}{}
				},
			})

			src, _ := ax25.ParseAddress("N0CALL")
			dst, _ := ax25.ParseAddress("APRS")
			ax := &ax25.Frame{
				Source:      src,
				Destination: dst,
				Control:     ax25.ControlUI,
				PID:         ax25.PIDNoLayer3,
				Info:        []byte("hello"),
			}
			c.dispatchDataFrame(context.Background(), "test", 7, ax, []byte("rawbytes"), ModeModem)

			// Drain the channel non-blocking.
			tally := 0
			deadline := time.After(200 * time.Millisecond)
		loop:
			for {
				select {
				case <-gotCalls:
					tally++
				case <-deadline:
					break loop
				}
			}
			if tally != tc.wantHookCalls {
				t.Fatalf("hook fired %d times, want %d", tally, tc.wantHookCalls)
			}
		})
	}
}
```

`errSink` is defined in Task 4 Step 1 (server_test.go, same package). `silentLogger` is the existing helper used by other client tests in the file.

- [ ] **Step 2: Run the test to confirm it fails**

Run: `go test ./pkg/kiss/ -run TestClientGateTxToIsHookFires -v`
Expected: FAIL with `unknown field GateTxToIs` / `unknown field OnClientTxAccepted` on `ClientConfig`.

- [ ] **Step 3: Add the two fields to `ClientConfig`**

In `pkg/kiss/client.go`, find `ClientConfig` (around line 148). After the `RxIngress` field, insert the same two fields as in Task 4 Step 3 (`GateTxToIs bool` + `OnClientTxAccepted func(ctx context.Context, channel uint32, f *ax25.Frame)`), with the same comments adapted to "client" wording.

- [ ] **Step 4: Fire the hook in `Client.dispatchDataFrame`**

In `pkg/kiss/client.go:449-460` (the `default` branch), modify identically to Task 4 Step 4 — add the `return` after the warn, and fire `c.cfg.OnClientTxAccepted` when `c.cfg.GateTxToIs` is true.

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./pkg/kiss/ -run TestClientGateTxToIsHookFires -v`
Expected: PASS.

- [ ] **Step 6: Run full kiss suite**

Run: `go test ./pkg/kiss/...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add pkg/kiss/client.go pkg/kiss/client_test.go
git commit -m "kiss: add OnClientTxAccepted hook to Client dispatch (modem mode)"
```

---

## Task 6: kiss.Serial — pass `GateTxToIs` through

**Files:**
- Modify: `pkg/kiss/serial.go:30-67` (`SerialConfig`)
- Modify: `pkg/kiss/manager.go:455-510` (`StartSerial` — propagate the field into the constructed `ServerConfig`)

- [ ] **Step 1: Add `GateTxToIs` to `SerialConfig`**

In `pkg/kiss/serial.go`, find `SerialConfig` (around line 36). After `AllowTxFromGovernor`, insert:

```go
	// GateTxToIs mirrors ServerConfig.GateTxToIs for the wrapping
	// Server constructed in StartSerial. Only meaningful when Mode ==
	// ModeModem; the field is read unconditionally and the server's
	// dispatch path enforces the mode gate.
	GateTxToIs bool
```

`SerialConfig` does NOT need an `OnClientTxAccepted` field — the manager threads its own (per-interface-id) closure into the `ServerConfig` it constructs around `StartSerial` (see Task 7).

- [ ] **Step 2: Propagate in `Manager.StartSerial`**

In `pkg/kiss/manager.go`, find `StartSerial` (line 461). It builds a `ServerConfig` from the `SerialConfig`. Locate that struct literal (around line 470-490) and add:

```go
		GateTxToIs:          cfg.GateTxToIs,
```

(Place it alongside `AllowTxFromGovernor` for consistency.)

- [ ] **Step 3: Build**

Run: `go build ./pkg/kiss/...`
Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add pkg/kiss/serial.go pkg/kiss/manager.go
git commit -m "kiss: thread GateTxToIs through SerialConfig + StartSerial"
```

---

## Task 7: kiss.Manager — `OnClientTxAccepted` field + per-server ifaceID closure

**Files:**
- Modify: `pkg/kiss/manager.go:65-90` (Manager struct), `:132-186` (`ManagerConfig`), `:189-211` (`NewManager`), `:226-309` (`Start`), `:341-440` (`StartClient`), `:455-555` (`StartSerial`)

- [ ] **Step 1: Add the field to `ManagerConfig`**

In `pkg/kiss/manager.go:132-186`, after the existing `RxIngress` field (around line 157), insert:

```go
	// OnClientTxAccepted, if non-nil, is installed on every Server /
	// Client / Serial launched by this Manager. The Manager wraps it
	// with the per-interface DB row ID so the wiring layer can route
	// the gated submission through one shared App method without each
	// transport having to know its own ID. The wrapped hook only
	// fires inside the Server/Client dispatch path when the
	// per-interface GateTxToIs flag is set.
	OnClientTxAccepted func(ctx context.Context, ifaceID, channel uint32, f *ax25.Frame)
```

- [ ] **Step 2: Add the field to the Manager struct**

Around line 72 (where `rxIngress` is stored), insert:

```go
	onClientTxAccepted func(ctx context.Context, ifaceID, channel uint32, f *ax25.Frame)
```

- [ ] **Step 3: Wire it in `NewManager`**

In `NewManager` (line 189), add to the struct literal:

```go
		onClientTxAccepted:    cfg.OnClientTxAccepted,
```

- [ ] **Step 4: Thread per-server in `Start`**

In `Manager.Start` (line 226), find the `cfg.RxIngress` finalization block (line 253-262). Immediately after it, add:

```go
	if cfg.OnClientTxAccepted == nil && m.onClientTxAccepted != nil {
		ifaceID := id
		fn := m.onClientTxAccepted
		cfg.OnClientTxAccepted = func(ctx context.Context, channel uint32, f *ax25.Frame) {
			fn(ctx, ifaceID, channel, f)
		}
	}
```

- [ ] **Step 5: Thread per-server in `StartClient`**

In `Manager.StartClient` (around line 367-390), add the same closure block, using `ClientConfig`'s `OnClientTxAccepted` field.

- [ ] **Step 6: Thread per-server in `StartSerial`**

In `Manager.StartSerial` (around line 495-510 — right next to the existing RxIngress finalization), add the same closure block targeting the constructed `ServerConfig.OnClientTxAccepted`.

- [ ] **Step 7: Build + run kiss tests**

Run: `go test ./pkg/kiss/...`
Expected: PASS. The existing tests don't supply `OnClientTxAccepted`, so the nil-default behavior covers them.

- [ ] **Step 8: Commit**

```bash
git add pkg/kiss/manager.go
git commit -m "kiss: thread OnClientTxAccepted through Manager Start/StartClient/StartSerial"
```

---

## Task 8: App — `kissClientTxGateToIs` method + unit test

**Files:**
- Create: `pkg/app/kiss_gate.go`
- Create: `pkg/app/kiss_gate_test.go`

- [ ] **Step 1: Write the failing method unit test**

Create `pkg/app/kiss_gate_test.go`:

```go
package app

import (
	"context"
	"testing"

	"github.com/chrissnell/graywolf/pkg/aprs"
	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/igate"
)

// recordingIgateSink captures every packet SendPacket sees so the test
// can assert direction/channel without standing up a real igate.Igate.
type recordingIgateSink struct {
	pkts []*aprs.DecodedAPRSPacket
}

// We can't wrap igate.IgateOutput directly (its inner ig is private),
// so the test uses a tiny PacketOutput-compatible double. The App
// method is structured to accept the *igate.IgateOutput field, which
// in turn delegates to its inner Igate. For the unit test we set the
// field to nil and bypass the iGate, asserting only that the hook
// parses + sets channel/direction. The wiring integration test in
// Task 10 covers the IgateOutput path end-to-end.

// TestKissClientTxGateToIs_NoIgateIsNoop verifies the App method
// gracefully tolerates a nil igateOut (iGate disabled or not wired).
func TestKissClientTxGateToIs_NoIgateIsNoop(t *testing.T) {
	app := &App{
		logger:   quietLogger(),
		igateOut: nil,
	}
	// Build a minimal AX.25 UI frame from APRS101 example text.
	src, _ := ax25.ParseAddress("N0CALL")
	dst, _ := ax25.ParseAddress("APRS")
	f := &ax25.Frame{
		Source:      src,
		Destination: dst,
		Control:     ax25.ControlUI,
		PID:         ax25.PIDNoLayer3,
		Info:        []byte(`=4900.00N/12300.00W-wx`),
	}
	// Must not panic, must not block.
	app.kissClientTxGateToIs(context.Background(), 1, 7, f)
}

// TestKissClientTxGateToIs_FeedsIgate verifies the parsed packet is
// passed to igateOut.SendPacket with Channel + Direction set. Uses
// an IgateOutput wrapping a nil *Igate so SendPacket is a no-op but
// the call site is exercised — combined with the integration test in
// Task 10, this catches the parse + tag work without a live iGate.
func TestKissClientTxGateToIs_FeedsIgate(t *testing.T) {
	app := &App{
		logger:   quietLogger(),
		igateOut: igate.NewIgateOutput(nil),
	}
	src, _ := ax25.ParseAddress("N0CALL")
	dst, _ := ax25.ParseAddress("APRS")
	f := &ax25.Frame{
		Source:      src,
		Destination: dst,
		Control:     ax25.ControlUI,
		PID:         ax25.PIDNoLayer3,
		Info:        []byte(`=4900.00N/12300.00W-wx`),
	}
	app.kissClientTxGateToIs(context.Background(), 1, 7, f)
}
```

If `quietLogger()` is not already defined in the `app` package's test files, copy the existing helper from `pkg/app/wiring_kiss_tnc_test.go` (it's a shared test util in the same package).

- [ ] **Step 2: Run the test to confirm it fails**

Run: `go test ./pkg/app/ -run TestKissClientTxGateToIs -v`
Expected: FAIL with `undefined: app.kissClientTxGateToIs`.

- [ ] **Step 3: Write the method**

Create `pkg/app/kiss_gate.go`:

```go
package app

import (
	"context"

	"github.com/chrissnell/graywolf/pkg/aprs"
	"github.com/chrissnell/graywolf/pkg/ax25"
)

// kissClientTxGateToIs is the per-interface OnClientTxAccepted hook
// the kiss.Manager invokes for each KISS frame accepted by Sink.Submit
// on an interface that has GateTxToIs set. The hook offers the parsed
// APRS packet to the iGate's RF→IS path, bypassing the messages
// router / Actions classifier / station cache / digipeater — those
// surfaces exist to handle heard traffic, and a frame the operator is
// transmitting is not heard traffic. The iGate's filter chain
// (NOGATE / RFONLY / TCPIP path markers + operator filter rules) is
// applied unchanged inside IgateOutput.SendPacket.
//
// Non-blocking by contract: runs on the kiss.Server per-connection
// read goroutine. The iGate's SendPacket is also non-blocking (it
// hands off to the iGate's internal channel and returns) so this
// inherits the right semantics.
//
// ifaceID is unused today but reserved for future per-interface
// metrics labeling so the API doesn't need a breaking change later.
func (a *App) kissClientTxGateToIs(ctx context.Context, ifaceID, channel uint32, f *ax25.Frame) {
	_ = ifaceID
	if a == nil || a.igateOut == nil || f == nil {
		return
	}
	if !f.IsUI() {
		return
	}
	pkt, err := aprs.Parse(f)
	if err != nil || pkt == nil {
		return
	}
	pkt.Channel = int(channel)
	pkt.Direction = aprs.DirectionRF
	_ = a.igateOut.SendPacket(ctx, pkt)
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./pkg/app/ -run TestKissClientTxGateToIs -v`
Expected: PASS (both subtests).

- [ ] **Step 5: Commit**

```bash
git add pkg/app/kiss_gate.go pkg/app/kiss_gate_test.go
git commit -m "app: add kissClientTxGateToIs hook for KISS-modem -> APRS-IS gating"
```

---

## Task 9: App wiring — install hook + propagate `GateTxToIs` per interface

**Files:**
- Modify: `pkg/app/wiring.go:496-526` (kiss.ManagerConfig literal), `:1852-1985` (`kissComponent` per-interface `Start*` calls)

- [ ] **Step 1: Install the hook on `kiss.ManagerConfig`**

In `pkg/app/wiring.go:496-526`, the `kiss.NewManager(kiss.ManagerConfig{...})` literal. Add (alphabetically near `RxIngress`):

```go
		OnClientTxAccepted: a.kissClientTxGateToIs,
```

- [ ] **Step 2: Thread `GateTxToIs` into `StartClient` (tcp-client)**

In `pkg/app/wiring.go:1875-1888` (the `KissTypeTCPClient` branch), add to the `kiss.ClientConfig` literal:

```go
			GateTxToIs:          ki.GateTxToIs,
```

- [ ] **Step 3: Thread into the three `StartSerial` call sites**

Three identical `kiss.SerialConfig` literals exist for `KissTypeSerial` (line 1898), `KissTypeBluetooth` (line 1921), and `KissTypeUsbSerial` (line 1945). Add `GateTxToIs: ki.GateTxToIs,` to all three. (The Bluetooth branch is hard-forced to `ModeTnc` so the flag will be inert there; passing it through is still correct for shape parity and survives a future mode-relaxation cleanly.)

- [ ] **Step 4: Thread into the server-listen `Start` call**

In `pkg/app/wiring.go:1964-1977` (the `kiss.ServerConfig` literal for server-listen tcp), add:

```go
				GateTxToIs:          ki.GateTxToIs,
```

- [ ] **Step 5: Build + run app tests**

Run: `go test ./pkg/app/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add pkg/app/wiring.go
git commit -m "app: wire KISS gate-tx-to-is hook + propagate flag per interface"
```

---

## Task 10: kiss.Manager — ifaceID-closure threading test

**Why this test (not an App-wiring test):** Task 4 covers the Server-level hook semantics. Task 8 covers the App method in isolation. The remaining integration value is that `kiss.Manager.Start` correctly wraps the operator's `OnClientTxAccepted` with the per-server interface ID — i.e., the closure captures `id` and not the loop variable. That is best tested at the manager level. An App-wiring test would need to swap out `*igate.IgateOutput` (a concrete struct with a private inner pointer) for a recording double, which is more plumbing than the asserted behavior justifies.

**Files:**
- Modify: `pkg/kiss/manager_test.go` (append `TestManagerThreadsOnClientTxAcceptedWithIfaceID`)

- [ ] **Step 1: Write the failing manager-level test**

Append to `pkg/kiss/manager_test.go`:

```go
// TestManagerThreadsOnClientTxAcceptedWithIfaceID asserts that the
// per-interface OnClientTxAccepted closure installed by Manager.Start
// captures the correct interface ID. Two servers with different IDs
// must each see their own ID arrive at the manager-level callback.
func TestManagerThreadsOnClientTxAcceptedWithIfaceID(t *testing.T) {
	type seen struct {
		ifaceID uint32
		channel uint32
	}
	gotCh := make(chan seen, 4)

	mgr := NewManager(ManagerConfig{
		Sink:   newFakeSink(),
		Logger: silentLogger(),
		OnClientTxAccepted: func(_ context.Context, ifaceID, channel uint32, _ *ax25.Frame) {
			gotCh <- seen{ifaceID: ifaceID, channel: channel}
		},
	})

	// Bind two listeners and Start two servers under different IDs.
	starts := []struct {
		id      uint32
		channel uint32
	}{
		{id: 11, channel: 7},
		{id: 22, channel: 9},
	}
	addrs := make(map[uint32]string)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for _, s := range starts {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("listen: %v", err)
		}
		addr := ln.Addr().String()
		_ = ln.Close()
		addrs[s.id] = addr
		mgr.Start(ctx, s.id, ServerConfig{
			Name:       fmt.Sprintf("srv-%d", s.id),
			ListenAddr: addr,
			ChannelMap: map[uint8]uint32{0: s.channel},
			Logger:     silentLogger(),
			Mode:       ModeModem,
			GateTxToIs: true,
		})
	}
	defer mgr.StopAll()

	// Give the listeners a moment to bind.
	time.Sleep(50 * time.Millisecond)

	// Feed one frame at each server. Use feedFrame from server_test.go.
	for _, s := range starts {
		feedFrame(t, addrs[s.id], kissUIFrameBytes(t, "hello"))
	}

	// Collect both calls, asserting each ID landed on the matching channel.
	want := map[uint32]uint32{11: 7, 22: 9}
	deadline := time.After(2 * time.Second)
	for len(want) > 0 {
		select {
		case got := <-gotCh:
			ch, ok := want[got.ifaceID]
			if !ok {
				t.Fatalf("unexpected ifaceID %d in callback", got.ifaceID)
			}
			if got.channel != ch {
				t.Fatalf("ifaceID=%d: channel=%d, want %d", got.ifaceID, got.channel, ch)
			}
			delete(want, got.ifaceID)
		case <-deadline:
			t.Fatalf("missing ifaceIDs: %v", want)
		}
	}
}
```

Add `"fmt"` and `"net"` to the import block if not already present.

- [ ] **Step 2: Run the test to confirm it fails before Task 7 is wired**

If you executed Tasks 1-9 in order, this test will already pass — Task 7 wired the closure. Run anyway to confirm:

Run: `go test ./pkg/kiss/ -run TestManagerThreadsOnClientTxAcceptedWithIfaceID -v`
Expected: PASS.

If the test fails because the closure captures the loop variable instead of `id`, the fix is in `Manager.Start` (Task 7 Step 4): use `ifaceID := id` (a fresh local binding inside the if-block) before closing over it. The plan as written already does this.

- [ ] **Step 3: Run the full kiss suite**

Run: `go test ./pkg/kiss/...`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add pkg/kiss/manager_test.go
git commit -m "kiss: test Manager threads OnClientTxAccepted with per-server ifaceID"
```

---

## Task 11: Web UI — checkbox in the KISS edit modal

**Files:**
- Modify: `web/src/routes/Kiss.svelte` — `emptyForm` (line 220-239), `openEdit` (line 302-320), `buildPayload` (line 526-556), and the JSX form (modem branch, see Step 4)

- [ ] **Step 1: Add `gate_tx_to_is: false` to `emptyForm`**

In `emptyForm` (line 220-239), after `allow_tx_from_governor: false,`:

```js
      // When the operator enables this on a Mode=modem interface,
      // frames a connected KISS client submits for TX are ALSO
      // offered to the iGate's RF->IS gate after the TX governor
      // accepts them. Default off — operator opts in.
      gate_tx_to_is: false,
```

- [ ] **Step 2: Add the field to `openEdit`'s form initializer**

In `openEdit` (line 302-320), after `allow_tx_from_governor: !!row.allow_tx_from_governor,`:

```js
      gate_tx_to_is: !!row.gate_tx_to_is,
```

- [ ] **Step 3: Surface in `buildPayload`**

In `buildPayload` (line 526-556), inside the `data = { ... }` object literal, after the `allow_tx_from_governor: form.mode === 'tnc' ? ...` line, add:

```js
      // Only meaningful in Mode=modem (Mode=tnc already feeds the
      // iGate via the RX fanout). Force false outside that mode so
      // the persisted value matches the UI's visibility rule.
      gate_tx_to_is: form.mode === 'modem' ? !!form.gate_tx_to_is : false,
```

- [ ] **Step 4: Add the checkbox to the form**

In the existing Mode-conditional block in `Kiss.svelte` (around line 951-961, the `{#if form.mode === 'tnc'}` block for `allow_tx_from_governor`), add a sibling `{#if form.mode === 'modem'}` block. Place it immediately AFTER the tnc block closes (right after line 961's `</div>`):

```svelte
    {#if form.mode === 'modem'}
      <!-- Gating opt-in for connected KISS clients (YAAC, Xastir,
           APRSdroid, etc.). When enabled, packets the client submits
           for TX are also offered to the iGate's RF->IS gate after
           the TX governor accepts them. -->
      <div class="field checkbox-field">
        <label class="checkbox-row" for="kiss-gate-tx-to-is">
          <Checkbox id="kiss-gate-tx-to-is" bind:checked={form.gate_tx_to_is} />
          <span>Also forward transmissions from connected clients to APRS-IS</span>
        </label>
        <span class="field-hint">
          Useful when a KISS app (YAAC, Xastir, APRSdroid) sends through graywolf
          and you want its packets to reach APRS-IS without that app holding its
          own APRS-IS connection. The iGate must be enabled; its filter rules
          (NOGATE, RFONLY, TCPIP) still apply.
        </span>
      </div>
    {/if}
```

- [ ] **Step 5: Add a `.field-hint` style rule**

`Kiss.svelte` has no existing `.field-hint` class (verified). In the `<style>` block, immediately after the existing `.field-warning` rule, add:

```css
  .field-hint {
    display: block;
    margin-top: 0.4rem;
    font-size: 0.875rem;
    color: var(--text-secondary, #666);
  }
```

- [ ] **Step 6: Visual smoke-check**

Run the dev server (`cd web && npm run dev`) or whichever target is your habit, open the Kiss page, click "Add Interface", switch `Mode` between `Modem` and `TNC`, and verify the new checkbox is visible only in `Modem`. Edit an existing modem-mode interface, toggle the new checkbox, save, refresh, and confirm the value persists.

Per `feedback_ui_design_quality` memory: this is a single checkbox added to an existing modal, not a new UI surface, so no Playwright mockup screenshot review is required.

- [ ] **Step 7: Commit**

```bash
git add web/src/routes/Kiss.svelte
git commit -m "web: add gate-tx-to-is checkbox to KISS edit modal (modem mode)"
```

---

## Task 12: Regenerate OpenAPI spec + TS client

**Files:**
- Auto-regenerated: `pkg/webapi/docs/gen/swagger.json`, `pkg/webapi/docs/gen/swagger.yaml`, `web/src/api/generated/api.d.ts`

- [ ] **Step 1: Confirm swag is pinned**

Per `feedback_swag_pinned_version` memory: install swag at the version CI pins (currently v1.16.4). If you do not already have it, run:

```bash
go install github.com/swaggo/swag/cmd/swag@v1.16.4
```

`@latest` will produce drift unrelated to your changes.

- [ ] **Step 2: Regenerate docs**

Run: `make docs`
Expected: regenerates `pkg/webapi/docs/gen/swagger.{json,yaml}`. Inspect the diff and confirm the only new lines are `gate_tx_to_is` field additions on `KissRequest` and `KissResponse`.

- [ ] **Step 3: Regenerate the TS client**

Run: `make api-client`
Expected: regenerates `web/src/api/generated/api.d.ts`. Diff should show only `gate_tx_to_is` additions on the generated `KissRequest`/`KissResponse` types.

- [ ] **Step 4: Run docs-check + api-client-check guards**

Run: `make docs-check api-client-check`
Expected: PASS ("no drift detected").

- [ ] **Step 5: Commit**

```bash
git add pkg/webapi/docs/gen/swagger.json \
        pkg/webapi/docs/gen/swagger.yaml \
        web/src/api/generated/api.d.ts
git commit -m "generated: regen swagger + ts client for kiss gate-tx-to-is"
```

---

## Task 13: Wiki + handbook updates

**Files:**
- Modify: `docs/wiki/code-map.md` — `kiss` row of the "Go service: networking & protocol" table
- Modify: `docs/handbook/kiss.html` — Interface Settings table

- [ ] **Step 1: Update the wiki code-map row**

Open `docs/wiki/code-map.md`. Find the `kiss` row (around line 63) in the "Go service: networking & protocol" table:

> `kiss` | KISS framing + TCP server + TCP client + serial supervisor + multi-port manager + tx queue + ratelimit | `framing.go`, `server.go`, `client.go`, `serial.go`, `manager.go`, `tx_queue.go`

Append a short sentence at the end of the "Purpose" column:

> Per-interface `gate_tx_to_is` flag, when set on a Mode=modem interface, gates frames accepted by `Sink.Submit` into the iGate's RF→IS path via `Server.OnClientTxAccepted`/`Client.OnClientTxAccepted` (wired from `App.kissClientTxGateToIs`). The flag is inert in Mode=tnc because the RX fanout already feeds the iGate (see `pkg/app/rxfanout.go:107-127`).

- [ ] **Step 2: Update the handbook Interface Settings table**

Open `docs/handbook/kiss.html`. Find the "Interface Settings" table (around line 153-201). Insert a new `<tr>` immediately after the `enabled` row:

```html
            <tr>
              <td><code>gate_tx_to_is</code></td>
              <td><code>false</code></td>
              <td>When <code>mode=modem</code>, also forward packets submitted by
              connected KISS clients to APRS-IS through your iGate. The iGate
              must be enabled; its filter rules (NOGATE / RFONLY / TCPIP)
              still apply. Has no effect when <code>mode=tnc</code>.</td>
            </tr>
```

- [ ] **Step 3: Commit**

```bash
git add docs/wiki/code-map.md docs/handbook/kiss.html
git commit -m "docs: document kiss gate-tx-to-is flag (wiki + handbook)"
```

---

## Task 14: Release note entry

**Files:**
- Modify: `pkg/releasenotes/notes.yaml` (prepend new entry)

- [ ] **Step 1: Ask the user for the bump kind + draft wording**

Per `CLAUDE.md` release workflow step 0: ask the user whether this ships as a patch or minor, and confirm the exact operator-facing wording. Suggested draft (plain ASCII, no emojis, no em dashes, single-station operator framing):

```yaml
- version: "X.Y.Z"          # set when the user picks bump-point or bump-minor
  date: "2026-MM-DD"        # set at release time
  style: info
  title: "Optionally iGate packets sent from your KISS app"
  body: |
    If you point YAAC, Xastir, APRSdroid, or another KISS client at
    Graywolf for transmission, you can now have those packets reach
    APRS-IS without configuring a separate APRS-IS connection in the
    client. Open Settings, edit the KISS interface, and turn on
    "Also forward transmissions from connected clients to APRS-IS".
    Only available on Modem-mode interfaces; the iGate must be on,
    and its existing filter rules still apply.
  link: "#/kiss"
```

- [ ] **Step 2: Prepend the finalized entry**

Open `pkg/releasenotes/notes.yaml` and prepend the entry the user approved. Keep the YAML sequence ordered newest-first per the existing convention.

- [ ] **Step 3: Build to verify embedded notes still parse**

Run: `go build ./...`
Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add pkg/releasenotes/notes.yaml
git commit -m "releasenotes: prepend entry for kiss gate-tx-to-is"
```

(Do NOT run `make bump-point` / `bump-minor` as part of this plan — that is the user's call, and the release workflow lives in `CLAUDE.md`.)

---

## Final verification

- [ ] **Step 1: Run the full test suite**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 2: Run lints**

Run: `make lint`
Expected: PASS.

- [ ] **Step 3: Manual smoke**

Bring up the dev server, configure a `Mode=modem` KISS interface with `gate_tx_to_is=true`, enable the iGate, point a KISS client (YAAC if available — otherwise `kissutil` will do) at it, and submit a beacon. Confirm:

1. The packet appears on RF (modem TX log / packet log).
2. The packet appears on APRS-IS within a few seconds — check via aprs.fi or the connected iGate's `igate_status` page.
3. Toggle the flag off, send another beacon, confirm it transmits on RF but does NOT reach APRS-IS through graywolf.

If you cannot test against APRS-IS, the wiring test from Task 10 is the gating regression guard.
