//go:build linux

package workspacejournal

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/storage/fsstore"
)

func TestKernelJournalLive(t *testing.T) {
	if os.Getenv("WEFT_TEST_KERNEL_JOURNAL") != "1" {
		t.Skip("run make test-workspace-journal inside the dedicated daemon")
	}
	root, err := os.MkdirTemp("/var/lib/weft/workspaces", ".kernel-journal-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	workspace := filepath.Join(root, "journal-capture")
	if err := os.Mkdir(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "events")
	w, err := OpenWriter(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Capture(ctx, root, w) }()
	defer func() { cancel(); <-done; w.Close() }()
	waitRecords := func(min int) []Record {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			select {
			case err := <-done:
				done <- err
				t.Fatalf("capture stopped: %v", err)
			default:
			}
			data, _ := os.ReadFile(path)
			var records []Record
			for _, line := range strings.Split(string(data), "\n") {
				var record Record
				if json.Unmarshal([]byte(line), &record) == nil {
					records = append(records, record)
				}
			}
			if len(records) >= min {
				return records
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("kernel records did not arrive")
		return nil
	}
	waitRecords(1)
	// The consumer/controller stays offline while the kernel collector persists
	// a file that is created and removed before any snapshot could see it.
	cmd := exec.Command("sh", "-c", "printf temporary > transient; rm transient; sleep 1")
	cmd.Dir = workspace
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := int32(cmd.Process.Pid)
	records := waitRecords(4)
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	authored := false
	for _, r := range records {
		if r.Path == "journal-capture/transient" {
			seen[r.Operation] = true
			if r.PID == pid && r.ProcessStart != "" && r.Namespace != "" && r.BootID != "" {
				authored = true
			}
		}
	}
	if !seen["created"] || !seen["deleted"] || !authored {
		t.Fatalf("missing process/transient evidence: %+v", records)
	}
	// Independent concurrent writers must retain their kernel PIDs.
	one := exec.Command("sh", "-c", "printf one > concurrent; sleep 1")
	one.Dir = workspace
	two := exec.Command("sh", "-c", "printf two >> concurrent; sleep 1")
	two.Dir = workspace
	if err := one.Start(); err != nil {
		t.Fatal(err)
	}
	if err := two.Start(); err != nil {
		t.Fatal(err)
	}
	if err := one.Wait(); err != nil {
		t.Fatal(err)
	}
	if err := two.Wait(); err != nil {
		t.Fatal(err)
	}
	records = waitRecords(7)
	authors := map[int32]bool{}
	for _, r := range records {
		if r.Path == "journal-capture/concurrent" {
			authors[r.PID] = true
		}
	}
	if !authors[int32(one.Process.Pid)] || !authors[int32(two.Process.Pid)] {
		t.Fatalf("concurrent attribution missing: %+v", records)
	}
	// An unprivileged nested sandbox writes under its own PID namespace.
	if err := os.Chown(workspace, 10001, 10001); err != nil {
		t.Fatal(err)
	}
	nested := exec.Command("docker", "run", "--rm", "--network=none", "--cap-drop=ALL", "--security-opt=no-new-privileges", "--user=10001:10001", "-v", workspace+":/workspace", "alpine:3.22", "sh", "-c", "printf nested > /workspace/nested; sleep 1")
	if out, err := nested.CombinedOutput(); err != nil {
		t.Fatalf("nested sandbox: %v %s", err, out)
	}
	records = waitRecords(9)
	nestedAuthor := false
	for _, r := range records {
		if r.Path == "journal-capture/nested" && r.PID > 0 && r.ProcessStart != "" {
			nestedAuthor = true
		}
	}
	if !nestedAuthor {
		t.Fatalf("missing nested process evidence: %+v", records)
	}
	if export := os.Getenv("WEFT_TEST_JOURNAL_EXPORT"); export != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(export, data, 0640); err != nil {
			t.Fatal(err)
		}
	}
	store, err := fsstore.Open(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	importer := Importer{Store: store, Path: path, Scopes: func(context.Context) ([]Scope, error) { return []Scope{{GraphID: "g"}}, nil }}
	if err := importer.Import(context.Background()); err != nil {
		t.Fatal(err)
	}
	events, err := store.ListEvents(context.Background(), storage.EventFilter{GraphID: "g"})
	if err != nil || len(events) < 4 {
		t.Fatalf("offline replay: %v %v", events, err)
	}
	if err := importer.Import(context.Background()); err != nil {
		t.Fatal(err)
	}
	again, _ := store.ListEvents(context.Background(), storage.EventFilter{GraphID: "g"})
	if len(again) != len(events) {
		t.Fatal("duplicate replay")
	}
}

func TestKernelQueueOverflowBecomesGap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events")
	w, err := OpenWriter(path)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	event := make([]byte, 24)
	binary.NativeEndian.PutUint64(event[8:16], unix.FAN_Q_OVERFLOW)
	binary.NativeEndian.PutUint32(event[16:20], ^uint32(0))
	var gaps unresolvedGaps
	if err := gaps.observe(time.Now(), w.Append); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := captureEvent(event, -1, "/workspace", "namespace", "boot", w, &gaps); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil || strings.Count(string(data), "kernel_queue_overflow") != 2 ||
		strings.Contains(string(data), unresolvedKernelPath) {
		t.Fatalf("%s %v", data, err)
	}
}

func TestUnresolvableDirectoryHandlesShareGapWindow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events")
	w, err := OpenWriter(path)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	event := unresolvedHandleEvent()
	var gaps unresolvedGaps
	for range 3 {
		if err := captureEvent(event, -1, "/workspace", "namespace", "boot", w, &gaps); err != nil {
			t.Fatal(err)
		}
	}
	if err := gaps.flush(w.Append); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var r Record
	if err := json.Unmarshal(data, &r); err != nil || r.Gap != unresolvedKernelPath || r.GapWindow == nil || r.GapWindow.Count != 3 {
		t.Fatalf("unresolved handles: %s (%v)", data, err)
	}
}

