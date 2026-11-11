//go:build linux || darwin || freebsd || netbsd || openbsd || dragonfly

package fsstore

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// dirLock holds an exclusive flock(2) on <dir>/LOCK. The kernel releases it
// when the process exits, so a crash never leaves a stale lock. flock locks
// belong to the open file description, so even a second Open of the same
// directory inside one process is refused.
type dirLock struct{ f *os.File }

func acquireDirLock(path string) (*dirLock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, fmt.Errorf("fsstore: %s is locked by another process (or another store in this process)", path)
		}
		return nil, fmt.Errorf("fsstore: lock %s: %w", path, err)
	}
	_ = f.Truncate(0)
	_, _ = fmt.Fprintf(f, "%d\n", os.Getpid())
	return &dirLock{f: f}, nil
}

func (l *dirLock) release() error {
	if l == nil || l.f == nil {
		return nil
	}
	_ = syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	err := l.f.Close()
	l.f = nil
	return err
}
