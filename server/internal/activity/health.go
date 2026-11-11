package activity

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strconv"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/observability"
	"github.com/woliveiras/weft/server/internal/storage"
)

// Health bounds. Rankings are computed over every agent and workflow in the
// read; the row lists then keep every ranked row plus the busiest others.
const (
	DefaultRanking = 10
	MaxRanking     = 50
	// TopErrors is the number of normalized errors listed per row.
	TopErrors = 5
	// maxHealthRows caps each of the agent and workflow lists. It is above
	// three full rankings, so a ranked row is always listed.
	maxHealthRows = 200
	// maxErrorGroups caps the distinct normalized errors tracked per row;
	// failures past it are counted in OtherErrors.
	maxErrorGroups = 100
)

// HealthWindows are the windows the health view accepts.
var HealthWindows = map[string]time.Duration{"24h": 24 * time.Hour, "7d": 7 * 24 * time.Hour}

// HealthQuery is what a caller asks the health view for.
type HealthQuery struct {
	Window time.Duration
	// Ranking is the number of rows per ranking.
	Ranking int
}

// ParseHealthQuery reads window (24h or 7d, default 24h) and ranking.
func ParseHealthQuery(v url.Values) (HealthQuery, error) {
	q := HealthQuery{Window: DefaultWindow, Ranking: DefaultRanking}
	if s := v.Get("window"); s != "" {
		d, ok := HealthWindows[s]
		if !ok {
			return q, fmt.Errorf("%w: window must be 24h or 7d", ErrInvalid)
		}
		q.Window = d
	}
	if s := v.Get("ranking"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > MaxRanking {
			return q, fmt.Errorf("%w: ranking must be between 1 and %d", ErrInvalid, MaxRanking)
		}
		q.Ranking = n
	}
	return q, nil
}

// HealthView is the response of GET /api/v1/activity/health.
type HealthView struct {
	Window      string    `json:"window"`
	Since       time.Time `json:"since"`
	GeneratedAt time.Time `json:"generatedAt"`
	Currency    string    `json:"currency,omitempty"`
	// Agents, Workflows and Rankings are null when executions could not be
	// read (see Errors).
	Agents    []HealthRow     `json:"agents"`
	Workflows []HealthRow     `json:"workflows"`
	Rankings  *Rankings       `json:"rankings"`
	Limits    HealthLimits    `json:"limits"`
	Truncated HealthTruncated `json:"truncated"`
	Errors    []SectionError  `json:"errors"`
}

type HealthLimits struct {
	Executions int `json:"executions"`
	CostEvents int `json:"costEvents"`
	Rows       int `json:"rows"`
	Ranking    int `json:"ranking"`
	Errors     int `json:"errors"`
}

// HealthTruncated says which reads or lists hit their bound.
type HealthTruncated struct {
	// Executions: more executions started in the window than were read;
	// statistics describe the newest ones only.
	Executions bool `json:"executions"`
	Cost       bool `json:"cost"`
	Agents     bool `json:"agents"`
	Workflows  bool `json:"workflows"`
}

// HealthRow is one agent (a node of a workflow; task runs count under their
// workflow) or one workflow over the window.
type HealthRow struct {
	// Key is unique within its list and is what Rankings refer to.
	Key          string `json:"key"`
	Workflow     string `json:"workflow"`
	WorkflowName string `json:"workflowName,omitempty"`
	NodeID       string `json:"nodeId,omitempty"` // agents only
	Runs         int    `json:"runs"`             // executions started in the window
	Finished     int    `json:"finished"`         // of those, terminal
	Succeeded    int    `json:"succeeded"`
	Failed       int    `json:"failed"` // failed or interrupted
	Cancelled    int    `json:"cancelled"`
	// FailureRate is Failed / Finished; null with no finished run.
	FailureRate *float64 `json:"failureRate"`
	// Duration percentiles (nearest rank) of finished runs; null without one.
	P50Ms *int64       `json:"p50Ms"`
	P90Ms *int64       `json:"p90Ms"`
	Usage domain.Usage `json:"usage"`
	// CostMicros is null when cost could not be read.
	CostMicros *int64       `json:"costMicros"`
	TopErrors  []ErrorGroup `json:"topErrors"`
	// OtherErrors counts failures whose error is not in TopErrors.
	OtherErrors int     `json:"otherErrors"`
	LastRun     *RunRef `json:"lastRun,omitempty"`
	LastFailure *RunRef `json:"lastFailure,omitempty"`
}

