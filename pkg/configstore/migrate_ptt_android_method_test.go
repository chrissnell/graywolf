package configstore

import (
	"fmt"
	"path/filepath"
	"testing"

	"gorm.io/gorm"
)

// seed a ptt_configs row with the legacy android encoding (method=android,
// transport int in gpio_pin) using raw SQL so the test does not depend on
// the post-migration struct shape.
func seedLegacyPtt(t *testing.T, db *gorm.DB, channelID, gpioPin uint32, method string) {
	t.Helper()
	if err := db.Exec(
		`INSERT INTO ptt_configs (channel_id, method, device, gpio_pin, gpio_line, invert, slot_time_ms, persist, dwait_ms) `+
			`VALUES (?, ?, '', ?, 0, 0, 10, 63, 0)`,
		channelID, method, gpioPin,
	).Error; err != nil {
		t.Fatalf("seed ptt_configs: %v", err)
	}
}

func ptt(t *testing.T, db *gorm.DB, channelID uint32) PttConfig {
	t.Helper()
	var p PttConfig
	if err := db.Where("channel_id = ?", channelID).First(&p).Error; err != nil {
		t.Fatalf("load ptt_config ch=%d: %v", channelID, err)
	}
	return p
}

func TestMigratePttAndroidMethodField(t *testing.T) {
	t.Parallel()
	dsn := filepath.Join(t.TempDir(), "ptt_android_method.db")
	store, err := Open(dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer store.Close()
	db := store.DB()

	// Seed minimal channel rows to satisfy ptt_configs.channel_id FK.
	for _, chID := range []uint32{101, 102, 103, 104} {
		if err := db.Exec(
			`INSERT INTO channels (id, name, modem_type, bit_rate, mark_freq, space_freq, profile, `+
				`num_slicers, fix_bits, num_decoders, decoder_offset, created_at, updated_at) `+
				`VALUES (?, ?, 'afsk', 1200, 1200, 2200, 'A', 1, 'none', 1, 0, datetime('now'), datetime('now'))`,
			chID, fmt.Sprintf("test-ch-%d", chID),
		).Error; err != nil {
			t.Fatalf("seed channel %d: %v", chID, err)
		}
	}

	// Re-seed legacy rows AFTER full migration, then run only the v22 fn
	// to assert its behavior deterministically.
	seedLegacyPtt(t, db, 101, 3, "android") // well-formed AIOC
	seedLegacyPtt(t, db, 102, 0, "android") // malformed -> coerce to 1
	seedLegacyPtt(t, db, 103, 3, "cm108")   // non-android, untouched
	seedLegacyPtt(t, db, 104, 5, "android") // above-upper-bound -> coerce to 1

	if err := migratePttAndroidMethodField(db); err != nil {
		t.Fatalf("migration: %v", err)
	}

	if p := ptt(t, db, 101); p.PttMethod != 3 || p.GpioPin != 0 {
		t.Fatalf("ch101: got ptt_method=%d gpio_pin=%d, want 3/0", p.PttMethod, p.GpioPin)
	}
	if p := ptt(t, db, 102); p.PttMethod != 1 || p.GpioPin != 0 {
		t.Fatalf("ch102 (malformed): got ptt_method=%d gpio_pin=%d, want 1/0", p.PttMethod, p.GpioPin)
	}
	if p := ptt(t, db, 103); p.PttMethod != 0 || p.GpioPin != 3 {
		t.Fatalf("ch103 (cm108): got ptt_method=%d gpio_pin=%d, want 0/3 (untouched)", p.PttMethod, p.GpioPin)
	}
	if p := ptt(t, db, 104); p.PttMethod != 1 || p.GpioPin != 0 {
		t.Fatalf("ch104 (gpio_pin=5 above range): got ptt_method=%d gpio_pin=%d, want 1/0", p.PttMethod, p.GpioPin)
	}

	// Idempotency: a second run must not re-coerce ch101 (3) to 1.
	if err := migratePttAndroidMethodField(db); err != nil {
		t.Fatalf("migration rerun: %v", err)
	}
	if p := ptt(t, db, 101); p.PttMethod != 3 {
		t.Fatalf("ch101 after rerun: got ptt_method=%d, want 3 (idempotent)", p.PttMethod)
	}
}
