// Package activity builds the Activity view: one bounded, read-only summary
// of what is running across all workflows, what is waiting for a person and
// what happened recently. It reads only existing records (executions,
// approvals, human reviews, replica status and the agent.plan and
// cost.recorded events) and never scans the full history: every read has a
// window, a page size or both, and a capped read is reported as truncated.
//
// Each section is built independently. A section that cannot be read is
// reported in View.Errors and its data is null, never an empty success.
package activity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"go.opentelemetry.io/otel/attribute"

	"github.com/woliveiras/weft/server/internal/cost"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/observability"
	"github.com/woliveiras/weft/server/internal/review"
	"github.com/woliveiras/weft/server/internal/storage"
)

// Bounds of one request. Reads above these sizes are cut and flagged.
const (
	DefaultWindow  = 24 * time.Hour
	MinWindow      = 5 * time.Minute
	MaxWindow      = 7 * 24 * time.Hour
	DefaultHistory = 100
	MaxHistory     = 200
	MaxNeedsYou    = 100

	maxExecutions   = 1000 // one store page (the store's maximum)
	maxApprovals    = 1000 // pending approvals read before sorting oldest first
	maxActiveGraphs = 50
	maxReplicas     = 200
	planEventsGraph = 200 // latest agent.plan events read per active graph
	costPage        = 1000
	maxCostEvents   = 5000
	maxErrorRunes   = 300
)

// ErrInvalid marks an invalid query (window, limit or filter).
var ErrInvalid = errors.New("activity: invalid query")

// Query is what a caller asks for. History filters do not change the
// counters, which always describe the whole window.
type Query struct {
	Window       time.Duration
	HistoryLimit int
	// Status filters history by outcome group: active, succeeded, failed
	// (failed or interrupted) or cancelled.
	Status   string
	Workflow string // a workflow (graph) id; task runs match their workflow
	Agent    string // a node id
}

var statusGroups = map[string][]domain.ExecutionStatus{
	"active":    {domain.ExecPending, domain.ExecRunning, domain.ExecAwaitingApproval},
	"succeeded": {domain.ExecSucceeded},
	"failed":    {domain.ExecFailed, domain.ExecInterrupted},
	"cancelled": {domain.ExecCancelled},
}

// ParseQuery reads window, limit, status, workflow and agent.
func ParseQuery(v url.Values) (Query, error) {
	q := Query{HistoryLimit: DefaultHistory}
	var err error
	if q.Window, err = ParseWindow(v.Get("window")); err != nil {
		return q, err
	}
	if s := v.Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > MaxHistory {
			return q, fmt.Errorf("%w: limit must be between 1 and %d", ErrInvalid, MaxHistory)
		}
		q.HistoryLimit = n
	}
	q.Status = v.Get("status")
	if _, ok := statusGroups[q.Status]; q.Status != "" && !ok {
		return q, fmt.Errorf("%w: status must be active, succeeded, failed or cancelled", ErrInvalid)
	}
	q.Workflow, q.Agent = strings.TrimSpace(v.Get("workflow")), strings.TrimSpace(v.Get("agent"))
	if len(q.Workflow) > 200 || len(q.Agent) > 200 {
		return q, fmt.Errorf("%w: filter too long", ErrInvalid)
	}
	return q, nil
}

// ParseWindow accepts Go durations ("90m", "24h") and whole days ("7d"),
// from MinWindow to MaxWindow; empty means DefaultWindow.
func ParseWindow(s string) (time.Duration, error) {
	if s == "" {
		return DefaultWindow, nil
	}
	var d time.Duration
	if days, ok := strings.CutSuffix(s, "d"); ok {
		n, err := strconv.Atoi(days)
		if err != nil {
			return 0, fmt.Errorf("%w: window %q", ErrInvalid, s)
		}
		d = time.Duration(n) * 24 * time.Hour
	} else {
		var err error
		if d, err = time.ParseDuration(s); err != nil {
			return 0, fmt.Errorf("%w: window %q", ErrInvalid, s)
		}
	}
	if d < MinWindow || d > MaxWindow {
		return 0, fmt.Errorf("%w: window must be between %s and 7d", ErrInvalid, MinWindow)
	}
	return d, nil
}

