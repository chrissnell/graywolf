// Package mapscache manages on-disk PMTiles archives for offline use.
package mapscache

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// writeAtomic streams r into a tempfile sibling of finalPath, fsyncs,
// then renames it. Truncates if a stale .tmp exists. The onProgress
// callback (if non-nil) is invoked with the cumulative byte count
// after each successful write — used to expose progress to the API.
// On any failure, the .tmp is removed and the partial bytes count is
// returned alongside the error.
func writeAtomic(finalPath string, r io.Reader, onProgress func(int64)) (int64, error) {
	tmp := finalPath + ".tmp"
	if err := os.MkdirAll(filepath.Dir(finalPath), 0o755); err != nil {
		return 0, fmt.Errorf("mkdir parent: %w", err)
	}
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return 0, fmt.Errorf("open tmp: %w", err)
	}

	written, err := copyWithProgress(f, r, onProgress)
	if err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return written, err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return written, fmt.Errorf("fsync: %w", err)
	}
	// Must close before rename: Windows refuses to rename an open file.
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return written, fmt.Errorf("close tmp: %w", err)
	}
	if err := os.Rename(tmp, finalPath); err != nil {
		_ = os.Remove(tmp)
		return written, fmt.Errorf("rename: %w", err)
	}
	return written, nil
}

func copyWithProgress(dst io.Writer, src io.Reader, onProgress func(int64)) (int64, error) {
	buf := make([]byte, 64*1024)
	var total int64
	for {
		n, err := src.Read(buf)
		if n > 0 {
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return total, werr
			}
			total += int64(n)
			if onProgress != nil {
				onProgress(total)
			}
		}
		if err == io.EOF {
			return total, nil
		}
		if err != nil {
			return total, err
		}
	}
}
