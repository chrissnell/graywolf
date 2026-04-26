package logbuffer

// evict caps the logs table to at most ringSize rows by deleting any
// row whose id is at most (MAX(id) - ringSize). Newer rows survive.
//
// ringSize <= 0 is a no-op so callers can pass the configured ring size
// without first checking the disabled-state.
//
// AUTOINCREMENT on the id column gives us a strict monotonic key even
// across deletes, so the cutoff stays correct as the ring slides
// forward.
func evict(d *DB, ringSize int) error {
	if ringSize <= 0 {
		return nil
	}
	return d.gorm.Exec(
		"DELETE FROM logs WHERE id <= (SELECT MAX(id) - ? FROM logs)",
		ringSize,
	).Error
}