// Viewer is the caller, as far as visibility goes. Everything else follows
// the existing role gates: executions, approvals and replica status are
// visible to every role; human reviews need operator and, when a review names
// reviewers, the caller among them.
type Viewer struct {
	Name      string
	CanReview bool
}

// ReviewSource lists human reviews (review.Manager).
type ReviewSource interface {
	List(ctx context.Context) ([]*review.Record, error)
}

// Builder reads the activity of an installation.
type Builder struct {
	Store storage.Store
	// Reviews is nil when human reviews are not configured.
	Reviews ReviewSource
	Now     func() time.Time
}

// View is the response of GET /api/v1/activity.
type View struct {
	Window      string    `json:"window"`
	Since       time.Time `json:"since"`
	GeneratedAt time.Time `json:"generatedAt"`
	Currency    string    `json:"currency,omitempty"`
	Counters    Counters  `json:"counters"`
	// Each list is null when its section failed (see Errors).
	NeedsYou  []NeedItem     `json:"needsYou"`
	Running   []RunningGroup `json:"running"`
	History   []HistoryRow   `json:"history"`
	Options   *Options       `json:"options"`
	Limits    Limits         `json:"limits"`
	Truncated Truncated      `json:"truncated"`
	Errors    []SectionError `json:"errors"`
}

// Counters are null when the section they come from failed.
type Counters struct {
	Running   *int `json:"running"`   // replicas in a turn (running or awaiting approval)
	Waiting   *int `json:"waiting"`   // pending approvals and visible pending reviews
	Failed    *int `json:"failed"`    // failed or interrupted executions started in the window
	Finished  *int `json:"finished"`  // succeeded executions started in the window
	Cancelled *int `json:"cancelled"` // cancelled executions started in the window
}

type Limits struct {
	History     int `json:"history"`
	NeedsYou    int `json:"needsYou"`
	Executions  int `json:"executions"`
	ActiveGraph int `json:"activeGraphs"`
	Replicas    int `json:"replicas"`
	CostEvents  int `json:"costEvents"`
}

// Truncated says which reads hit their bound; counts are then lower bounds.
type Truncated struct {
	NeedsYou   bool `json:"needsYou"`
	Running    bool `json:"running"`
	Executions bool `json:"executions"`
	History    bool `json:"history"`
	Cost       bool `json:"cost"`
}

// SectionError reports a section that is unavailable to this caller.
// Code: forbidden (role), unavailable (not configured) or failed (read error).
type SectionError struct {
	Section string `json:"section"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Err     error  `json:"-"`
}

type NeedItem struct {
	// Kind is an approval kind (tool, question, message, turn, delegation,
	// environment) or "review" for a human review.
	Kind        string     `json:"kind"`
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	GraphID     string     `json:"graphId"`
	GraphName   string     `json:"graphName,omitempty"`
	Task        string     `json:"task,omitempty"`
	NodeID      string     `json:"nodeId,omitempty"`
	Replica     int        `json:"replica"`
	ExecutionID string     `json:"executionId,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
	ExpiresAt   *time.Time `json:"expiresAt,omitempty"`
}

type RunningGroup struct {
	GraphID   string            `json:"graphId"`
	GraphName string            `json:"graphName,omitempty"`
	Task      string            `json:"task,omitempty"`
	Workflow  string            `json:"workflow"`
	State     domain.GraphState `json:"state"`
	Replicas  []RunningReplica  `json:"replicas"`
}