// ErrorGroup is one normalized error and the newest run that produced it.
type ErrorGroup struct {
	Message string    `json:"message"`
	Count   int       `json:"count"`
	LastAt  time.Time `json:"lastAt"`
	LastRun RunRef    `json:"lastRun"`
}

// RunRef points at an existing run detail view.
type RunRef struct {
	ID        string                 `json:"id"`
	GraphID   string                 `json:"graphId"`
	Status    domain.ExecutionStatus `json:"status"`
	StartedAt time.Time              `json:"startedAt"`
}

// Rankings list row keys, worst first. MostExpensive is null when cost
// could not be read.
type Rankings struct {
	FailsMost     Ranking  `json:"failsMost"`
	Slowest       Ranking  `json:"slowest"`
	MostExpensive *Ranking `json:"mostExpensive"`
}

type Ranking struct {
	Agents    []string `json:"agents"`
	Workflows []string `json:"workflows"`
}

type healthAcc struct {
	row       HealthRow
	durations []int64
	errors    map[string]*ErrorGroup
	cost      int64
	lastFail  time.Time
}

// Health builds the health view: per agent and per workflow statistics of
// executions started in the window. It returns an error only for a cancelled
// context; section failures are reported inside the view.
func (b Builder) Health(ctx context.Context, q HealthQuery) (*HealthView, error) {
	start := time.Now()
	defer func() { observability.ActivityHealthSeconds.Observe(time.Since(start).Seconds()) }()
	now := time.Now().UTC()
	if b.Now != nil {
		now = b.Now().UTC()
	}
	if q.Window == 0 {
		q.Window = DefaultWindow
	}
	if q.Ranking <= 0 || q.Ranking > MaxRanking {
		q.Ranking = DefaultRanking
	}
	since := now.Add(-q.Window)
	v := &HealthView{Window: formatWindow(q.Window), Since: since, GeneratedAt: now, Errors: []SectionError{},
		Limits: HealthLimits{Executions: maxExecutions, CostEvents: maxCostEvents, Rows: maxHealthRows, Ranking: q.Ranking, Errors: TopErrors}}
	var costs map[string]int64
	report := func(f *SectionError) {
		if f != nil {
			v.Errors = append(v.Errors, *f)
		}
	}
	report(runSection(ctx, "activity.health.cost", "health_cost", v.Window, func(ctx context.Context) (bool, *SectionError) {
		sums, currency, truncated, err := readCosts(ctx, b.Store, since)
		if err != nil {
			return false, failed("cost", "Cost is unavailable; the most expensive ranking is not shown.", err)
		}
		costs, v.Currency, v.Truncated.Cost = sums, currency, truncated
		return truncated, nil
	}))
	report(runSection(ctx, "activity.health.executions", "health_executions", v.Window, func(ctx context.Context) (bool, *SectionError) {
		return b.healthRows(ctx, v, q, since, now, costs)
	}))
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return v, nil
}

