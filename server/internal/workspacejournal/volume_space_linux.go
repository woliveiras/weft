//go:build linux

package workspacejournal

import "golang.org/x/sys/unix"

func volumeSpace(path string) (available, size float64, err error) {
	var stats unix.Statfs_t
	if err := unix.Statfs(path, &stats); err != nil {
		return 0, 0, err
	}
	blockSize := stats.Frsize
	if blockSize <= 0 {
		blockSize = stats.Bsize
	}
	return float64(stats.Bavail) * float64(blockSize), float64(stats.Blocks) * float64(blockSize), nil
}