type RunningReplica struct {
	NodeID  string              `json:"nodeId"`
	Replica int                 `json:"replica"`
	State   domain.ReplicaState `json:"state"`
	// Phase refines State: waiting_for_capacity, awaiting_approval,
	// awaiting_review, waiting_for_message, or the state itself.
	Phase       string       `json:"phase"`
	Detail      string       `json:"detail,omitempty"`
	ExecutionID string       `json:"executionId,omitempty"`
	StartedAt   *time.Time   `json:"startedAt,omitempty"`
	ElapsedMs   int64        `json:"elapsedMs,omitempty"`
	PlanStep    string       `json:"planStep,omitempty"`
	Usage       domain.Usage `json:"usage"`
	ToolCalls   int          `json:"toolCalls"`
	CostMicros  *int64       `json:"costMicros"`
}

type HistoryRow struct {
	ID          string                 `json:"id"`
	GraphID     string                 `json:"graphId"`
	GraphName   string                 `json:"graphName,omitempty"`
	Task        string                 `json:"task,omitempty"`
	Workflow    string                 `json:"workflow"`
	NodeID      string                 `json:"nodeId"`
	Replica     int                    `json:"replica"`
	Status      domain.ExecutionStatus `json:"status"`
	StartedAt   time.Time              `json:"startedAt"`
	CompletedAt *time.Time             `json:"completedAt,omitempty"`
	DurationMs  int64                  `json:"durationMs"`
	ToolCalls   int                    `json:"toolCalls"`
	Error       string                 `json:"error,omitempty"`
	Usage       domain.Usage           `json:"usage"`
	CostMicros  *int64                 `json:"costMicros"`
}

// Options are the history filter values present in the window.
type Options struct {
	Workflows []Option `json:"workflows"`
	Agents    []string `json:"agents"`
}

type Option struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

type graphInfo struct{ name, task, workflow string }

type build struct {
	b      Builder
	q      Query
	v      Viewer
	now    time.Time
	since  time.Time
	graphs map[string]graphInfo
	view   *View
	costs  map[string]int64 // nil when the cost section failed
}

// Build assembles the view. It returns an error only for a cancelled
// context; section failures are reported inside the view.
func (b Builder) Build(ctx context.Context, q Query, v Viewer) (*View, error) {
	start := time.Now()
	defer func() { observability.ActivitySeconds.Observe(time.Since(start).Seconds()) }()
	now := time.Now().UTC()
	if b.Now != nil {
		now = b.Now().UTC()
	}
	if q.Window == 0 {
		q.Window = DefaultWindow
	}
	if q.HistoryLimit <= 0 || q.HistoryLimit > MaxHistory {
		q.HistoryLimit = DefaultHistory
	}
	x := &build{b: b, q: q, v: v, now: now, since: now.Add(-q.Window), graphs: map[string]graphInfo{}}
	x.view = &View{
		Window: formatWindow(q.Window), Since: x.since, GeneratedAt: now, Errors: []SectionError{},
		Limits: Limits{History: q.HistoryLimit, NeedsYou: MaxNeedsYou, Executions: maxExecutions, ActiveGraph: maxActiveGraphs, Replicas: maxReplicas, CostEvents: maxCostEvents},
	}
	x.section(ctx, "cost", x.cost)
	x.section(ctx, "needs_you", x.needsYou)
	x.section(ctx, "running", x.running)
	x.section(ctx, "history", x.history)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return x.view, nil
}

// section runs one builder in its own span and records its outcome.
func (x *build) section(ctx context.Context, name string, fn func(context.Context) (bool, *SectionError)) {
	if failure := runSection(ctx, "activity."+name, name, x.view.Window, fn); failure != nil {
		x.view.Errors = append(x.view.Errors, *failure)
	}
}

