package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/graph"
	"github.com/woliveiras/weft/server/internal/observability"
	"github.com/woliveiras/weft/server/internal/storage"
)

// Errors the API maps to HTTP status codes.
var (
	ErrNotFound = errors.New("not found")
	ErrInvalid  = errors.New("invalid")
	ErrConflict = errors.New("conflict")
)

// ValidationError carries a full validation report.
type ValidationError struct{ Report *graph.Report }

func (v *ValidationError) Error() string { return v.Report.Err().Error() }
func (v *ValidationError) Unwrap() error { return ErrInvalid }

func notFound(err error) error {
	if errors.Is(err, storage.ErrNotFound) {
		return fmt.Errorf("%w: %v", ErrNotFound, err)
	}
	return err
}

// Validate normalises and validates a graph without saving it. Model
// providers that cannot serve a node are reported as warnings: the graph can
// be saved while providers are being set up, but it cannot start (preflight).
func (e *Engine) Validate(g *domain.Graph) *graph.Report {
	rep := graph.Validate(g, e.KnownRuntimes())
	if rep.Valid {
		pre := e.preflight(context.Background(), g, "")
		rep.Warnings = append(rep.Warnings, pre.Errors...)
	}
	return rep
}

// ValidateForStart keeps informational graph warnings distinct from runtime
// requirements that prevent admission. It does not create or start a graph.
func (e *Engine) ValidateForStart(ctx context.Context, g *domain.Graph) *graph.Report {
	report := graph.Validate(g, e.KnownRuntimes())
	if report.Valid {
		pre := e.preflight(ctx, g, "")
		report.Errors = append(report.Errors, pre.Errors...)
		report.Valid = report.Valid && pre.Valid
	}
	if e.opt.ValidateToolGrants != nil {
		if err := e.opt.ValidateToolGrants(ctx, g); err != nil {
			report.Valid = false
			report.Errors = append(report.Errors, graph.Issue{Path: "spec.nodes.tools", Message: err.Error()})
		}
	}
	return report
}

// preflight checks what must hold before nodes run: every node that talks
// to a model must resolve to a usable provider, and nodes that run commands
// in the graph's environment need it built from the current definition.
// only limits the check to one node ("" = every enabled node).
func (e *Engine) preflight(ctx context.Context, g *domain.Graph, only string) *graph.Report {
	rep := &graph.Report{Valid: true, Errors: []graph.Issue{}, Warnings: []graph.Issue{}}
	if e.opt.EnvironmentProposals != nil {
		if err := e.opt.EnvironmentProposals.Check(ctx, g); err != nil {
			rep.Valid = false
			rep.Errors = append(rep.Errors, graph.Issue{Path: "spec.environment", Message: err.Error()})
		}
	}
	if e.opt.Environments != nil && g.Spec.Environment != nil {
		var users []string
		for _, n := range g.Spec.Nodes {
			if (only != "" && n.ID != only) || (only == "" && n.Disabled) {
				continue
			}
			if UsesEnvironment(g, &n) {
				users = append(users, n.ID)
			}
		}
		if len(users) > 0 {
			if err := e.opt.Environments.Check(ctx, g); err != nil {
				rep.Valid = false
				rep.Errors = append(rep.Errors, graph.Issue{
					Path:    "spec.environment",
					Message: fmt.Sprintf("%v (needed by %s)", err, strings.Join(users, ", ")),
				})
			}
		}
	}
	if e.opt.Models == nil {
		return rep
	}
	for i, n := range g.Spec.Nodes {
		if (only != "" && n.ID != only) || (only == "" && n.Disabled) {
			continue
		}
		// Native agents always call a model; external runtimes only when the
		// node selects a provider (they may not use the LLM gateway at all).
		needs := n.Runtime.Kind == domain.RuntimeNative || (n.Runtime.Kind == domain.RuntimeExternal && n.Model.Provider != "")
		if !needs {
			continue
		}
		if _, _, err := e.opt.Models.Resolve(n.Model); err != nil {
			rep.Valid = false
			name := n.Model.Provider
			if name == "" {
				name = "the default provider"
			}
			rep.Errors = append(rep.Errors, graph.Issue{
				Path:    fmt.Sprintf("spec.nodes[%d].model.provider", i),
				Message: fmt.Sprintf("node %q cannot use %s: %v", n.ID, name, err),
			})
		}
	}
	return rep
}

