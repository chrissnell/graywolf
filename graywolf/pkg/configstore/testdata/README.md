# configstore test fixtures

## `channels_pre_v0_11.db`

A SQLite configstore file produced by graywolf v0.10.11 with a
representative configuration (audio device, 3 channels, 2 KISS
interfaces, 3 beacons, 2 digipeater rules, 1 igate config). Consumed by
`TestMigrateFromPriorRelease` to exercise the Phase 2
`channels_nullable_input_device` migration against real prior-release
output.

Generated once via `scripts/testdata/gen_pre_v0_11_db.sh` and committed
as a binary test artifact. Run the script whenever a new prior-release
bump warrants refreshing the fixture:

```
./scripts/testdata/gen_pre_v0_11_db.sh
```

If the file is absent, `TestMigrateFromPriorRelease` skips with a
clear message rather than failing — fresh clones still build green.