func TestCaptureLoopFlushesPendingGapOnExit(t *testing.T) {
	for _, readErr := range []error{unix.EAGAIN, unix.EIO} {
		t.Run(readErr.Error(), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "events")
			w, err := OpenWriter(path)
			if err != nil {
				t.Fatal(err)
			}
			defer w.Close()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			event := unresolvedHandleEvent()
			read := func(buffer []byte) (int, error) {
				if event != nil {
					n := copy(buffer, event)
					event = nil
					return n, nil
				}
				if readErr == unix.EAGAIN {
					cancel()
				}
				return 0, readErr
			}
			err = captureLoop(ctx, read, -1, "/workspace", "namespace", "boot", w)
			if readErr == unix.EAGAIN && err != nil || readErr == unix.EIO && !errors.Is(err, readErr) {
				t.Fatalf("capture exit: %v", err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			records := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
			if len(records) != 2 {
				t.Fatalf("expected startup and flushed gap, got %d records", len(records))
			}
			var gap Record
			if err := json.Unmarshal([]byte(records[1]), &gap); err != nil || gap.Gap != unresolvedKernelPath ||
				gap.GapWindow == nil || gap.GapWindow.Count != 1 {
				t.Fatalf("final gap: %s (%v)", records[1], err)
			}
		})
	}
}

func unresolvedHandleEvent() []byte {
	order := binary.NativeEndian
	event := make([]byte, 50)
	order.PutUint32(event[:4], uint32(len(event)))
	event[4] = 3
	order.PutUint16(event[6:8], 24)
	order.PutUint64(event[8:16], unix.FAN_CREATE)
	order.PutUint32(event[16:20], ^uint32(0))
	info := event[24:]
	info[0] = unix.FAN_EVENT_INFO_TYPE_DFID_NAME
	order.PutUint16(info[2:4], uint16(len(info)))
	order.PutUint32(info[12:16], 4)
	copy(info[24:], "x\x00")
	return event
}

func TestKernelJournalShutdownFlush(t *testing.T) {
	if os.Getenv("WEFT_TEST_KERNEL_JOURNAL") != "1" {
		t.Skip("run make test-workspace-journal inside the dedicated daemon")
	}
	root, err := os.MkdirTemp("/var/lib/weft/workspaces", ".kernel-journal-shutdown-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	path := filepath.Join(t.TempDir(), "events")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, "-test.run=^TestKernelJournalCaptureChild$")
	cmd.Env = append(os.Environ(), "WEFT_TEST_CAPTURE_ROOT="+root, "WEFT_TEST_CAPTURE_JOURNAL="+path)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	defer func() {
		cancel()
		<-done
	}()
	waitFor := func(description string, ready func() bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			select {
			case err := <-done:
				done <- err
				t.Fatalf("capture stopped before %s: %v", description, err)
			default:
			}
			if ready() {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatalf("timed out waiting for %s", description)
	}
	hasRecord := func(fragment string) bool {
		data, _ := os.ReadFile(path)
		return strings.Contains(string(data), fragment)
	}
	waitFor("startup record", func() bool { return hasRecord("collector_started") })
	if err := cmd.Process.Signal(syscall.SIGSTOP); err != nil {
		t.Fatal(err)
	}
	waitFor("stopped collector", func() bool {
		data, _ := os.ReadFile(fmt.Sprintf("/proc/%d/status", cmd.Process.Pid))
		return strings.Contains(string(data), "\nState:\tT")
	})
	// The file events reach the kernel queue before the parent directory is
	// removed. The collector cannot resolve its handle when it resumes.
	parent := filepath.Join(root, "transient-parent")
	if err := os.Mkdir(parent, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"one", "two", "three"} {
		if err := os.WriteFile(filepath.Join(parent, name), []byte("transient"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.RemoveAll(parent); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sentinel"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Process.Signal(syscall.SIGCONT); err != nil {
		t.Fatal(err)
	}
	waitFor("sentinel record", func() bool { return hasRecord(`"path":"sentinel"`) })
	if hasRecord(unresolvedKernelPath) {
		t.Fatal("unresolved window was already published before shutdown")
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	err = <-done
	done <- err
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var windows []gapWindow
	for line := range strings.SplitSeq(strings.TrimSuffix(string(data), "\n"), "\n") {
		r, err := decodeRecord([]byte(line))
		if err != nil {
			t.Fatal(err)
		}
		if r.Gap == unresolvedKernelPath && r.GapWindow != nil {
			windows = append(windows, *r.GapWindow)
		}
	}
	if len(windows) != 1 || windows[0].Count < 2 {
		t.Fatalf("expected one flushed window for unresolved events: %+v", windows)
	}
	if heartbeat := readJournalMetric(t, scrapeJournalMetrics(t, path), "collector_heartbeat_timestamp_seconds"); heartbeat <= 0 {
		t.Fatal("capture loop did not publish its heartbeat")
	}
}

func TestKernelJournalCaptureChild(t *testing.T) {
	root := os.Getenv("WEFT_TEST_CAPTURE_ROOT")
	if root == "" {
		t.Skip("subprocess fixture for TestKernelJournalShutdownFlush")
	}
	ctx, stop := signal.NotifyContext(t.Context(), syscall.SIGTERM)
	defer stop()
	w, err := OpenWriter(os.Getenv("WEFT_TEST_CAPTURE_JOURNAL"))
	if err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(Capture(ctx, root, w), w.Close()); err != nil {
		t.Fatal(err)
	}
}
