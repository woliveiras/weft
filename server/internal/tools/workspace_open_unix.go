//go:build !windows

package tools

import (
	"os"
	"syscall"
)

// Do not block on a FIFO or follow a final symlink swapped after WalkDir.
func openWorkspaceFile(root *os.Root, path string) (*os.File, error) {
	return root.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
}
