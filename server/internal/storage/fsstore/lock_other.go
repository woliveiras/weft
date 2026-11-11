//go:build !(linux || darwin || freebsd || netbsd || openbsd || dragonfly)

package fsstore

import (
	"fmt"
	"os"
)

// dirLock on platforms without flock (Windows and others) is an exclusively
// created lock file holding the owner's pid. Unlike flock it survives a
// crash: after an unclean shutdown the operator must delete <dir>/LOCK (the
// error message says so). This is the documented price of staying free of
// platform-specific dependencies; production deployments use SQLite or
// PostgreSQL.
type dirLock struct{ path string }

func acquireDirLock(path string) (*dirLock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return nil, fmt.Errorf("fsstore: %s exists: another process holds the store, or a previous run crashed (delete the file if no process is running)", path)
		}
		return nil, err
	}
	fmt.Fprintf(f, "%d\n", os.Getpid())
	f.Close()
	return &dirLock{path: path}, nil
}

func (l *dirLock) release() error {
	if l == nil || l.path == "" {
		return nil
	}
	err := os.Remove(l.path)
	l.path = ""
	return err
}
