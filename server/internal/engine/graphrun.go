package engine

import (
	"context"
	"encoding/json"
	"strconv"
	"sync"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/graph"
	"github.com/woliveiras/weft/server/internal/observability"
	"github.com/woliveiras/weft/server/internal/queue"
	"github.com/woliveiras/weft/server/internal/storage"
)

// graphRun is one graph version deployed on this instance.
type graphRun struct {
	e       *Engine
	g       *domain.Graph
	version int
	runID   string
	router  *graph.Router
	params  map[string]string

	mu       sync.Mutex
	replicas map[string]*replica // key: node#n
	paused   bool
	declared map[string]int // subject -> number of groups declared
}

func newGraphRun(e *Engine, g *domain.Graph, runID string) (*graphRun, error) {
	rt, err := graph.NewRouter(g)
	if err != nil {
		return nil, err
	}
	return &graphRun{
		e: e, g: g, version: g.Metadata.Version, runID: runID, router: rt,
		params:   graph.Params(g),
		replicas: map[string]*replica{},
		declared: map[string]int{},
	}, nil
}

// nodeControl is the per-node operator override, persisted in KV so that it
// survives restarts and is shared by every instance.
type nodeControl struct {
	Stopped  bool `json:"stopped,omitempty"`
	Paused   bool `json:"paused,omitempty"`
	Replicas int  `json:"replicas,omitempty"` // scale override for fixed clusters
}

func controlNS(graphID string) string { return "_control/" + graphID }

func (e *Engine) getNodeControl(ctx context.Context, graphID, node string) nodeControl {
	var c nodeControl
	if b, err := e.store.KVGet(ctx, controlNS(graphID), "node/"+node); err == nil {
		_ = json.Unmarshal(b, &c)
	}
	return c
}

func (e *Engine) putNodeControl(ctx context.Context, graphID, node string, c nodeControl) error {
	b, _ := json.Marshal(c)
	return e.store.KVPut(ctx, controlNS(graphID), "node/"+node, b)
}

func doneKey(node string, replica int) string {
	return "done/" + domain.ReplicaKey("", node, replica)[1:]
}

func (e *Engine) markCompleted(ctx context.Context, graphID, node string, replica int, runID string) {
	_ = e.store.KVPut(ctx, controlNS(graphID), doneKey(node, replica), []byte(runID))
}

func (e *Engine) isCompleted(ctx context.Context, graphID, node string, replica int, runID string) bool {
	b, err := e.store.KVGet(ctx, controlNS(graphID), doneKey(node, replica))
	return err == nil && string(b) == runID
}

func (r *graphRun) setPaused(p bool) {
	r.mu.Lock()
	changed := r.paused != p
	r.paused = p
	reps := r.replicaList()
	r.mu.Unlock()
	if changed {
		for _, rep := range reps {
			rep.poke()
		}
	}
}

func (r *graphRun) isPaused() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.paused
}

func (r *graphRun) replicaList() []*replica {
	out := make([]*replica, 0, len(r.replicas))
	for _, rep := range r.replicas {
		out = append(out, rep)
	}
	return out
}

// replicasFor returns a replica-count function for g that honours operator
// scale overrides, so broadcast channels get a group for every replica that
// can actually run.
func (e *Engine) replicasFor(ctx context.Context, g *domain.Graph) func(string) int {
	return func(node string) int {
		n := g.Node(node)
		if n != nil && n.Cluster.Mode != domain.ClusterAutoscale {
			if c := e.getNodeControl(ctx, g.Metadata.ID, node); c.Replicas > 0 {
				return c.Replicas
			}
		}
		return replicasOf(g, node)
	}
}

// replicaCount is the number of replicas a node is configured for right
// now (used to declare broadcast groups and to tell agents their cluster size).
func (r *graphRun) replicaCount(node string) int {
	n := r.g.Node(node)
	if n == nil {
		return 0
	}
	if n.Cluster.Mode == domain.ClusterAutoscale {
		return n.Cluster.MaxReplicas
	}
	c := r.e.getNodeControl(context.Background(), r.g.Metadata.ID, node)
	if c.Replicas > 0 {
		return c.Replicas
	}
	return n.Cluster.Replicas
}