// SaveGraph validates and stores a new version. If the graph is running the
// new version is applied live: behaviour changes (prompt, model, tools, loop
// settings) on the next turn, structural changes (runtime, isolation,
// workspace) by gracefully restarting the affected replicas.
//
// The document is stored as written: defaults are applied when it is read
// for validation or execution, not copied into every node, so changing
// spec.defaults later still reaches nodes that do not set their own value.
// Only identity derived from names (graph, node and channel ids) is kept.
// The returned graph is the stored document.
func (e *Engine) SaveGraph(ctx context.Context, g *domain.Graph, expectedVersion int, actor string) (*domain.Graph, *domain.GraphRecord, error) {
	authored, err := cloneGraph(g)
	if err != nil {
		return nil, nil, err
	}
	rep := e.Validate(g) // normalises g in place
	if !rep.Valid {
		return nil, nil, &ValidationError{Report: rep}
	}
	if e.opt.ValidateToolGrants != nil {
		if err := e.opt.ValidateToolGrants(ctx, g); err != nil {
			return nil, nil, err
		}
	}
	keepIdentity(authored, g)
	effective := g
	g = authored
	_, existsErr := e.store.GetGraph(ctx, g.Metadata.ID, 0)
	isNew := errors.Is(existsErr, storage.ErrNotFound)
	now := time.Now().UTC()
	if isNew {
		g.Metadata.CreatedAt = now
	}
	g.Metadata.UpdatedAt = now
	v, err := e.store.SaveGraph(ctx, g, expectedVersion)
	if errors.Is(err, storage.ErrConflict) {
		return nil, nil, fmt.Errorf("%w: graph was modified (expected version %d)", ErrConflict, expectedVersion)
	}
	if err != nil {
		return nil, nil, err
	}
	g.Metadata.Version = v
	rec, err := e.store.GetGraphRecord(ctx, g.Metadata.ID)
	if errors.Is(err, storage.ErrNotFound) {
		rec = &domain.GraphRecord{ID: g.Metadata.ID, DesiredState: domain.GraphStopped, State: domain.GraphStopped}
	} else if err != nil {
		return nil, nil, err
	}
	rec.Version = v
	rec.UpdatedAt = now
	if err := e.store.PutGraphRecord(ctx, rec); err != nil {
		return nil, nil, err
	}
	// Declare topology now, so work injected before the first start is kept.
	effective.Metadata.Version = v
	if err := e.declareTopology(ctx, effective, e.replicasFor(ctx, effective)); err != nil {
		e.log.Warn("declare topology", "graph", g.Metadata.ID, "err", err)
	}
	evt := domain.EvGraphUpdated
	if isNew {
		evt = domain.EvGraphCreated
	}
	e.bus.Publish(ctx, domain.NewEvent(evt, g.Metadata.ID, map[string]any{"version": v}).WithActor(actor))
	e.Nudge()
	return g, rec, nil
}

// cloneGraph deep-copies a document (extensions included).
func cloneGraph(g *domain.Graph) (*domain.Graph, error) {
	b, err := json.Marshal(g)
	if err != nil {
		return nil, fmt.Errorf("copy graph: %w", err)
	}
	var out domain.Graph
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("copy graph: %w", err)
	}
	return &out, nil
}

// keepIdentity copies what normalisation derives from names and must stay
// stable across reads: the graph id, kind and apiVersion, node and channel
// ids. Node and channel order is unchanged by normalisation.
func keepIdentity(doc, normalized *domain.Graph) {
	doc.APIVersion, doc.Kind, doc.Metadata.ID = normalized.APIVersion, normalized.Kind, normalized.Metadata.ID
	if doc.Metadata.Name == "" {
		doc.Metadata.Name = normalized.Metadata.Name
	}
	for i := range doc.Spec.Nodes {
		doc.Spec.Nodes[i].ID = normalized.Spec.Nodes[i].ID
	}
	for i := range doc.Spec.Channels {
		doc.Spec.Channels[i].ID = normalized.Spec.Channels[i].ID
	}
}

// GetGraphDocument returns a version (0 = latest) as stored - defaults not
// applied - with its control record. Editors use it; execution uses GetGraph.
func (e *Engine) GetGraphDocument(ctx context.Context, id string, version int) (*domain.Graph, *domain.GraphRecord, error) {
	g, err := e.store.GetGraph(ctx, id, version)
	if err != nil {
		return nil, nil, notFound(err)
	}
	rec, err := e.store.GetGraphRecord(ctx, id)
	if err != nil {
		return nil, nil, notFound(err)
	}
	return g, rec, nil
}