func (b Builder) healthRows(ctx context.Context, v *HealthView, q HealthQuery, since, now time.Time, costs map[string]int64) (bool, *SectionError) {
	const msg = "Executions could not be loaded; health is unavailable."
	xs, err := b.Store.ListExecutions(ctx, storage.ExecutionFilter{Limit: maxExecutions})
	if err != nil {
		return false, failed("executions", msg, err)
	}
	truncated := len(xs) >= maxExecutions && !xs[len(xs)-1].StartedAt.Before(since)
	graphs := map[string]graphInfo{}
	agents, workflows := map[string]*healthAcc{}, map[string]*healthAcc{}
	names := map[string]string{}
	for _, e := range xs {
		if e.StartedAt.Before(since) {
			break // newest first
		}
		gi, err := resolveGraph(ctx, b.Store, graphs, e.GraphID)
		if err != nil {
			return false, failed("executions", msg, err)
		}
		if gi.task == "" && gi.name != "" { // a workflow's own graph names it
			names[gi.workflow] = gi.name
		}
		akey := gi.workflow + "/" + e.NodeID
		a := agents[akey]
		if a == nil {
			a = &healthAcc{row: HealthRow{Key: akey, Workflow: gi.workflow, NodeID: e.NodeID}, errors: map[string]*ErrorGroup{}}
			agents[akey] = a
		}
		w := workflows[gi.workflow]
		if w == nil {
			w = &healthAcc{row: HealthRow{Key: gi.workflow, Workflow: gi.workflow}, errors: map[string]*ErrorGroup{}}
			workflows[gi.workflow] = w
		}
		var c int64
		if costs != nil {
			c = costs[e.ID]
		}
		a.add(e, c)
		w.add(e, c)
	}
	if err := ctx.Err(); err != nil {
		return false, failed("executions", msg, err)
	}
	agentRows := finish(agents, names, costs != nil)
	workflowRows := finish(workflows, names, costs != nil)
	r := &Rankings{
		FailsMost: Ranking{Agents: rank(agentRows, q.Ranking, failsMore), Workflows: rank(workflowRows, q.Ranking, failsMore)},
		Slowest:   Ranking{Agents: rank(agentRows, q.Ranking, slower), Workflows: rank(workflowRows, q.Ranking, slower)},
	}
	if costs != nil {
		r.MostExpensive = &Ranking{Agents: rank(agentRows, q.Ranking, costlier), Workflows: rank(workflowRows, q.Ranking, costlier)}
	}
	var cutA, cutW bool
	v.Agents, cutA = keep(agentRows, r, func(r Ranking) []string { return r.Agents })
	v.Workflows, cutW = keep(workflowRows, r, func(r Ranking) []string { return r.Workflows })
	v.Rankings = r
	v.Truncated.Executions, v.Truncated.Agents, v.Truncated.Workflows = truncated, cutA, cutW
	return truncated || cutA || cutW, nil
}

func (h *healthAcc) add(e *domain.Execution, cost int64) {
	r := &h.row
	r.Runs++
	r.Usage.InputTokens += e.Usage.InputTokens
	r.Usage.OutputTokens += e.Usage.OutputTokens
	r.Usage.CacheReadTokens += e.Usage.CacheReadTokens
	r.Usage.CacheWriteTokens += e.Usage.CacheWriteTokens
	h.cost += cost
	ref := RunRef{ID: e.ID, GraphID: e.GraphID, Status: e.Status, StartedAt: e.StartedAt}
	if r.LastRun == nil {
		r.LastRun = &ref // executions arrive newest first
	}
	if !e.Status.Terminal() {
		return
	}
	r.Finished++
	if e.CompletedAt != nil {
		h.durations = append(h.durations, max(e.CompletedAt.Sub(e.StartedAt).Milliseconds(), 0))
	}
	switch e.Status {
	case domain.ExecSucceeded:
		r.Succeeded++
		return
	case domain.ExecCancelled:
		r.Cancelled++
		return
	case domain.ExecFailed, domain.ExecInterrupted:
	default:
		return
	}
	r.Failed++
	if r.LastFailure == nil {
		r.LastFailure = &ref
	}
	at := e.StartedAt
	if e.CompletedAt != nil {
		at = *e.CompletedAt
	}
	m := NormalizeError(e.Error)
	g := h.errors[m]
	if g == nil {
		if len(h.errors) >= maxErrorGroups {
			r.OtherErrors++
			return
		}
		g = &ErrorGroup{Message: m}
		h.errors[m] = g
	}
	g.Count++
	if g.LastAt.IsZero() || at.After(g.LastAt) {
		g.LastAt, g.LastRun = at, ref
	}
}

