//go:build linux || darwin || freebsd || netbsd || openbsd || dragonfly

package workspacejournal

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

func lockWriter(path string) (func() error, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, fmt.Errorf("another writer holds the journal lock: %w", err)
		}
		return nil, err
	}
	return f.Close, nil
}
