//go:build linux

package workspacejournal

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// Capture runs in the already privileged dedicated daemon's PID namespace.
// It does not change sandbox capabilities, mounts or authorization policies.
func Capture(ctx context.Context, root string, writer *Writer) error {
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	mount, err := unix.Open(root, unix.O_DIRECTORY|unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer unix.Close(mount)
	fd, err := unix.FanotifyInit(unix.FAN_CLASS_NOTIF|unix.FAN_CLOEXEC|unix.FAN_NONBLOCK|unix.FAN_REPORT_DFID_NAME|unix.FAN_REPORT_PIDFD, unix.O_RDONLY|unix.O_CLOEXEC)
	if err != nil {
		return fmt.Errorf("fanotify initialization: %w", err)
	}
	defer unix.Close(fd)
	mask := uint64(unix.FAN_CREATE | unix.FAN_DELETE | unix.FAN_MOVED_FROM | unix.FAN_MOVED_TO | unix.FAN_CLOSE_WRITE | unix.FAN_ONDIR)
	if err := unix.FanotifyMark(fd, unix.FAN_MARK_ADD|unix.FAN_MARK_FILESYSTEM, mask, unix.AT_FDCWD, root); err != nil {
		return fmt.Errorf("fanotify filesystem mark: %w", err)
	}
	namespace, err := os.Readlink("/proc/self/ns/pid")
	if err != nil {
		return fmt.Errorf("identify collector PID namespace: %w", err)
	}
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return fmt.Errorf("identify collector boot: %w", err)
	}
	if strings.TrimSpace(string(boot)) == "" {
		return fmt.Errorf("collector boot identity is empty")
	}
	if err := writer.Append(Record{Gap: "collector_started", Namespace: namespace, BootID: strings.TrimSpace(string(boot))}); err != nil {
		return err
	}
	buffer := make([]byte, 256<<10)
	for {
		if ctx.Err() != nil {
			return nil
		}
		n, err := unix.Read(fd, buffer)
		if errors.Is(err, unix.EAGAIN) {
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(10 * time.Millisecond):
				continue
			}
		}
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return err
		}
		for pos := 0; pos < n; {
			if n-pos < 24 {
				return fmt.Errorf("short fanotify metadata")
			}
			event := buffer[pos:n]
			length := int(binary.NativeEndian.Uint32(event[:4]))
			if length < 24 || length > len(event) || event[4] != 3 {
				return fmt.Errorf("invalid fanotify metadata")
			}
			event = event[:length]
			pos += length
			if err := captureEvent(event, mount, root, namespace, strings.TrimSpace(string(boot)), writer); err != nil {
				return err
			}
		}
	}
}

func captureEvent(event []byte, mount int, root, namespace, boot string, writer *Writer) error {
	order := binary.NativeEndian
	mask := order.Uint64(event[8:16])
	pid := int32(order.Uint32(event[20:24]))
	eventFD := int32(order.Uint32(event[16:20]))
	if eventFD >= 0 {
		defer unix.Close(int(eventFD))
	}
	if mask&unix.FAN_Q_OVERFLOW != 0 {
		return writer.Append(Record{Gap: "kernel_queue_overflow"})
	}
	var path string
	pidfd := -1
	unresolved := false
	for offset := int(order.Uint16(event[6:8])); offset < len(event); {
		if len(event)-offset < 4 {
			return fmt.Errorf("short fanotify info")
		}
		info := event[offset:]
		length := int(order.Uint16(info[2:4]))
		if length < 4 || length > len(info) {
			return fmt.Errorf("invalid fanotify info")
		}
		info = info[:length]
		offset += length
		switch info[0] {
		case unix.FAN_EVENT_INFO_TYPE_PIDFD:
			if len(info) < 8 {
				return fmt.Errorf("short pidfd info")
			}
			pidfd = int(int32(order.Uint32(info[4:8])))
			if pidfd >= 0 {
				defer unix.Close(pidfd)
			}
		case unix.FAN_EVENT_INFO_TYPE_DFID_NAME:
			if len(info) < 20 {
				return fmt.Errorf("short directory handle")
			}
			size := int(order.Uint32(info[12:16]))
			if size < 0 || size > len(info)-20 {
				return fmt.Errorf("invalid file handle")
			}
			handle := unix.NewFileHandle(int32(order.Uint32(info[16:20])), info[20:20+size])
			parent, err := unix.OpenByHandleAt(mount, handle, unix.O_PATH|unix.O_CLOEXEC)
			if err != nil {
				unresolved = true
				continue
			}
			parentPath, err := os.Readlink(fmt.Sprintf("/proc/self/fd/%d", parent))
			unix.Close(parent)
			if err != nil || strings.HasSuffix(parentPath, " (deleted)") {
				unresolved = true
				continue
			}
			name := string(info[20+size:])
			if end := strings.IndexByte(name, 0); end >= 0 {
				name = name[:end]
			}
			if name == "" || strings.Contains(name, "/") {
				unresolved = true
				continue
			}
			absolute := filepath.Join(parentPath, name)
			rel, err := filepath.Rel(root, absolute)
			if err == nil && validPath(rel) {
				path = rel
			}
		}
	}
	if unresolved {
		return writer.Append(Record{Gap: "unresolved_kernel_path"})
	}
	if path == "" {
		return nil
	} // another directory on the same filesystem
	for _, part := range strings.Split(path, "/") {
		if part == ".git" {
			return nil
		}
	}
	start := ""
	if pidfd >= 0 && pid > 0 {
		poll := []unix.PollFd{{Fd: int32(pidfd), Events: unix.POLLIN}}
		ready, err := unix.Poll(poll, 0)
		if err == nil && ready == 0 {
			stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
			if err == nil {
				end := strings.LastIndex(string(stat), ")")
				if end >= 0 {
					fields := strings.Fields(string(stat[end+1:]))
					if len(fields) > 19 {
						start = fields[19]
					}
				}
			}
			if ready, err := unix.Poll(poll, 0); err != nil || ready != 0 {
				start = ""
			}
		}
	}
	for _, op := range []struct {
		mask uint64
		name string
	}{{unix.FAN_CREATE, "created"}, {unix.FAN_DELETE, "deleted"}, {unix.FAN_MOVED_FROM, "moved_from"}, {unix.FAN_MOVED_TO, "moved_to"}, {unix.FAN_CLOSE_WRITE, "modified"}} {
		if mask&op.mask != 0 {
			if err := writer.Append(Record{Path: path, Operation: op.name, PID: pid, ProcessStart: start, Namespace: namespace, BootID: boot}); err != nil {
				return err
			}
		}
	}
	return nil
}
