# configstore test fixtures

## `prev_release.db` (not committed)

A SQLite configstore file produced by the **previous graywolf release**
— whichever version the newest `v*` tag reachable from HEAD points at —
with a representative configuration (audio device, 3 channels, 2 KISS
interfaces, 3 beacons, 2 digipeater rules, 1 igate config). Consumed by
`TestMigrateFromPriorRelease` to exercise schema migrations against
real prior-release output rather than synthetic fresh-schema rows.

**This file is not committed to git.** It is gitignored and regenerated
dynamically — on demand locally and automatically in CI. Because the
generator always targets the current previous release tag, the fixture
never goes stale and version-pinned filenames aren't needed.

To generate locally:

```
./scripts/testdata/gen_prev_release_db.sh
```

The generator discovers the previous release by walking `git describe
--tags --match 'v*'` from HEAD (skipping HEAD itself if it carries a
v* tag, so release commits land on the version before). It then uses
`git worktree add` to materialize that tag, builds the binary, seeds
it via REST, and exports the resulting DB here.

If the file is absent, `TestMigrateFromPriorRelease` skips with a
clear message rather than failing — fresh clones still build green.
CI restores real coverage by running the generator before tests.
