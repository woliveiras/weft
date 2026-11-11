//go:build !linux

package workspacejournal

import "errors"

func volumeSpace(string) (float64, float64, error) {
	return 0, 0, errors.ErrUnsupported
}
