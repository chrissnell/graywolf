package configstore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// CreateAX25TranscriptSession inserts a new session row. Stamps
// StartedAt = now if the caller left it zero.
func (s *Store) CreateAX25TranscriptSession(ctx context.Context, sess *AX25TranscriptSession) error {
	if sess.PeerCall == "" {
		return errors.New("ax25 transcript: PeerCall required")
	}
	if sess.StartedAt.IsZero() {
		sess.StartedAt = time.Now().UTC()
	}
	return s.db.WithContext(ctx).Create(sess).Error
}

// EndAX25TranscriptSession stamps EndedAt + EndReason and rolls up the
// final byte/frame counters. Idempotent: running twice is harmless,
// the latest call wins.
func (s *Store) EndAX25TranscriptSession(ctx context.Context, id uint32, reason string, bytes, frames uint64) error {
	end := time.Now().UTC()
	res := s.db.WithContext(ctx).
		Model(&AX25TranscriptSession{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"ended_at":    end,
			"end_reason":  reason,
			"byte_count":  bytes,
			"frame_count": frames,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// AppendAX25TranscriptEntry inserts a single transcript entry. The
// caller stamps the timestamp so a transcript-on toggle that lands a
// burst of buffered events keeps their original wall-clock order.
func (s *Store) AppendAX25TranscriptEntry(ctx context.Context, e *AX25TranscriptEntry) error {
	if e.SessionID == 0 {
		return errors.New("ax25 transcript entry: SessionID required")
	}
	if e.TS.IsZero() {
		e.TS = time.Now().UTC()
	}
	if e.Direction != "rx" && e.Direction != "tx" {
		return fmt.Errorf("ax25 transcript entry: Direction must be rx|tx, got %q", e.Direction)
	}
	if e.Kind == "" {
		e.Kind = "data"
	}
	return s.db.WithContext(ctx).Create(e).Error
}

// ListAX25TranscriptSessions returns transcript-session rows ordered
// by StartedAt desc (most recent first). Cap clamps the result; pass
// 0 for "no cap" but expect callers to set a sane upper bound.
func (s *Store) ListAX25TranscriptSessions(ctx context.Context, limit int) ([]AX25TranscriptSession, error) {
	var rows []AX25TranscriptSession
	q := s.db.WithContext(ctx).Order("started_at DESC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// GetAX25TranscriptSession fetches one session by id.
func (s *Store) GetAX25TranscriptSession(ctx context.Context, id uint32) (*AX25TranscriptSession, error) {
	var row AX25TranscriptSession
	if err := s.db.WithContext(ctx).First(&row, id).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// ListAX25TranscriptEntries returns every entry for a session, ordered
// by TS asc (chronological).
func (s *Store) ListAX25TranscriptEntries(ctx context.Context, sessionID uint32) ([]AX25TranscriptEntry, error) {
	var rows []AX25TranscriptEntry
	if err := s.db.WithContext(ctx).
		Where("session_id = ?", sessionID).
		Order("ts ASC, id ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// DeleteAX25TranscriptSession removes a session row plus every entry
// that references it. Idempotent.
func (s *Store) DeleteAX25TranscriptSession(ctx context.Context, id uint32) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("session_id = ?", id).Delete(&AX25TranscriptEntry{}).Error; err != nil {
			return err
		}
		return tx.Delete(&AX25TranscriptSession{}, id).Error
	})
}

// DeleteAllAX25Transcripts wipes every transcript session + entry.
// Used by the "delete all" button on the transcripts subroute.
func (s *Store) DeleteAllAX25Transcripts(ctx context.Context) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("1 = 1").Delete(&AX25TranscriptEntry{}).Error; err != nil {
			return err
		}
		return tx.Where("1 = 1").Delete(&AX25TranscriptSession{}).Error
	})
}
