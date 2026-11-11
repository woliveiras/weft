// Package observability wires metrics (Prometheus), traces and logs
// (OpenTelemetry/OTLP) for the whole platform. Metric names are part of the
// public contract (dashboards and alerts depend on them): add, never rename.
package observability

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const ns = "weft"

var (
	RepositoryImports         = promauto.NewCounterVec(prometheus.CounterOpts{Namespace: ns, Name: "repository_imports_total", Help: "Public repository import attempts by bounded outcome."}, []string{"outcome"})
	RepositoryImportSeconds   = promauto.NewHistogram(prometheus.HistogramOpts{Namespace: ns, Name: "repository_import_duration_seconds", Help: "Public repository import attempt duration."})
	TreeBudgetOperations      = promauto.NewCounterVec(prometheus.CounterOpts{Namespace: ns, Name: "tree_budget_operations_total", Help: "Tree token budget operations by bounded operation and outcome."}, []string{"operation", "outcome"})
	NativeContextOperations   = promauto.NewCounterVec(prometheus.CounterOpts{Namespace: ns, Name: "native_context_operations_total", Help: "Native turn context management by bounded operation (cap, elide, compact) and outcome."}, []string{"operation", "outcome"})
	NativeContextSeconds      = promauto.NewHistogram(prometheus.HistogramOpts{Namespace: ns, Name: "native_context_compaction_duration_seconds", Help: "Latency of native turn compaction model calls."})
	WorkspaceCheckpoints      = promauto.NewCounterVec(prometheus.CounterOpts{Namespace: ns, Name: "workspace_checkpoints_total", Help: "Worktree checkpoint operations by bounded operation (record, restore) and outcome (record: created, unchanged, unavailable, failed; restore: restored, rejected, failed)."}, []string{"operation", "outcome"})
	WorkspaceCheckpointSecs   = promauto.NewHistogramVec(prometheus.HistogramOpts{Namespace: ns, Name: "workspace_checkpoint_duration_seconds", Help: "Worktree checkpoint record and restore latency."}, []string{"operation"})
	CheckRuns                 = promauto.NewCounterVec(prometheus.CounterOpts{Namespace: ns, Name: "check_runs_total", Help: "Declared check runs (check.run) by bounded kind (test, lint, build, custom) and outcome (passed, failed, timed_out, denied, unknown, error)."}, []string{"kind", "outcome"})
	CheckSeconds              = promauto.NewHistogramVec(prometheus.HistogramOpts{Namespace: ns, Name: "check_run_duration_seconds", Help: "Duration of declared check runs that executed.", Buckets: []float64{0.5, 1, 5, 15, 30, 60, 120, 300, 600, 1200, 1800}}, []string{"kind"})
	CheckEvidenceDropped      = promauto.NewCounter(prometheus.CounterOpts{Namespace: ns, Name: "check_evidence_dropped_total", Help: "check.run results that could not be recorded as check.completed evidence."})
	ActivitySections          = promauto.NewCounterVec(prometheus.CounterOpts{Namespace: ns, Name: "activity_sections_total", Help: "Activity view sections built, by bounded section (needs_you, reviews, running, history, cost) and outcome (ok, truncated, forbidden, unavailable, failed)."}, []string{"section", "outcome"})
	ActivitySeconds           = promauto.NewHistogram(prometheus.HistogramOpts{Namespace: ns, Name: "activity_build_duration_seconds", Help: "Latency of building one Activity view response."})
	TriggerRuns               = promauto.NewCounterVec(prometheus.CounterOpts{Namespace: ns, Name: "trigger_runs_total", Help: "Issue trigger runs by bounded outcome."}, []string{"outcome"})
	TriggerSeconds            = promauto.NewHistogram(prometheus.HistogramOpts{Namespace: ns, Name: "trigger_run_duration_seconds", Help: "Issue trigger run latency."})
	BudgetOperations          = promauto.NewCounterVec(prometheus.CounterOpts{Namespace: ns, Name: "cost_budget_operations_total", Help: "Monetary budget ledger operations by scope, operation and outcome."}, []string{"scope", "operation", "outcome"})
	BudgetSeconds             = promauto.NewHistogramVec(prometheus.HistogramOpts{Namespace: ns, Name: "cost_budget_operation_duration_seconds", Help: "Monetary budget CAS operation latency."}, []string{"operation"})
	TreeBudgetSeconds         = promauto.NewHistogramVec(prometheus.HistogramOpts{Namespace: ns, Name: "tree_budget_operation_duration_seconds", Help: "Tree token budget CAS operation latency."}, []string{"operation"})
	ReviewTransitions         = promauto.NewCounterVec(prometheus.CounterOpts{Namespace: ns, Name: "review_transitions_total", Help: "Human review and preview lifecycle transitions."}, []string{"action"})
	PreviewAccess             = promauto.NewCounterVec(prometheus.CounterOpts{Namespace: ns, Name: "preview_access_total", Help: "Preview HTTP access by bounded outcome."}, []string{"outcome"})
	PreviewRequestSeconds     = promauto.NewHistogram(prometheus.HistogramOpts{Namespace: ns, Name: "preview_request_duration_seconds", Help: "Authorized preview HTTP and WebSocket connection duration."})
	EnvironmentCleanup        = promauto.NewCounterVec(prometheus.CounterOpts{Namespace: ns, Name: "environment_cleanup_total", Help: "Shared resource cleanup requests by outcome."}, []string{"outcome"})
	EnvironmentCleanupSeconds = promauto.NewHistogram(prometheus.HistogramOpts{Namespace: ns, Name: "environment_cleanup_duration_seconds", Help: "Shared resource cleanup request duration."})

	SandboxLifecycle        = promauto.NewCounterVec(prometheus.CounterOpts{Namespace: ns, Name: "sandbox_lifecycle_total", Help: "Sandbox suspend/resume operations by bounded outcome."}, []string{"action", "outcome"})
	SandboxLifecycleSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{Namespace: ns, Name: "sandbox_lifecycle_duration_seconds", Help: "Sandbox suspend/resume latency including capacity admission."}, []string{"action"})

	SandboxAdmissions       = promauto.NewCounterVec(prometheus.CounterOpts{Namespace: ns, Name: "sandbox_capacity_transitions_total", Help: "Sandbox admission transitions by bounded state."}, []string{"state"})
	SandboxAdmissionSeconds = promauto.NewHistogram(prometheus.HistogramOpts{Namespace: ns, Name: "sandbox_admission_duration_seconds", Help: "Time spent waiting for sandbox capacity, including inventory and reservation."})

	EnvironmentBuilds = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: ns, Name: "environment_builds_total",
		Help: "Project environment builds by outcome (ready, failed).",
	}, []string{"outcome"})
	EnvironmentBuildSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: ns, Name: "environment_build_duration_seconds",
		Help:    "Duration of project environment builds.",
		Buckets: []float64{5, 15, 30, 60, 120, 300, 600, 1200, 2400, 3600, 7200},
	}, []string{"outcome"})

	MessagesPublished = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: ns, Name: "messages_published_total",
		Help: "Messages published to a channel.",
	}, []string{"graph", "channel", "type"})

	MessagesConsumed = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: ns, Name: "messages_consumed_total",
		Help: "Messages consumed by a node, by outcome (acked, nacked, dead, duplicate).",
	}, []string{"graph", "node", "outcome"})

	MessagesDropped = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: ns, Name: "messages_dropped_total",
		Help: "Emitted messages that matched no route or exceeded the hop limit.",
	}, []string{"graph", "node", "reason"})

	QueuePending = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: ns, Name: "queue_pending",
		Help: "Messages waiting in a consumer group.",
	}, []string{"graph", "channel", "group"})

	QueueInFlight = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: ns, Name: "queue_inflight",
		Help: "Messages delivered and not yet acknowledged in a consumer group.",
	}, []string{"graph", "channel", "group"})

	MessageLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: ns, Name: "message_queue_latency_seconds",
		Help:    "Time from publish to the start of the turn that consumed the message.",
		Buckets: prometheus.ExponentialBuckets(0.01, 3, 12),
	}, []string{"graph", "node"})

	Executions = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: ns, Name: "executions_total",
		Help: "Agent turns by final status.",
	}, []string{"graph", "node", "status"})

	ExecutionDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: ns, Name: "execution_duration_seconds",
		Help:    "Duration of agent turns.",
		Buckets: prometheus.ExponentialBuckets(0.05, 2.5, 12),
	}, []string{"graph", "node"})

	Replicas = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: ns, Name: "replicas",
		Help: "Replicas per node by state.",
	}, []string{"graph", "node", "state"})

	LLMRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: ns, Name: "llm_requests_total",
		Help: "LLM requests by provider, model and outcome.",
	}, []string{"provider", "model", "outcome"})

	LLMDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: ns, Name: "llm_request_duration_seconds",
		Help:    "LLM request latency.",
		Buckets: prometheus.ExponentialBuckets(0.05, 2, 12),
	}, []string{"provider", "model"})

	LLMTokens = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: ns, Name: "llm_tokens_total",
		Help: "Tokens consumed by direction (input, output).",
	}, []string{"provider", "model", "direction"})

	CostCalls = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: ns, Name: "cost_calls_total",
		Help: "Model calls recorded for cost, by pricing (priced, unpriced, estimated).",
	}, []string{"provider", "model", "pricing"})

	CostMicros = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: ns, Name: "cost_micros_total",
		Help: "Recorded model cost in micro-units of the catalogue currency.",
	}, []string{"provider", "model"})

	ToolArgumentCoercions = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: ns, Name: "tool_argument_coercions_total",
		Help: "Tool calls whose argument values were converted to the schema's JSON types (registered tool names only).",
	}, []string{"tool"})

	ToolInvocations = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: ns, Name: "tool_invocations_total",
		Help: "Tool invocations by outcome (ok, error, denied, rejected, rate_limited).",
	}, []string{"tool", "outcome"})

	ToolDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: ns, Name: "tool_duration_seconds",
		Help:    "Tool invocation latency.",
		Buckets: prometheus.ExponentialBuckets(0.005, 3, 12),
	}, []string{"tool"})

	ApprovalsPending = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: ns, Name: "approvals_pending",
		Help: "Approvals waiting for a human.",
	})

	Approvals = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: ns, Name: "approvals_total",
		Help: "Approvals by kind and final status.",
	}, []string{"kind", "status"})

	APIRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: ns, Name: "api_requests_total",
		Help: "HTTP API requests.",
	}, []string{"method", "route", "code"})

	APIDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: ns, Name: "api_request_duration_seconds",
		Help:    "HTTP API latency.",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "route"})
)
