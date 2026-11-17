package workspacejournal

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	"github.com/woliveiras/weft/server/internal/observability"
)

func TestObserveImportedRecordCountsWindowsAndRotations(t *testing.T) {
	metrics := []prometheus.Metric{
		observability.WorkspaceJournalRecordsImported,
		observability.WorkspaceJournalGapWindows,
		observability.WorkspaceJournalGapsCoalesced,
		observability.WorkspaceJournalRotations,
	}
	before := make([]float64, len(metrics))
	for n, metric := range metrics {
		before[n] = journalMetricValue(t, metric)
	}
	observeImportedRecord(Record{Gap: "kernel_queue_overflow"})
	observeImportedRecord(Record{Gap: unresolvedKernelPath, GapWindow: &gapWindow{Count: 6}})
	observeImportedRecord(Record{PreviousSegment: &segmentRef{ID: "old", Bytes: 512}})
	for n, want := range []float64{2, 1, 5, 1} {
		if delta := journalMetricValue(t, metrics[n]) - before[n]; delta != want {
			t.Errorf("%s increased by %v, expected %v", metrics[n].Desc(), delta, want)
		}
	}
}

func TestJournalMetricsFollowCommittedImport(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "events")
	w, err := openWriter(path, 1200)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	appendLargeObservation(t, w, "legacy")
	old := inspectActive(t, path)
	appendLargeObservation(t, w, "successor")
	now := time.Now().UTC()
	if err := w.Append(Record{
		Gap: unresolvedKernelPath, Time: now, GapWindow: &gapWindow{Count: 10, Start: now, End: now},
	}); err != nil {
		t.Fatal(err)
	}
	active := inspectActive(t, path)
	store := openTestSQLite(t, filepath.Join(dir, "state.db"))
	importer := journalTestImporter(path, &cursorFailure{Store: store, fail: true})
	importer.Scopes = func(context.Context) ([]Scope, error) {
		return []Scope{{GraphID: "g"}, {GraphID: "other"}}, nil
	}
	imported := observability.WorkspaceJournalRecordsImported
	failedPasses := observability.WorkspaceJournalImports.WithLabelValues("error")
	startRecords, startErrors := journalMetricValue(t, imported), journalMetricValue(t, failedPasses)
	startRotations := journalMetricValue(t, observability.WorkspaceJournalRotations)
	startWindows := journalMetricValue(t, observability.WorkspaceJournalGapWindows)
	startGaps := journalMetricValue(t, observability.WorkspaceJournalGapsCoalesced)
	if err := importer.Import(ctx); err == nil {
		t.Fatal("expected cursor failure")
	}
	if journalMetricValue(t, failedPasses) != startErrors+1 || journalMetricValue(t, imported) != startRecords {
		t.Fatal("metrics advanced observations after a failed cursor commit")
	}
	for range 2 {
		if err := importer.Import(ctx); err != nil {
			t.Fatal(err)
		}
	}
	for _, check := range []struct {
		metric prometheus.Metric
		want   float64
	}{
		{imported, startRecords + 3},
		{observability.WorkspaceJournalRotations, startRotations + 1},
		{observability.WorkspaceJournalGapWindows, startWindows + 1},
		{observability.WorkspaceJournalGapsCoalesced, startGaps + 9},
	} {
		if got := journalMetricValue(t, check.metric); got != check.want {
			t.Errorf("%s = %v, want %v", check.metric.Desc(), got, check.want)
		}
	}
	body := scrapeJournalMetrics(t, path)
	for name, want := range map[string]int64{
		"active_bytes": active.Bytes, "archived_bytes": old.Bytes, "segment_limit_bytes": MaxJournalBytes,
	} {
		if got := readJournalMetric(t, body, name); got != float64(want) {
			t.Errorf("%s = %v, want %v", name, got, want)
		}
	}
	for _, name := range []string{
		"active_bytes", "archived_bytes", "segment_limit_bytes", "rotations_total",
		"gap_windows_total", "gaps_coalesced_total", "records_imported_total",
		"imports_total", "import_duration_seconds_count",
	} {
		if !strings.Contains(body, "\nweft_workspace_journal_"+name) {
			t.Errorf("missing exported metric %q", name)
		}
	}
}