// runSection runs fn in a span named span and counts its outcome under the
// bounded metric label metric. It returns fn's failure, if any.
func runSection(ctx context.Context, span, metric, window string, fn func(context.Context) (bool, *SectionError)) *SectionError {
	ctx, sp := observability.Start(ctx, span, attribute.String("activity.window", window))
	truncated, failure := fn(ctx)
	outcome := "ok"
	switch {
	case failure != nil && failure.Code == "failed":
		outcome = "failed"
	case failure != nil:
		outcome = failure.Code
	case truncated:
		outcome = "truncated"
	}
	sp.SetAttributes(attribute.String("activity.outcome", outcome), attribute.Bool("activity.truncated", truncated))
	var err error
	if failure != nil {
		err = failure.Err
	}
	observability.EndSpan(sp, err)
	observability.ActivitySections.WithLabelValues(metric, outcome).Inc()
	return failure
}

func failed(section, message string, err error) *SectionError {
	return &SectionError{Section: section, Code: "failed", Message: message, Err: err}
}

// graph resolves a graph's display name and its task/workflow labels.
func (x *build) graph(ctx context.Context, id string) (graphInfo, error) {
	return resolveGraph(ctx, x.b.Store, x.graphs, id)
}

// resolveGraph reads a graph's display name and its task/workflow labels
// through cache. A deleted graph still has its id; any other read error is
// returned.
func resolveGraph(ctx context.Context, store storage.Store, cache map[string]graphInfo, id string) (graphInfo, error) {
	if gi, ok := cache[id]; ok {
		return gi, nil
	}
	gi := graphInfo{workflow: id}
	g, err := store.GetGraph(ctx, id, 0)
	switch {
	case errors.Is(err, storage.ErrNotFound):
	case err != nil:
		return gi, fmt.Errorf("read graph %s: %w", id, err)
	default:
		gi.name = g.Metadata.Name
		if l := g.Metadata.Labels; l != nil {
			gi.task = l[domain.LabelTask]
			if w := l[domain.LabelTaskWorkflow]; w != "" {
				gi.workflow = w
			}
		}
	}
	cache[id] = gi
	return gi, nil
}

// cost sums cost.recorded micro-units per execution over the window.
func (x *build) cost(ctx context.Context) (bool, *SectionError) {
	sums, currency, truncated, err := readCosts(ctx, x.b.Store, x.since)
	if err != nil {
		return false, failed("cost", "Cost is unavailable; tokens are still shown.", err)
	}
	x.view.Currency, x.costs, x.view.Truncated.Cost = currency, sums, truncated
	return truncated, nil
}

// readCosts sums cost.recorded micro-units per execution since a time,
// reading newest first and stopping at since or after maxCostEvents (then
// truncated is true). Records priced in another currency than the catalog's
// are skipped.
func readCosts(ctx context.Context, store storage.Store, since time.Time) (sums map[string]int64, currency string, truncated bool, err error) {
	cat, _, err := (cost.Prices{Store: store}).Get(ctx)
	if err != nil {
		return nil, "", false, err
	}
	sums = map[string]int64{}
	var before int64
	read := 0
	for read < maxCostEvents {
		evs, err := store.ListEvents(ctx, storage.EventFilter{Types: []domain.EventType{domain.EvCostRecorded}, BeforeSeq: before, Limit: costPage})
		if err != nil {
			return nil, "", false, err
		}
		read += len(evs)
		for _, ev := range evs {
			if ev.Time.Before(since) {
				return sums, cat.Currency, false, nil
			}
			before = ev.Seq
			var r cost.Record
			if json.Unmarshal(ev.Data, &r) != nil || r.Execution == "" || (r.Priced && r.Currency != "" && r.Currency != cat.Currency) {
				continue
			}
			sums[r.Execution] += r.CostMicros
		}
		if len(evs) < costPage {
			return sums, cat.Currency, false, nil
		}
	}
	return sums, cat.Currency, true, nil
}

func (x *build) costOf(execution string) *int64 {
	if x.costs == nil {
		return nil
	}
	c := x.costs[execution]
	return &c
}

