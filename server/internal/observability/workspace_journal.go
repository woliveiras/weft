package observability

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	WorkspaceJournalActiveBytes = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: ns, Name: "workspace_journal_active_bytes",
		Help: "Size of the active workspace journal segment at the last read.",
	})
	WorkspaceJournalArchivedBytes = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: ns, Name: "workspace_journal_archived_bytes",
		Help: "Total bytes in the archived workspace journal segments at the last successful chain validation.",
	})
	WorkspaceJournalSegmentLimitBytes = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: ns, Name: "workspace_journal_segment_limit_bytes",
		Help: "Workspace journal segment size limit when journal import is enabled.",
	})
	WorkspaceJournalRotations = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: ns, Name: "workspace_journal_rotations_total",
		Help: "Workspace journal rotation links committed by the importer.",
	})
	WorkspaceJournalGapWindows = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: ns, Name: "workspace_journal_gap_windows_total",
		Help: "Coalesced workspace journal gap windows imported.",
	})
	WorkspaceJournalGapsCoalesced = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: ns, Name: "workspace_journal_gaps_coalesced_total",
		Help: "Additional kernel gap observations represented by imported windows (sum of count minus one).",
	})
	WorkspaceJournalRecordsImported = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: ns, Name: "workspace_journal_records_imported_total",
		Help: "Workspace observation records committed by the importer, counted once regardless of scope.",
	})
	WorkspaceJournalImports = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: ns, Name: "workspace_journal_imports_total",
		Help: "Workspace journal import passes by outcome (ok, error).",
	}, []string{"outcome"})
	WorkspaceJournalImportSeconds = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: ns, Name: "workspace_journal_import_duration_seconds",
		Help: "Latency of a workspace journal import pass.",
	})
)