func finish(accs map[string]*healthAcc, names map[string]string, withCost bool) []HealthRow {
	rows := make([]HealthRow, 0, len(accs))
	for _, h := range accs {
		r := h.row
		r.WorkflowName = names[r.Workflow]
		if r.Finished > 0 {
			rate := float64(r.Failed) / float64(r.Finished)
			r.FailureRate = &rate
		}
		if len(h.durations) > 0 {
			sort.Slice(h.durations, func(i, j int) bool { return h.durations[i] < h.durations[j] })
			p50, p90 := percentile(h.durations, 0.5), percentile(h.durations, 0.9)
			r.P50Ms, r.P90Ms = &p50, &p90
		}
		if withCost {
			c := h.cost
			r.CostMicros = &c
		}
		groups := make([]ErrorGroup, 0, len(h.errors))
		for _, g := range h.errors {
			groups = append(groups, *g)
		}
		sort.Slice(groups, func(i, j int) bool {
			if groups[i].Count != groups[j].Count {
				return groups[i].Count > groups[j].Count
			}
			if !groups[i].LastAt.Equal(groups[j].LastAt) {
				return groups[i].LastAt.After(groups[j].LastAt)
			}
			return groups[i].Message < groups[j].Message
		})
		if len(groups) > TopErrors {
			for _, g := range groups[TopErrors:] {
				r.OtherErrors += g.Count
			}
			groups = groups[:TopErrors]
		}
		r.TopErrors = groups
		rows = append(rows, r)
	}
	// Busiest first, then by key, so the list is stable.
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Runs != rows[j].Runs {
			return rows[i].Runs > rows[j].Runs
		}
		return rows[i].Key < rows[j].Key
	})
	return rows
}

// percentile is the nearest-rank percentile of sorted values.
func percentile(sorted []int64, p float64) int64 {
	i := int(math.Ceil(p*float64(len(sorted)))) - 1
	return sorted[min(max(i, 0), len(sorted)-1)]
}

// A ranking says which rows take part and which of two ranks higher.
type ranking struct {
	qualifies func(r *HealthRow) bool
	above     func(a, b *HealthRow) bool
}

var (
	failsMore = ranking{
		qualifies: func(r *HealthRow) bool { return r.Failed > 0 && r.FailureRate != nil },
		above: func(a, b *HealthRow) bool {
			if *a.FailureRate != *b.FailureRate {
				return *a.FailureRate > *b.FailureRate
			}
			return a.Failed > b.Failed
		},
	}
	slower = ranking{
		qualifies: func(r *HealthRow) bool { return r.P90Ms != nil },
		above: func(a, b *HealthRow) bool {
			if *a.P90Ms != *b.P90Ms {
				return *a.P90Ms > *b.P90Ms
			}
			return *a.P50Ms > *b.P50Ms
		},
	}
	costlier = ranking{
		qualifies: func(r *HealthRow) bool { return r.CostMicros != nil && *r.CostMicros > 0 },
		above:     func(a, b *HealthRow) bool { return *a.CostMicros > *b.CostMicros },
	}
)

// rank returns the keys of the top n qualifying rows, worst first; ties
// break by key.
func rank(rows []HealthRow, n int, by ranking) []string {
	var in []*HealthRow
	for i := range rows {
		if by.qualifies(&rows[i]) {
			in = append(in, &rows[i])
		}
	}
	sort.SliceStable(in, func(i, j int) bool {
		if by.above(in[i], in[j]) {
			return true
		}
		if by.above(in[j], in[i]) {
			return false
		}
		return in[i].Key < in[j].Key
	})
	out := make([]string, 0, min(n, len(in)))
	for _, r := range in[:min(n, len(in))] {
		out = append(out, r.Key)
	}
	return out
}

// keep returns every ranked row and then the busiest others, up to
// maxHealthRows, in busiest-first order. cut reports dropped rows.
func keep(rows []HealthRow, r *Rankings, pick func(Ranking) []string) ([]HealthRow, bool) {
	if len(rows) <= maxHealthRows {
		return rows, false
	}
	ranked := map[string]bool{}
	for _, rk := range []*Ranking{&r.FailsMost, &r.Slowest, r.MostExpensive} {
		if rk != nil {
			for _, k := range pick(*rk) {
				ranked[k] = true
			}
		}
	}
	room := maxHealthRows - len(ranked)
	out := make([]HealthRow, 0, maxHealthRows)
	for _, row := range rows {
		switch {
		case ranked[row.Key]:
		case room > 0:
			room--
		default:
			continue
		}
		out = append(out, row)
	}
	return out, true
}
