//go:build unix

package rblk

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

// lockDir takes the store's writer lock: one process at a time owns a store,
// so sequence numbers and merges never race. The lock goes when the file
// closes, so a crashed process leaves none behind.
func lockDir(dir string) (*os.File, error) {
	f, err := os.OpenFile(filepath.Join(dir, "lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrLocked
		}
		return nil, err
	}
	return f, nil
}