func (x *build) needsYou(ctx context.Context) (bool, *SectionError) {
	const msg = "Pending approvals could not be loaded."
	as, err := x.b.Store.ListApprovals(ctx, storage.ApprovalFilter{Status: domain.ApprovalPending, Limit: maxApprovals})
	if err != nil {
		return false, failed("needsYou", msg, err)
	}
	truncated := len(as) >= maxApprovals
	items := make([]NeedItem, 0, len(as))
	for _, a := range as {
		gi, err := x.graph(ctx, a.GraphID)
		if err != nil {
			return false, failed("needsYou", msg, err)
		}
		items = append(items, NeedItem{Kind: string(a.Kind), ID: a.ID, Title: a.Title, GraphID: a.GraphID, GraphName: gi.name, Task: gi.task,
			NodeID: a.NodeID, Replica: a.Replica, ExecutionID: a.ExecutionID, CreatedAt: a.CreatedAt, ExpiresAt: a.ExpiresAt})
	}
	switch {
	case !x.v.CanReview:
		x.view.Errors = append(x.view.Errors, SectionError{Section: "reviews", Code: "forbidden", Message: "Your role cannot see human reviews; they are not counted."})
	case x.b.Reviews == nil:
		x.view.Errors = append(x.view.Errors, SectionError{Section: "reviews", Code: "unavailable", Message: "Human reviews are not configured on this installation."})
	default:
		rs, err := x.b.Reviews.List(ctx)
		if err != nil {
			// Approvals stay visible; only the reviews part failed.
			x.view.Errors = append(x.view.Errors, *failed("reviews", "Human reviews could not be loaded; they are not counted.", err))
			observability.ActivitySections.WithLabelValues("reviews", "failed").Inc()
			break
		}
		for _, r := range rs {
			if r.State != "pending" || !r.Allows(x.v.Name) {
				continue
			}
			gi, err := x.graph(ctx, r.GraphID)
			if err != nil {
				return false, failed("needsYou", msg, err)
			}
			exp := r.ExpiresAt
			items = append(items, NeedItem{Kind: "review", ID: r.ID, Title: r.Request.Title, GraphID: r.GraphID, GraphName: gi.name, Task: gi.task,
				NodeID: r.NodeID, Replica: r.Replica, ExecutionID: r.ExecutionID, CreatedAt: r.CreatedAt, ExpiresAt: &exp})
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		if !items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].CreatedAt.Before(items[j].CreatedAt)
		}
		return items[i].ID < items[j].ID
	})
	waiting := len(items)
	x.view.Counters.Waiting = &waiting
	if len(items) > MaxNeedsYou {
		items, truncated = items[:MaxNeedsYou], true
	}
	x.view.NeedsYou = items
	x.view.Truncated.NeedsYou = truncated
	return truncated, nil
}

func activeGraph(r *domain.GraphRecord) bool {
	switch r.State {
	case domain.GraphRunning, domain.GraphPaused, domain.GraphStopping:
		return true
	}
	return r.DesiredState == domain.GraphRunning
}

// Phase refines a replica state with what its detail says it waits for.
func Phase(st domain.ReplicaState, detail string) string {
	d := strings.ToLower(detail)
	switch {
	case st == domain.ReplicaAwaitingApproval:
		return "awaiting_approval"
	case strings.Contains(d, "capacity"):
		return "waiting_for_capacity"
	case strings.Contains(d, "human review"):
		return "awaiting_review"
	case st == domain.ReplicaIdle && strings.Contains(d, "operator message"):
		return "waiting_for_message"
	}
	return string(st)
}