// GetGraph returns a version (0 = latest) with defaults applied, and its
// control record.
func (e *Engine) GetGraph(ctx context.Context, id string, version int) (*domain.Graph, *domain.GraphRecord, error) {
	g, err := e.store.GetGraph(ctx, id, version)
	if err != nil {
		return nil, nil, notFound(err)
	}
	graph.Normalize(g)
	rec, err := e.store.GetGraphRecord(ctx, id)
	if err != nil {
		return nil, nil, notFound(err)
	}
	return g, rec, nil
}

// DeleteGraph removes a stopped graph and its broker groups.
func (e *Engine) DeleteGraph(ctx context.Context, id, actor string) error {
	g, rec, err := e.GetGraph(ctx, id, 0)
	if err != nil {
		return err
	}
	if rec.DesiredState != domain.GraphStopped || (rec.State != domain.GraphStopped && rec.State != "") {
		return fmt.Errorf("%w: stop the graph before deleting it", ErrConflict)
	}
	for i := range g.Spec.Channels {
		c := &g.Spec.Channels[i]
		for _, grp := range graph.SubjectGroups(g, c, e.replicasFor(ctx, g)) {
			_ = e.opt.Broker.DeleteGroup(ctx, subjectOf(g, c), grp)
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := e.store.DeleteGraph(ctx, id); err != nil {
		return notFound(err)
	}
	e.bus.Publish(ctx, domain.NewEvent(domain.EvGraphDeleted, id, nil).WithActor(actor))
	e.Nudge()
	return nil
}

func subjectOf(g *domain.Graph, c *domain.Channel) string { return g.Metadata.ID + "." + c.ID }

// GraphAction is start|stop|pause|resume|kill.
func (e *Engine) GraphAction(ctx context.Context, id, action, actor string) error {
	if action == "start" {
		g, _, err := e.GetGraph(ctx, id, 0)
		if err != nil {
			return err
		}
		if rep := e.preflight(ctx, g, ""); !rep.Valid {
			return &ValidationError{Report: rep}
		}
	}
	return e.graphAction(ctx, id, action, actor)
}

func (e *Engine) graphAction(ctx context.Context, id, action, actor string) error {
	_, rec, err := e.GetGraph(ctx, id, 0)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	var ev domain.EventType
	switch action {
	case "start":
		if rec.DesiredState == domain.GraphRunning {
			return nil
		}
		if rec.DesiredState == domain.GraphStopped || rec.DesiredState == "" {
			// A start is a new run: clear per-node stops so "run all" means all
			// (disabled nodes stay out - that is what disabled means).
			rec.RunID = domain.NewID("run")
			rec.StartedAt = &now
			if err := e.clearNodeStops(ctx, id); err != nil {
				return err
			}
		}
		rec.DesiredState = domain.GraphRunning
		ev = domain.EvGraphStarted
	case "stop":
		rec.DesiredState = domain.GraphStopped
		if rec.State != domain.GraphStopped {
			rec.State = domain.GraphStopping
		}
		// graph.stopped follows once every replica has drained.
		ev = domain.EvGraphStopping
	case "kill":
		rec.DesiredState = domain.GraphStopped
		rec.State = domain.GraphStopping
		if run := e.graphRun(id); run != nil {
			go run.stopAll(context.Background(), true)
		}
		ev = domain.EvGraphStopping
	case "pause":
		if rec.DesiredState != domain.GraphRunning {
			return fmt.Errorf("%w: only a running graph can be paused", ErrConflict)
		}
		rec.DesiredState = domain.GraphPaused
		ev = domain.EvGraphPaused
	case "resume":
		if rec.DesiredState != domain.GraphPaused {
			return fmt.Errorf("%w: graph is not paused", ErrConflict)
		}
		rec.DesiredState = domain.GraphRunning
		ev = domain.EvGraphResumed
	default:
		return fmt.Errorf("%w: unknown action %q", ErrInvalid, action)
	}
	rec.UpdatedAt = now
	if err := e.store.PutGraphRecord(ctx, rec); err != nil {
		return err
	}
	if (action == "stop" || action == "kill") && e.opt.Reviews != nil {
		if err := e.opt.Reviews.Cancel(ctx, id, "", actor); err != nil {
			return err
		}
	}
	e.bus.Publish(ctx, domain.NewEvent(ev, id, map[string]any{"action": action, "runId": rec.RunID}).WithActor(actor))
	e.Nudge()
	return nil
}

func (e *Engine) clearNodeStops(ctx context.Context, id string) error {
	ctl, err := e.store.KVList(ctx, controlNS(id), "")
	if err != nil {
		return err
	}
	for k := range ctl {
		if strings.HasPrefix(k, "node/") {
			node := strings.TrimPrefix(k, "node/")
			c := e.getNodeControl(ctx, id, node)
			c.Stopped, c.Paused = false, false
			if err := e.putNodeControl(ctx, id, node, c); err != nil {
				return err
			}
		}
	}
	return nil
}

// NodeAction is start|stop|pause|resume|kill for one node.
func (e *Engine) NodeAction(ctx context.Context, id, node, action, actor string) error {
	g, rec, err := e.GetGraph(ctx, id, 0)
	if err != nil {
		return err
	}
	n := g.Node(node)
	if n == nil {
		return fmt.Errorf("%w: node %q", ErrNotFound, node)
	}
	c := e.getNodeControl(ctx, id, node)
	switch action {
	case "start":
		if n.Disabled {
			return fmt.Errorf("%w: node is disabled", ErrConflict)
		}
		if rep := e.preflight(ctx, g, node); !rep.Valid {
			return &ValidationError{Report: rep}
		}
		c.Stopped, c.Paused = false, false
		// Starting a node clears its completion so it runs again.
		for i := 0; i < 256; i++ {
			_ = e.store.KVDelete(ctx, controlNS(id), doneKey(node, i))
		}
		if rec.DesiredState == domain.GraphStopped {
			// Starting one node of a stopped graph starts the graph with only
			// that node running.
			if err := e.graphAction(ctx, id, "start", actor); err != nil {
				return err
			}
			for _, other := range g.Spec.Nodes {
				if other.ID != node {
					oc := e.getNodeControl(ctx, id, other.ID)
					oc.Stopped = true
					_ = e.putNodeControl(ctx, id, other.ID, oc)
				}
			}
		}
	case "stop":
		c.Stopped = true
	case "kill":
		c.Stopped = true
		if run := e.graphRun(id); run != nil {
			for _, rep := range run.nodeReplicas(node) {
				go rep.stop(true)
			}
		}
	case "pause":
		c.Paused = true
	case "resume":
		c.Paused = false
	default:
		return fmt.Errorf("%w: unknown action %q", ErrInvalid, action)
	}
	if err := e.putNodeControl(ctx, id, node, c); err != nil {
		return err
	}
	if (action == "stop" || action == "kill") && e.opt.Reviews != nil {
		if err := e.opt.Reviews.Cancel(ctx, id, node, actor); err != nil {
			return err
		}
	}
	e.bus.Publish(ctx, domain.NewEvent(domain.EvLog, id, map[string]any{"message": "node " + action, "node": node}).WithNode(node, -1).WithActor(actor))
	e.Nudge()
	return nil
}

// Scale sets the replica count of a fixed cluster (or the ceiling of an
// autoscaled one, by editing the graph).
func (e *Engine) Scale(ctx context.Context, id, node string, replicas int, actor string) error {
	if replicas < 0 || replicas > 256 {
		return fmt.Errorf("%w: replicas must be between 0 and 256", ErrInvalid)
	}
	g, _, err := e.GetGraph(ctx, id, 0)
	if err != nil {
		return err
	}
	n := g.Node(node)
	if n == nil {
		return fmt.Errorf("%w: node %q", ErrNotFound, node)
	}
	if n.Cluster.Mode == domain.ClusterAutoscale {
		n.Cluster.MaxReplicas = max(replicas, n.Cluster.MinReplicas)
		_, _, err := e.SaveGraph(ctx, g, g.Metadata.Version, actor)
		return err
	}
	c := e.getNodeControl(ctx, id, node)
	c.Replicas = replicas
	if replicas == 0 {
		c.Stopped = true
	}
	if err := e.putNodeControl(ctx, id, node, c); err != nil {
		return err
	}
	// Broadcast channels need a group per replica.
	if err := e.declareTopology(ctx, g, func(nid string) int {
		if nid == node {
			return max(replicas, 1)
		}
		return replicasOf(g, nid)
	}); err != nil {
		return err
	}
	e.bus.Publish(ctx, domain.NewEvent(domain.EvNodeScaled, id, map[string]any{"replicas": replicas}).WithNode(node, -1).WithActor(actor))
	e.Nudge()
	return nil
}

// SendOperatorMessage queues a human message for a node (replica -1 = any).
func (e *Engine) SendOperatorMessage(ctx context.Context, id, node string, replica int, text, actor string) (*domain.OperatorMessage, error) {
	g, _, err := e.GetGraph(ctx, id, 0)
	if err != nil {
		return nil, err
	}
	if g.Node(node) == nil {
		return nil, fmt.Errorf("%w: node %q", ErrNotFound, node)
	}
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("%w: empty message", ErrInvalid)
	}
	m := &domain.OperatorMessage{ID: domain.NewID("opm"), GraphID: id, NodeID: node, Replica: replica, Text: text, Author: actor, CreatedAt: time.Now().UTC()}
	if err := e.store.PutOperatorMessage(ctx, m); err != nil {
		return nil, err
	}
	e.bus.Publish(ctx, domain.NewEvent(domain.EvOperatorMsg, id, m).WithNode(node, replica).WithActor(actor))
	// Wake local replicas waiting for work; remote ones see it on their next poll.
	if run := e.graphRun(id); run != nil {
		for _, rep := range run.nodeReplicas(node) {
			if replica < 0 || rep.index == replica {
				rep.mu.Lock()
				rep.pendStop = false // an operator leaning in withdraws a stop declaration
				rep.mu.Unlock()
				rep.poke()
			}
		}
	}
	return m, nil
}

// --- status ---------------------------------------------------------------

type GroupStatus struct {
	Group    string `json:"group"`
	Node     string `json:"node"`
	Replica  *int   `json:"replica,omitempty"`
	Pending  int64  `json:"pending"`
	InFlight int64  `json:"inFlight"`
	Dead     int64  `json:"dead"`
}

type ChannelStatus struct {
	ChannelID string             `json:"channelId"`
	Type      domain.ChannelType `json:"type"`
	From      string             `json:"from"`
	To        []string           `json:"to"`
	Subject   string             `json:"subject"`
	Approval  bool               `json:"approval"`
	When      string             `json:"when,omitempty"`
	Groups    []GroupStatus      `json:"groups"`
}

type NodeStatus struct {
	NodeID          string                  `json:"nodeId"`
	Name            string                  `json:"name"`
	State           domain.ReplicaState     `json:"state"`
	DesiredReplicas int                     `json:"desiredReplicas"`
	Replicas        []*domain.ReplicaStatus `json:"replicas"`
	Pending         int64                   `json:"pending"`
	Disabled        bool                    `json:"disabled"`
	Stopped         bool                    `json:"stopped"`
	Paused          bool                    `json:"paused"`
}

type GraphStatus struct {
	GraphID          string            `json:"graphId"`
	State            domain.GraphState `json:"state"`
	DesiredState     domain.GraphState `json:"desiredState"`
	RunID            string            `json:"runId"`
	Version          int               `json:"version"`
	Nodes            []NodeStatus      `json:"nodes"`
	Channels         []ChannelStatus   `json:"channels"`
	ApprovalsPending int               `json:"approvalsPending"`
}

var liveness = map[domain.ReplicaState]int{
	domain.ReplicaRunning: 9, domain.ReplicaAwaitingApproval: 8, domain.ReplicaStarting: 7, domain.ReplicaIdle: 6,
	domain.ReplicaPaused: 5, domain.ReplicaStopping: 4, domain.ReplicaFailed: 3, domain.ReplicaCompleted: 2, domain.ReplicaStopped: 1,
}

// Status assembles the live view of a graph: replica states from the store
// (written by whichever instance runs them) and backlog from the broker.
func (e *Engine) Status(ctx context.Context, id string) (*GraphStatus, error) {
	g, rec, err := e.GetGraph(ctx, id, 0)
	if err != nil {
		return nil, err
	}
	st := &GraphStatus{GraphID: id, State: rec.State, DesiredState: rec.DesiredState, RunID: rec.RunID, Version: rec.Version}
	if st.State == "" {
		st.State = domain.GraphStopped
	}
	reps, err := e.store.ListReplicaStatus(ctx, id)
	if err != nil {
		return nil, err
	}
	byNode := map[string][]*domain.ReplicaStatus{}
	for _, r := range reps {
		byNode[r.NodeID] = append(byNode[r.NodeID], r)
	}
	pendingByNode := map[string]int64{}
	for i := range g.Spec.Channels {
		c := &g.Spec.Channels[i]
		cs := ChannelStatus{ChannelID: c.ID, Type: c.Type, From: c.From, To: g.Consumers(c), Subject: subjectOf(g, c), Approval: c.Approval, When: c.When}
		if cs.To == nil {
			cs.To = []string{}
		}
		for _, node := range g.Consumers(c) {
			for _, gr := range graph.ConsumerGroups(g, node, 0) {
				if gr.Channel != c.ID {
					continue
				}
				if c.Type == domain.ChannelBroadcast {
					for r := 0; r < e.replicasFor(ctx, g)(node); r++ {
						cs.Groups = append(cs.Groups, e.groupStatus(ctx, g, c, node, subjectOf(g, c), c.ID+"."+node+"."+itoa(r), &r, pendingByNode))
					}
					continue
				}
				if c.Type == domain.ChannelQueue && len(cs.Groups) > 0 {
					continue // one shared group, already counted
				}
				cs.Groups = append(cs.Groups, e.groupStatus(ctx, g, c, node, gr.Subject, gr.Group, nil, pendingByNode))
			}
		}
		if c.Type == domain.ChannelQueue {
			// a shared queue's backlog is pending work for every consumer
			for _, gs := range cs.Groups {
				for _, node := range cs.To {
					if node != gs.Node {
						pendingByNode[node] += gs.Pending
					}
				}
			}
		}
		if cs.Groups == nil {
			cs.Groups = []GroupStatus{}
		}
		st.Channels = append(st.Channels, cs)
	}
	for _, n := range g.Spec.Nodes {
		ctl := e.getNodeControl(ctx, id, n.ID)
		ns := NodeStatus{NodeID: n.ID, Name: n.DisplayName(), State: domain.ReplicaStopped, Replicas: byNode[n.ID],
			Pending: pendingByNode[n.ID], Disabled: n.Disabled, Stopped: ctl.Stopped, Paused: ctl.Paused}
		ns.DesiredReplicas = n.Cluster.Replicas
		if ctl.Replicas > 0 {
			ns.DesiredReplicas = ctl.Replicas
		}
		if n.Cluster.Mode == domain.ClusterAutoscale {
			ns.DesiredReplicas = n.Cluster.MaxReplicas
		}
		sort.Slice(ns.Replicas, func(i, j int) bool { return ns.Replicas[i].Replica < ns.Replicas[j].Replica })
		for _, r := range ns.Replicas {
			if liveness[r.State] > liveness[ns.State] {
				ns.State = r.State
			}
		}
		if ns.Replicas == nil {
			ns.Replicas = []*domain.ReplicaStatus{}
		}
		st.Nodes = append(st.Nodes, ns)
	}
	if st.Channels == nil {
		st.Channels = []ChannelStatus{}
	}
	if aps, err := e.store.ListApprovals(ctx, storage.ApprovalFilter{GraphID: id, Status: domain.ApprovalPending, Limit: 1000}); err == nil {
		st.ApprovalsPending = len(aps)
	}
	return st, nil
}

func (e *Engine) groupStatus(ctx context.Context, g *domain.Graph, c *domain.Channel, node, subject, group string, replica *int, pending map[string]int64) GroupStatus {
	gs := GroupStatus{Group: group, Node: node}
	if replica != nil {
		r := *replica
		gs.Replica = &r
	}
	if s, err := e.opt.Broker.Stats(ctx, subject, group); err == nil {
		gs.Pending, gs.InFlight, gs.Dead = s.Pending, s.InFlight, s.Dead
		observability.QueuePending.WithLabelValues(g.Metadata.ID, c.ID, group).Set(float64(s.Pending))
		observability.QueueInFlight.WithLabelValues(g.Metadata.ID, c.ID, group).Set(float64(s.InFlight))
	}
	pending[node] += gs.Pending
	return gs
}

// UsesEnvironment reports whether node n runs its commands in g's
// environment: docker isolation without an image of its own.
func UsesEnvironment(g *domain.Graph, n *domain.Node) bool {
	return g.Spec.Environment != nil && n.Isolation.Mode == domain.IsolationDocker && n.Isolation.Image == ""
}
