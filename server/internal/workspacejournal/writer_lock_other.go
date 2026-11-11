//go:build !(linux || darwin || freebsd || netbsd || openbsd || dragonfly)

package workspacejournal

import (
	"errors"
	"os"
)

func lockWriter(path string) (func() error, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	return func() error { return errors.Join(f.Close(), os.Remove(path)) }, nil
}
