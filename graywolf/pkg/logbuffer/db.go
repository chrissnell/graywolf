package logbuffer

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// schema is the only table we ever create. id is monotonic so the eviction
// query can use MAX(id) - ringSize as a stable cutoff without a window
// function.
const schema = `
CREATE TABLE IF NOT EXISTS logs (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  ts_ns       INTEGER NOT NULL,
  level       TEXT NOT NULL,
  component   TEXT NOT NULL,
  msg         TEXT NOT NULL,
  attrs_json  TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_logs_ts ON logs(ts_ns);
`

// DB is the standalone graywolf-logs.db handle. It deliberately exposes
// no application-level methods beyond Close — the handler reaches into
// gorm directly so we don't carry a duplicate insert API.
type DB struct {
	gorm *gorm.DB
	Path string
}

// Open opens (or creates) the log buffer database at path. The parent
// directory is created if missing; the SQLite file is chmod-ed 0600 since
// log content can include hostnames, paths, and other operator-private
// detail.
func Open(path string) (*DB, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve logbuffer path %q: %w", path, err)
	}
	dir := filepath.Dir(abs)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create logbuffer dir %q: %w", dir, err)
	}
	// Pre-flight: confirm the target directory is writable. SQLite
	// reports a full / read-only filesystem as "out of memory", which
	// is unactionable; converting it here yields a message the operator
	// can act on. Mirrors historydb.checkWritable.
	if err := checkWritable(dir); err != nil {
		return nil, fmt.Errorf("logbuffer dir %q is not writable (filesystem full or read-only?): %w", dir, err)
	}

	gdb, err := gorm.Open(sqlite.Open(abs), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("open logbuffer %q: %w", abs, err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		return nil, err
	}
	// Single writer matches the rest of graywolf's SQLite usage and avoids
	// "database is locked" surprises on bursty inserts.
	sqlDB.SetMaxOpenConns(1)

	// WAL is required for the bursty write pattern (one INSERT per slog
	// record). Unlike historydb (which swallows this error), we treat
	// failure as fatal: rollback-journal mode would serialize every
	// record under an exclusive lock and break the "console output
	// never delayed by DB work" contract documented on Handle.
	if err := gdb.Exec("PRAGMA journal_mode=WAL").Error; err != nil {
		return nil, fmt.Errorf("PRAGMA journal_mode: %w", err)
	}
	_ = gdb.Exec("PRAGMA busy_timeout=5000").Error
	_ = gdb.Exec("PRAGMA synchronous=NORMAL").Error

	if err := gdb.Exec(schema).Error; err != nil {
		return nil, fmt.Errorf("bootstrap schema: %w", err)
	}
	// chmod is hygiene, not a security control. Filesystems that don't
	// support unix permissions (FAT32 on a Pi /boot partition, for
	// example) silently ignore it; that's intentional. Mirrors
	// configstore.Open's treatment of the same call.
	_ = os.Chmod(abs, 0o600)

	return &DB{gorm: gdb, Path: abs}, nil
}

// checkWritable verifies dir accepts a temp-file write. The created
// file is removed before return; the only side effect on success is a
// brief inode allocation. Used by Open to convert SQLite's opaque
// "out of memory" error on a full or read-only filesystem into an
// actionable message.
func checkWritable(dir string) error {
	f, err := os.CreateTemp(dir, ".logbuffer-probe-*")
	if err != nil {
		return err
	}
	name := f.Name()
	f.Close()
	return os.Remove(name)
}

// Close releases the underlying connection.
func (d *DB) Close() error {
	sqlDB, err := d.gorm.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}
