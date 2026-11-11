package workspacejournal

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	heartbeatInterval = 10 * time.Second
	heartbeatSuffix   = ".collector-heartbeat"
)

var (
	activeBytes        = journalDesc("active_bytes", "Size of the active workspace journal segment.")
	archivedBytes      = journalDesc("archived_bytes", "Total bytes in workspace journal archive files on disk.")
	segmentLimit       = journalDesc("segment_limit_bytes", "Workspace journal segment size limit.")
	volumeAvailable    = journalDesc("volume_available_bytes", "Bytes available to unprivileged users on the workspace journal filesystem.")
	volumeSize         = journalDesc("volume_size_bytes", "Total bytes on the workspace journal filesystem.")
	filesystemOK       = journalDesc("filesystem_metrics_ok", "Whether workspace journal filesystem metrics could be read.")
	collectorHeartbeat = journalDesc("collector_heartbeat_timestamp_seconds", "Unix timestamp of the last workspace journal capture-loop heartbeat, or zero if missing.")
	collectorFailure   = journalDesc("collector_failure_timestamp_seconds", "Unix timestamp of the last workspace journal I/O failure by operation, or zero if missing.", "operation")
)

func journalDesc(name, help string, labels ...string) *prometheus.Desc {
	return prometheus.NewDesc("weft_workspace_journal_"+name, help, labels, nil)
}

// MetricsHandler reads the existing read-only journal mount at scrape time.
// Volume and collector signals do not depend on an import pass or the store.
func MetricsHandler(path string) (http.Handler, error) {
	registry := prometheus.NewRegistry()
	if err := registry.Register(volumeMetrics{path: path}); err != nil {
		return nil, err
	}
	gatherers := prometheus.Gatherers{prometheus.DefaultGatherer, registry}
	return promhttp.InstrumentMetricHandler(prometheus.DefaultRegisterer, promhttp.HandlerFor(gatherers, promhttp.HandlerOpts{})), nil
}

type volumeMetrics struct{ path string }

func (m volumeMetrics) Describe(ch chan<- *prometheus.Desc) {
	for _, metric := range []*prometheus.Desc{activeBytes, archivedBytes, segmentLimit, volumeAvailable, volumeSize, filesystemOK, collectorHeartbeat, collectorFailure} {
		ch <- metric
	}
}

func (m volumeMetrics) Collect(ch chan<- prometheus.Metric) {
	gauge := func(metric *prometheus.Desc, value float64, labels ...string) {
		ch <- prometheus.MustNewConstMetric(metric, prometheus.GaugeValue, value, labels...)
	}
	gauge(segmentLimit, float64(MaxJournalBytes))
	ok := float64(1)
	info, err := regularFileInfo(m.path)
	if err == nil {
		gauge(activeBytes, float64(info.Size()))
	} else if !errors.Is(err, os.ErrNotExist) {
		ok = 0
	}
	if bytes, err := archiveUsage(m.path, info); err == nil {
		gauge(archivedBytes, float64(bytes))
	} else {
		ok = 0
	}
	if available, size, err := volumeSpace(filepath.Dir(m.path)); err == nil {
		gauge(volumeAvailable, available)
		gauge(volumeSize, size)
	} else {
		ok = 0
	}
	heartbeat, err := collectorTimestamp(m.path + heartbeatSuffix)
	if err != nil {
		ok = 0
	}
	gauge(collectorHeartbeat, heartbeat)
	for _, operation := range []string{"write", "rotate"} {
		timestamp, err := collectorTimestamp(failurePath(m.path, operation))
		if err != nil {
			ok = 0
		}
		gauge(collectorFailure, timestamp, operation)
	}
	gauge(filesystemOK, ok)
}

func archiveUsage(path string, active os.FileInfo) (int64, error) {
	dir := filepath.Dir(path)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	prefix := filepath.Base(path) + archiveSuffix
	var bytes int64
	for _, entry := range entries {
		suffix, ok := strings.CutPrefix(entry.Name(), prefix)
		if !ok || len(suffix) != 64 {
			continue
		}
		if _, err := hex.DecodeString(suffix); err != nil {
			continue
		}
		info, err := regularFileInfo(filepath.Join(dir, entry.Name()))
		if errors.Is(err, os.ErrNotExist) {
			continue // an authorized prune removed it during the directory read
		}
		if err != nil {
			return 0, err
		}
		if active == nil || !os.SameFile(info, active) {
			bytes += info.Size()
		}
	}
	return bytes, nil
}

func regularFileInfo(path string) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("workspace journal file %q is not regular", path)
	}
	return info, nil
}

func failurePath(path, operation string) string {
	return path + ".collector-" + operation + "-failed"
}

func collectorTimestamp(path string) (float64, error) {
	info, err := regularFileInfo(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return float64(info.ModTime().Unix()), nil
}

// Only the writer holding the journal lock touches these private status files.
// Their timestamps survive collector restarts. Logs and stale-heartbeat alerts
// cover failures that also make the status files unwritable.
func markCollectorTime(path string, now time.Time) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0640)
	if err == nil {
		if err := f.Close(); err != nil {
			return err
		}
	} else if errors.Is(err, os.ErrExist) {
		if _, err := regularFileInfo(path); err != nil {
			return err
		}
	} else {
		return err
	}
	return os.Chtimes(path, now, now)
}