// declareTopology creates every subject and consumer group the graph needs.
// It is idempotent and additive: groups that disappear from the design are
// not deleted here, because they may still hold work (see DeleteGraph).
func (r *graphRun) declareTopology(ctx context.Context) error {
	return r.e.declareTopology(ctx, r.g, r.replicaCount)
}

func (e *Engine) declareTopology(ctx context.Context, g *domain.Graph, replicas func(string) int) error {
	for i := range g.Spec.Channels {
		c := &g.Spec.Channels[i]
		groups := graph.SubjectGroups(g, c, replicas)
		if len(groups) == 0 {
			// Keep the subject alive with a parking group so injected or
			// emitted messages are retained until a consumer is wired.
			groups = []string{c.ID + ".unconsumed"}
		}
		err := e.opt.Broker.Declare(ctx, queue.SubjectConfig{
			Subject:    queue.Subject(g.Metadata.ID, c.ID),
			Groups:     groups,
			MaxDeliver: c.MaxDeliver,
			AckWait:    c.AckWait.D(),
			Retention:  c.Retention.D(),
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// structuralKey captures the parts of a node that decide what a replica
// *is*. Changing them restarts replicas; anything else (prompt, model,
// tools, loop settings, approvals) is picked up live on the next turn.
func structuralKey(n *domain.Node) string {
	b, _ := json.Marshal(struct {
		R domain.RuntimeRef
		I domain.IsolationSpec
		W domain.NodeWorkspace
		C domain.ClusterComms
	}{n.Runtime, n.Isolation, n.Workspace, n.Cluster.Comms})
	return string(b)
}

// adopt moves the replicas of a previous version into this run.
func (r *graphRun) adopt(ctx context.Context, old *graphRun) {
	old.mu.Lock()
	reps := old.replicaList()
	old.replicas = map[string]*replica{}
	old.mu.Unlock()
	sameRun := old.runID == r.runID
	for _, rep := range reps {
		oldNode := old.g.Node(rep.nodeID)
		newNode := r.g.Node(rep.nodeID)
		if !sameRun || newNode == nil || oldNode == nil || structuralKey(oldNode) != structuralKey(newNode) {
			go rep.stop(false)
			continue
		}
		rep.setRun(r)
		r.mu.Lock()
		r.replicas[rep.key()] = rep
		r.mu.Unlock()
		rep.poke()
	}
}

// reconcile starts, renews and stops this instance's replicas.
func (r *graphRun) reconcile(ctx context.Context) error {
	e := r.e
	gid := r.g.Metadata.ID
	ttl := e.opt.LeaseTTL
	wanted := map[string]bool{}
	for i := range r.g.Spec.Nodes {
		n := &r.g.Spec.Nodes[i]
		desired := r.desiredReplicas(ctx, n)
		ctl := e.getNodeControl(ctx, gid, n.ID)
		for idx := 0; idx < desired; idx++ {
			key := n.ID + "#" + itoa(idx)
			wanted[key] = true
			r.mu.Lock()
			rep := r.replicas[key]
			r.mu.Unlock()
			lk := domain.ReplicaKey(gid, n.ID, idx)
			if rep != nil {
				if rep.finished() {
					r.mu.Lock()
					delete(r.replicas, key)
					r.mu.Unlock()
					_ = e.store.ReleaseLease(ctx, lk, e.opt.Instance)
					continue
				}
				ok, err := e.store.AcquireLease(ctx, lk, e.opt.Instance, ttl)
				if err == nil && !ok {
					e.log.Warn("lost lease; stopping replica", "replica", lk)
					go rep.stop(true)
				}
				rep.setNodePaused(ctl.Paused)
				continue
			}
			if e.isCompleted(ctx, gid, n.ID, idx, r.runID) {
				continue
			}
			ok, err := e.store.AcquireLease(ctx, lk, e.opt.Instance, ttl)
			if err != nil || !ok {
				continue // another instance runs it
			}
			// We now own the replica. Anything the previous owner left
			// running is interrupted - its inputs come back via redelivery.
			e.interruptStale(ctx, gid, n.ID, idx)
			rep = newReplica(r, n.ID, idx)
			rep.setNodePaused(ctl.Paused)
			r.mu.Lock()
			r.replicas[key] = rep
			r.mu.Unlock()
			go rep.run()
		}
	}
	// Replicas no longer wanted.
	r.mu.Lock()
	var surplus []*replica
	for key, rep := range r.replicas {
		if !wanted[key] {
			n := r.g.Node(rep.nodeID)
			// Autoscaled replicas are shed only once idle long enough, and
			// never mid-turn: a replica holding a claimed message finishes it.
			if n != nil && n.Cluster.Mode == domain.ClusterAutoscale && !n.Disabled && !rep.idleFor(n.Cluster.ScaleDownAfter.D()) {
				continue
			}
			surplus = append(surplus, rep)
			delete(r.replicas, key)
		}
	}
	r.mu.Unlock()
	for _, rep := range surplus {
		go func(rep *replica) {
			rep.stop(false)
			_ = e.store.ReleaseLease(context.Background(), domain.ReplicaKey(gid, rep.nodeID, rep.index), e.opt.Instance)
		}(rep)
	}
	r.updateRecord(ctx)
	return nil
}

func (r *graphRun) desiredReplicas(ctx context.Context, n *domain.Node) int {
	if n.Disabled {
		return 0
	}
	ctl := r.e.getNodeControl(ctx, r.g.Metadata.ID, n.ID)
	if ctl.Stopped {
		return 0
	}
	switch n.Cluster.Mode {
	case domain.ClusterAutoscale:
		var backlog int64
		for _, gr := range graph.ConsumerGroups(r.g, n.ID, 0) {
			st, err := r.e.opt.Broker.Stats(ctx, gr.Subject, gr.Group)
			if err == nil {
				backlog += st.Pending + st.InFlight
				observability.QueuePending.WithLabelValues(r.g.Metadata.ID, gr.Channel, gr.Group).Set(float64(st.Pending))
			}
		}
		want := int(backlog)
		if want < n.Cluster.MinReplicas {
			want = n.Cluster.MinReplicas
		}
		if want > n.Cluster.MaxReplicas {
			want = n.Cluster.MaxReplicas
		}
		return want
	default:
		if ctl.Replicas > 0 {
			return ctl.Replicas
		}
		return n.Cluster.Replicas
	}
}

// updateRecord reflects the run's actual state into the graph record.
func (r *graphRun) updateRecord(ctx context.Context) {
	rec, err := r.e.store.GetGraphRecord(ctx, r.g.Metadata.ID)
	if err != nil {
		return
	}
	want := domain.GraphRunning
	if rec.DesiredState == domain.GraphPaused {
		want = domain.GraphPaused
	}
	if rec.DesiredState == domain.GraphStopped || rec.Version != r.version || rec.RunID != r.runID {
		return
	}
	if rec.State != want {
		r.e.updateObservedState(ctx, rec, want)
	}
}

// stopAll stops every local replica. kill cancels in-flight turns.
func (r *graphRun) stopAll(ctx context.Context, kill bool) {
	r.mu.Lock()
	reps := r.replicaList()
	r.replicas = map[string]*replica{}
	r.mu.Unlock()
	var wg sync.WaitGroup
	for _, rep := range reps {
		wg.Add(1)
		go func(rep *replica) {
			defer wg.Done()
			done := make(chan struct{})
			go func() { rep.stop(kill); close(done) }()
			select {
			case <-done:
			case <-ctx.Done():
				rep.stop(true)
				<-done
			}
			_ = r.e.store.ReleaseLease(context.Background(), domain.ReplicaKey(r.g.Metadata.ID, rep.nodeID, rep.index), r.e.opt.Instance)
		}(rep)
	}
	wg.Wait()
}

// local replicas of one node (for node-level kill/pause).
func (r *graphRun) nodeReplicas(node string) []*replica {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*replica
	for _, rep := range r.replicas {
		if rep.nodeID == node {
			out = append(out, rep)
		}
	}
	return out
}

func (e *Engine) interruptStale(ctx context.Context, graphID, node string, replica int) {
	execs, err := e.store.ListExecutions(ctx, storage.ExecutionFilter{GraphID: graphID, NodeID: node, Limit: 200})
	if err != nil {
		return
	}
	for _, x := range execs {
		if x.Replica == replica && !x.Status.Terminal() && x.Instance != e.opt.Instance {
			e.interrupt(ctx, x, "replica moved to "+e.opt.Instance+" after its previous instance stopped renewing the lease")
		}
	}
}

func itoa(i int) string { return strconv.Itoa(i) }