func TestVolumeMetricsIndependentOfImporter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	w, err := openWriter(path, 1024)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { w.Close() }()
	appendLargeObservation(t, w, "first")
	first := inspectActive(t, path)
	heartbeat := time.Now().Truncate(time.Second)
	if err := markCollectorTime(path+heartbeatSuffix, heartbeat); err != nil {
		t.Fatal(err)
	}
	w.rename = func(_, _ string) error { return os.ErrPermission }
	record := Record{ID: "second", Path: strings.Repeat("a", 600), Operation: "created"}
	if err := w.Append(record); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("rotation error: %v", err)
	}
	failed := scrapeJournalMetrics(t, path)
	rotated := readJournalMetric(t, failed, `collector_failure_timestamp_seconds{operation="rotate"}`)
	if rotated < float64(heartbeat.Unix()) {
		t.Fatal("rotation failure timestamp was not exported")
	}
	if got := readJournalMetric(t, failed, "archived_bytes"); got != 0 {
		t.Fatalf("archive alias counted as extra disk usage: %v", got)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	w, err = openWriter(path, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Append(record); err != nil {
		t.Fatal(err)
	}
	if err := w.f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := w.Append(Record{Gap: "collector_started"}); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("expected write error: %v", err)
	}
	body := scrapeJournalMetrics(t, path)
	if got := readJournalMetric(t, body, `collector_failure_timestamp_seconds{operation="rotate"}`); got != rotated {
		t.Fatal("collector restart erased rotation failure timestamp")
	}
	if got := readJournalMetric(t, body, `collector_failure_timestamp_seconds{operation="write"}`); got < rotated {
		t.Fatal("write failure timestamp was not exported")
	}
	if got := readJournalMetric(t, body, "collector_heartbeat_timestamp_seconds"); got != float64(heartbeat.Unix()) {
		t.Fatalf("collector heartbeat %v", got)
	}
	if got := readJournalMetric(t, body, "archived_bytes"); got != float64(first.Bytes) {
		t.Fatalf("archived bytes without importer: %v", got)
	}
}

func TestCollectorTimestampRejectsNonRegularFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "heartbeat")
	if got, err := collectorTimestamp(path); got != 0 || err != nil {
		t.Fatalf("missing timestamp: %v, %v", got, err)
	}
	if err := os.Symlink(dir, path); err != nil {
		t.Fatal(err)
	}
	if _, err := collectorTimestamp(path); err == nil {
		t.Fatal("timestamp followed a symlink")
	}
	if err := markCollectorTime(path, time.Now()); err == nil {
		t.Fatal("timestamp updated a symlink")
	}
}

func TestVolumeMetricsReportUnavailableFilesystem(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "events")
	body := scrapeJournalMetrics(t, path)
	if got := readJournalMetric(t, body, "filesystem_metrics_ok"); got != 0 {
		t.Fatalf("missing filesystem was reported healthy: %v", got)
	}
	if got := readJournalMetric(t, body, "collector_heartbeat_timestamp_seconds"); got != 0 {
		t.Fatalf("missing heartbeat: %v", got)
	}
}

func scrapeJournalMetrics(t *testing.T, path string) string {
	t.Helper()
	handler, err := MetricsHandler(path)
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("metrics response: %d", response.Code)
	}
	return response.Body.String()
}

func readJournalMetric(t *testing.T, body, name string) float64 {
	t.Helper()
	prefix := "weft_workspace_journal_" + name + " "
	for line := range strings.SplitSeq(body, "\n") {
		if raw, ok := strings.CutPrefix(line, prefix); ok {
			value, err := strconv.ParseFloat(raw, 64)
			if err != nil {
				t.Fatal(err)
			}
			return value
		}
	}
	t.Fatalf("missing metric %q", name)
	return 0
}

func journalMetricValue(t *testing.T, metric prometheus.Metric) float64 {
	t.Helper()
	var value dto.Metric
	if err := metric.Write(&value); err != nil {
		t.Fatal(err)
	}
	if value.Gauge != nil {
		return value.GetGauge().GetValue()
	}
	return value.GetCounter().GetValue()
}