func (x *build) running(ctx context.Context) (bool, *SectionError) {
	const msg = "Running agents could not be loaded."
	recs, err := x.b.Store.ListGraphRecords(ctx)
	if err != nil {
		return false, failed("running", msg, err)
	}
	var active []*domain.GraphRecord
	for _, r := range recs {
		if activeGraph(r) {
			active = append(active, r)
		}
	}
	sort.SliceStable(active, func(i, j int) bool {
		a, b := active[i].StartedAt, active[j].StartedAt
		if a == nil || b == nil {
			return a != nil
		}
		return a.After(*b)
	})
	truncated := false
	if len(active) > maxActiveGraphs {
		active, truncated = active[:maxActiveGraphs], true
	}
	groups := []RunningGroup{}
	inTurn, replicas := 0, 0
	for _, rec := range active {
		if err := ctx.Err(); err != nil {
			return false, failed("running", msg, err)
		}
		reps, err := x.b.Store.ListReplicaStatus(ctx, rec.ID)
		if err != nil {
			return false, failed("running", msg, err)
		}
		gi, err := x.graph(ctx, rec.ID)
		if err != nil {
			return false, failed("running", msg, err)
		}
		state := rec.State
		if state == "" {
			state = domain.GraphStopped
		}
		g := RunningGroup{GraphID: rec.ID, GraphName: gi.name, Task: gi.task, Workflow: gi.workflow, State: state, Replicas: []RunningReplica{}}
		var withTurn []int
		for _, r := range reps {
			if !r.State.Live() {
				continue
			}
			if replicas >= maxReplicas {
				truncated = true
				break
			}
			replicas++
			rr := RunningReplica{NodeID: r.NodeID, Replica: r.Replica, State: r.State, Phase: Phase(r.State, r.Detail), Detail: r.Detail}
			if r.State == domain.ReplicaRunning || r.State == domain.ReplicaAwaitingApproval {
				inTurn++
			}
			if r.Execution != "" {
				ex, err := x.b.Store.GetExecution(ctx, r.Execution)
				switch {
				case errors.Is(err, storage.ErrNotFound):
				case err != nil:
					return false, failed("running", msg, err)
				case !ex.Status.Terminal():
					started := ex.StartedAt
					rr.ExecutionID, rr.StartedAt, rr.Usage, rr.ToolCalls = ex.ID, &started, ex.Usage, ex.ToolCalls
					rr.ElapsedMs = max(x.now.Sub(started).Milliseconds(), 0)
					rr.CostMicros = x.costOf(ex.ID)
					withTurn = append(withTurn, len(g.Replicas))
				}
			}
			g.Replicas = append(g.Replicas, rr)
		}
		if len(withTurn) > 0 {
			steps, err := x.planSteps(ctx, rec.ID)
			if err != nil {
				return false, failed("running", msg, err)
			}
			for _, i := range withTurn {
				g.Replicas[i].PlanStep = steps[g.Replicas[i].ExecutionID]
			}
		}
		if len(g.Replicas) > 0 {
			groups = append(groups, g)
		}
	}
	x.view.Running = groups
	x.view.Counters.Running = &inTurn
	x.view.Truncated.Running = truncated
	return truncated, nil
}

// planSteps maps execution id to the in-progress step of its latest plan,
// from the newest planEventsGraph agent.plan events of a graph. A plan with
// no in-progress step maps to "".
func (x *build) planSteps(ctx context.Context, graphID string) (map[string]string, error) {
	evs, err := x.b.Store.ListEvents(ctx, storage.EventFilter{GraphID: graphID, Types: []domain.EventType{domain.EvAgentPlan}, Limit: planEventsGraph})
	if err != nil {
		return nil, fmt.Errorf("read plans of %s: %w", graphID, err)
	}
	out := map[string]string{}
	for _, ev := range evs { // newest first: the first plan per execution wins
		if ev.ExecutionID == "" {
			continue
		}
		if _, seen := out[ev.ExecutionID]; seen {
			continue
		}
		out[ev.ExecutionID] = CurrentStep(ev.Data)
	}
	return out, nil
}

// CurrentStep returns the in-progress item of an agent.plan payload.
func CurrentStep(data json.RawMessage) string {
	var p struct {
		Items []struct {
			Step   string `json:"step"`
			Status string `json:"status"`
		} `json:"items"`
	}
	if json.Unmarshal(data, &p) != nil {
		return ""
	}
	for _, it := range p.Items {
		if it.Status == "in_progress" {
			return truncate(it.Step, maxErrorRunes)
		}
	}
	return ""
}

