package workspacejournal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
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
		{observability.WorkspaceJournalActiveBytes, float64(active.Bytes)},
		{observability.WorkspaceJournalArchivedBytes, float64(old.Bytes)},
		{observability.WorkspaceJournalSegmentLimitBytes, float64(MaxJournalBytes)},
	} {
		if got := journalMetricValue(t, check.metric); got != check.want {
			t.Errorf("%s = %v, want %v", check.metric.Desc(), got, check.want)
		}
	}
	response := httptest.NewRecorder()
	promhttp.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := response.Body.String()
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