func (x *build) history(ctx context.Context) (bool, *SectionError) {
	const msg = "Recent executions could not be loaded."
	xs, err := x.b.Store.ListExecutions(ctx, storage.ExecutionFilter{Limit: maxExecutions})
	if err != nil {
		return false, failed("history", msg, err)
	}
	// Newest first: the page reached past the window start unless it is full
	// and its oldest row is still inside the window.
	truncated := len(xs) >= maxExecutions && !xs[len(xs)-1].StartedAt.Before(x.since)
	var failedN, finished, cancelled int
	rows := []HistoryRow{}
	workflows := map[string]string{}
	agents := map[string]bool{}
	allowed := statusGroups[x.q.Status]
	more := false
	for _, e := range xs {
		if e.StartedAt.Before(x.since) {
			break
		}
		switch e.Status {
		case domain.ExecFailed, domain.ExecInterrupted:
			failedN++
		case domain.ExecSucceeded:
			finished++
		case domain.ExecCancelled:
			cancelled++
		}
		gi, err := x.graph(ctx, e.GraphID)
		if err != nil {
			return false, failed("history", msg, err)
		}
		if gi.task == "" { // a workflow's own graph names it; a task run names the run
			workflows[gi.workflow] = gi.name
		} else if _, ok := workflows[gi.workflow]; !ok {
			workflows[gi.workflow] = ""
		}
		agents[e.NodeID] = true
		if (len(allowed) > 0 && !hasStatus(allowed, e.Status)) || (x.q.Workflow != "" && x.q.Workflow != gi.workflow && x.q.Workflow != e.GraphID) || (x.q.Agent != "" && x.q.Agent != e.NodeID) {
			continue
		}
		if len(rows) >= x.q.HistoryLimit {
			more = true
			continue
		}
		end := x.now
		if e.CompletedAt != nil {
			end = *e.CompletedAt
		}
		rows = append(rows, HistoryRow{ID: e.ID, GraphID: e.GraphID, GraphName: gi.name, Task: gi.task, Workflow: gi.workflow, NodeID: e.NodeID, Replica: e.Replica,
			Status: e.Status, StartedAt: e.StartedAt, CompletedAt: e.CompletedAt, DurationMs: max(end.Sub(e.StartedAt).Milliseconds(), 0),
			ToolCalls: e.ToolCalls, Error: truncate(e.Error, maxErrorRunes), Usage: e.Usage, CostMicros: x.costOf(e.ID)})
	}
	x.view.History = rows
	x.view.Counters.Failed, x.view.Counters.Finished, x.view.Counters.Cancelled = &failedN, &finished, &cancelled
	x.view.Truncated.Executions, x.view.Truncated.History = truncated, more || truncated
	opts := &Options{Workflows: []Option{}, Agents: []string{}}
	for id, name := range workflows {
		opts.Workflows = append(opts.Workflows, Option{ID: id, Name: name})
	}
	sort.Slice(opts.Workflows, func(i, j int) bool { return opts.Workflows[i].ID < opts.Workflows[j].ID })
	for a := range agents {
		opts.Agents = append(opts.Agents, a)
	}
	sort.Strings(opts.Agents)
	x.view.Options = opts
	return truncated, nil
}

func hasStatus(in []domain.ExecutionStatus, s domain.ExecutionStatus) bool {
	for _, x := range in {
		if x == s {
			return true
		}
	}
	return false
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n]) + "…"
}

func formatWindow(d time.Duration) string {
	if d%(24*time.Hour) == 0 && d > 48*time.Hour {
		return strconv.Itoa(int(d/(24*time.Hour))) + "d"
	}
	if d%time.Hour == 0 {
		return strconv.Itoa(int(d/time.Hour)) + "h"
	}
	if d%time.Minute == 0 {
		return strconv.Itoa(int(d/time.Minute)) + "m"
	}
	return d.String()
}
