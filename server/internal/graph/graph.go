// Package graph turns a graph document into something the engine can run:
// it normalises defaults, validates structure and semantics, compiles the
// routing conditions, and derives the broker topology (subjects and consumer
// groups) that implements each channel's delivery semantics.
package graph

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/queue"
)

// DefaultMaxHops bounds message chains in cyclic graphs.
const DefaultMaxHops = 64

var idRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// Slug turns a display name into a valid id.
func Slug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	dash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		default:
			if !dash && b.Len() > 0 {
				b.WriteByte('-')
				dash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 63 {
		out = strings.Trim(out[:63], "-")
	}
	if out == "" {
		out = "x"
	}
	return out
}

// Normalize fills defaults in place. It is idempotent and never overrides an
// explicit value. Defaults are chosen so that the shortest useful document -
// nodes with prompts and channels with from/to - runs.
func Normalize(g *domain.Graph) {
	if g.APIVersion == "" {
		g.APIVersion = domain.APIVersion
	}
	if g.Kind == "" {
		g.Kind = "Graph"
	}
	if g.Metadata.ID == "" {
		g.Metadata.ID = Slug(g.Metadata.Name)
	}
	if g.Metadata.Name == "" {
		g.Metadata.Name = g.Metadata.ID
	}
	if g.Spec.MaxHops <= 0 {
		g.Spec.MaxHops = DefaultMaxHops
	}
	d := g.Spec.Defaults
	for i := range g.Spec.Nodes {
		n := &g.Spec.Nodes[i]
		if n.ID == "" {
			n.ID = Slug(n.Name)
		}
		if n.Runtime.Kind == "" {
			n.Runtime = mergeRuntime(n.Runtime, d.Runtime)
			if n.Runtime.Kind == "" {
				n.Runtime.Kind = domain.RuntimeNative
			}
		}
		if n.Runtime.Kind == domain.RuntimeExternal && n.Runtime.Agent == "" {
			n.Runtime.Agent = n.ID
		}
		if n.Model.Provider == "" {
			n.Model.Provider = d.Model.Provider
		}
		if n.Model.Name == "" {
			n.Model.Name = d.Model.Name
		}
		if n.Model.Temperature == nil {
			n.Model.Temperature = d.Model.Temperature
		}
		if n.Model.MaxTokens == 0 {
			n.Model.MaxTokens = d.Model.MaxTokens
		}
		if n.Tools == nil && d.Tools != nil {
			n.Tools = append([]domain.ToolGrant(nil), d.Tools...)
		}
		if n.Isolation.Mode == "" {
			n.Isolation = d.Isolation
			if n.Isolation.Mode == "" {
				n.Isolation.Mode = domain.IsolationProcess
			}
		}
		if n.Loop.Mode == "" {
			if d.Loop != nil && d.Loop.Mode != "" {
				n.Loop = *d.Loop
			} else if len(g.Incoming(n.ID)) > 0 {
				// A consumer waits for work by default: spending turns on an
				// empty queue is the most common way a factory burns money.
				n.Loop.Mode = domain.LoopEvent
			} else {
				n.Loop.Mode = domain.LoopOnce
			}
		}
		if n.Loop.Stop.Policy == "" {
			switch {
			case n.Loop.Stop.MaxIterations > 0:
				n.Loop.Stop.Policy = domain.StopIterations
			case n.Loop.Stop.MaxDuration > 0:
				n.Loop.Stop.Policy = domain.StopDuration
			case n.Loop.Stop.IdleTimeout > 0:
				n.Loop.Stop.Policy = domain.StopIdle
			default:
				n.Loop.Stop.Policy = domain.StopNever
			}
		}
		if n.Loop.BatchSize <= 0 {
			n.Loop.BatchSize = 1
		}
		if n.Cluster.Mode == "" {
			n.Cluster.Mode = domain.ClusterFixed
		}
		if n.Cluster.Comms == "" {
			n.Cluster.Comms = domain.CommsIsolated
		}
		if n.Cluster.Mode == domain.ClusterFixed && n.Cluster.Replicas <= 0 {
			n.Cluster.Replicas = 1
		}
		if n.Cluster.Mode == domain.ClusterAutoscale {
			if n.Cluster.MaxReplicas <= 0 {
				n.Cluster.MaxReplicas = max(n.Cluster.Replicas, 4)
			}
			if n.Cluster.MinReplicas <= 0 {
				// One replica stays live so a started component never reports
				// stopped while it waits for its first item.
				n.Cluster.MinReplicas = 1
			}
			if n.Cluster.ScaleDownAfter == 0 {
				n.Cluster.ScaleDownAfter = domain.Duration(60e9)
			}
		}
		if n.Workspace.Mode == "" {
			n.Workspace.Mode = domain.WorkspaceShared
		}
		if n.Limits.MaxToolCalls == 0 {
			n.Limits.MaxToolCalls = 50
		}
		if n.Limits.TurnTimeout == 0 {
			n.Limits.TurnTimeout = domain.Duration(30 * 60e9)
		}
	}
	for i := range g.Spec.Channels {
		c := &g.Spec.Channels[i]
		if c.Type == "" {
			c.Type = domain.ChannelQueue
		}
		if c.ID == "" {
			to := strings.Join(c.To, "-")
			if c.From == "" {
				c.ID = Slug("in-" + to)
			} else {
				c.ID = Slug(c.From + "-to-" + to)
			}
		}
		if c.MaxDeliver <= 0 {
			c.MaxDeliver = 5
		}
		if c.AckWait == 0 {
			c.AckWait = domain.Duration(10 * 60e9)
		}
	}
}

func mergeRuntime(n, d domain.RuntimeRef) domain.RuntimeRef {
	if n.Kind == "" {
		n.Kind = d.Kind
	}
	if n.Framework == "" {
		n.Framework = d.Framework
	}
	if n.Endpoint == "" {
		n.Endpoint = d.Endpoint
	}
	if n.Image == "" {
		n.Image = d.Image
	}
	if len(n.Command) == 0 {
		n.Command = d.Command
	}
	if n.Env == nil {
		n.Env = d.Env
	}
	return n
}

// Issue is a validation finding with a JSON-path-like location.
type Issue struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

// Report is the result of Validate.
type Report struct {
	Valid    bool    `json:"valid"`
	Errors   []Issue `json:"errors"`
	Warnings []Issue `json:"warnings"`
}

func (r *Report) errf(path, f string, a ...any) {
	r.Errors = append(r.Errors, Issue{path, fmt.Sprintf(f, a...)})
}
func (r *Report) warnf(path, f string, a ...any) {
	r.Warnings = append(r.Warnings, Issue{path, fmt.Sprintf(f, a...)})
}

// Err returns the report as an error when invalid.
func (r *Report) Err() error {
	if r.Valid {
		return nil
	}
	parts := make([]string, 0, len(r.Errors))
	for _, e := range r.Errors {
		parts = append(parts, e.Path+": "+e.Message)
	}
	return fmt.Errorf("invalid graph: %s", strings.Join(parts, "; "))
}

// reserved prompt names cannot be parameters.
var reserved = map[string]bool{"input": true, "output": true, "project": true, "workspace": true, "loop": true, "self": true}

// Validate normalises a copy-safe graph in place and checks it. Cycles are
// legal (they are how review loops are expressed); what is checked is that
// every reference resolves, every condition compiles, and that settings
// that cannot mean anything together are rejected rather than guessed at.
func Validate(g *domain.Graph, knownRuntimes map[domain.RuntimeKind]bool) *Report {
	Normalize(g)
	r := &Report{}
	if g.APIVersion != domain.APIVersion {
		r.errf("apiVersion", "unsupported apiVersion %q (want %q)", g.APIVersion, domain.APIVersion)
	}
	if g.Kind != "Graph" {
		r.errf("kind", "kind must be Graph")
	}
	if !idRe.MatchString(g.Metadata.ID) {
		r.errf("metadata.id", "%q must be a DNS label (lowercase letters, digits, '-')", g.Metadata.ID)
	}
	if len(g.Spec.Nodes) == 0 {
		r.errf("spec.nodes", "a graph needs at least one node")
	}
	params := map[string]bool{}
	for i, p := range g.Spec.Parameters {
		path := fmt.Sprintf("spec.parameters[%d]", i)
		if !regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`).MatchString(p.Name) {
			r.errf(path+".name", "invalid parameter name %q", p.Name)
		}
		if reserved[p.Name] {
			r.errf(path+".name", "%q is reserved", p.Name)
		}
		if params[p.Name] {
			r.errf(path+".name", "duplicate parameter %q", p.Name)
		}
		params[p.Name] = true
	}
	if repo := g.Spec.Workspace.Repo; repo != "" {
		switch RepoKind(repo) {
		case RepoRemote:
		case RepoInsecure:
			r.warnf("spec.workspace.repo", "%q is fetched over unencrypted http; use https", repo)
		default:
			r.errf("spec.workspace.repo", "%q is not a remote repository URL: use https://, ssh:// or user@host:path (local paths and other transports are refused)", repo)
		}
	}
	if env := g.Spec.Environment; env != nil {
		validateEnvironment(r, env)
	}
	if err := domain.ValidateChecks(g.Spec.Checks); err != nil {
		r.errf("spec.checks", "%v", err)
	}
	nodes := map[string]*domain.Node{}
	for i := range g.Spec.Nodes {
		n := &g.Spec.Nodes[i]
		path := fmt.Sprintf("spec.nodes[%d]", i)
		if !idRe.MatchString(n.ID) {
			r.errf(path+".id", "%q must be a DNS label", n.ID)
		}
		if nodes[n.ID] != nil {
			r.errf(path+".id", "duplicate node id %q", n.ID)
		}
		nodes[n.ID] = n
		if knownRuntimes != nil && !knownRuntimes[n.Runtime.Kind] {
			r.errf(path+".runtime.kind", "unknown runtime %q", n.Runtime.Kind)
		}
		if n.Runtime.Kind == domain.RuntimeExternal && n.Runtime.Endpoint == "" && len(n.Runtime.Command) == 0 && n.Runtime.Image == "" {
			r.errf(path+".runtime", "external runtime needs endpoint, command or image")
		}
		if n.Runtime.Kind == domain.RuntimeNative && strings.TrimSpace(n.Prompt) == "" {
			r.errf(path+".prompt", "native agents need a prompt")
		}
		switch n.Loop.Mode {
		case domain.LoopOnce, domain.LoopContinuous, domain.LoopEvent:
		case domain.LoopScheduled:
			if n.Loop.Schedule == "" {
				r.errf(path+".loop.schedule", "scheduled loops need a schedule")
			} else if _, err := ParseSchedule(n.Loop.Schedule); err != nil {
				r.errf(path+".loop.schedule", "%v", err)
			}
		default:
			r.errf(path+".loop.mode", "unknown loop mode %q", n.Loop.Mode)
		}
		switch n.Loop.Stop.Policy {
		case domain.StopNever, domain.StopAuto:
		case domain.StopIterations:
			if n.Loop.Stop.MaxIterations <= 0 {
				r.errf(path+".loop.stop.maxIterations", "must be > 0 for policy iterations")
			}
		case domain.StopDuration:
			if n.Loop.Stop.MaxDuration <= 0 {
				r.errf(path+".loop.stop.maxDuration", "must be > 0 for policy duration")
			}
		case domain.StopIdle:
			if n.Loop.Stop.IdleTimeout <= 0 {
				r.errf(path+".loop.stop.idleTimeout", "must be > 0 for policy idle")
			}
		default:
			r.errf(path+".loop.stop.policy", "unknown stop policy %q", n.Loop.Stop.Policy)
		}
		// Waiting for work is a claim that more is coming; auto-stop is a claim
		// that the work can finish. Both at once describes nothing.
		if n.Loop.Mode == domain.LoopEvent && n.Loop.Stop.Policy == domain.StopAuto {
			r.errf(path+".loop", "event mode and auto stop are mutually exclusive")
		}
		switch n.Cluster.Mode {
		case domain.ClusterFixed:
			if n.Cluster.Replicas < 1 || n.Cluster.Replicas > 256 {
				r.errf(path+".cluster.replicas", "must be between 1 and 256")
			}
		case domain.ClusterAutoscale:
			if n.Cluster.MinReplicas > n.Cluster.MaxReplicas {
				r.errf(path+".cluster", "minReplicas > maxReplicas")
			}
			if n.Cluster.MaxReplicas > 256 {
				r.errf(path+".cluster.maxReplicas", "must be <= 256")
			}
			if n.Loop.Mode != domain.LoopEvent {
				// Only idle replicas are shed; a replica that never idles never shrinks.
				r.warnf(path+".loop.mode", "autoscaled clusters should use loop mode event, or they grow to the peak and stay there")
			}
			if len(g.Incoming(n.ID)) == 0 {
				r.warnf(path+".cluster", "autoscaled node has no inputs; it will run minReplicas")
			}
		default:
			r.errf(path+".cluster.mode", "unknown cluster mode %q", n.Cluster.Mode)
		}
		if n.Cluster.Comms != domain.CommsIsolated && n.Cluster.Comms != domain.CommsRing {
			r.errf(path+".cluster.comms", "unknown comms %q", n.Cluster.Comms)
		}
		switch n.Isolation.Mode {
		case domain.IsolationProcess, domain.IsolationDocker, domain.IsolationKubernetes:
		default:
			r.errf(path+".isolation.mode", "unknown isolation %q", n.Isolation.Mode)
		}
		if n.Workspace.ReadOnly && n.Runtime.Kind != domain.RuntimeNative {
			r.errf(path+".workspace.readOnly", "read-only workspaces require the native runtime; external runtimes may write outside the tool gateway")
		}
		if n.Workspace.Mode != domain.WorkspaceShared && n.Workspace.Mode != domain.WorkspaceWorktree {
			r.errf(path+".workspace.mode", "unknown workspace mode %q", n.Workspace.Mode)
		}
		if n.Workspace.Mode == domain.WorkspaceWorktree && !g.Spec.Workspace.Git && g.Spec.Workspace.Repo == "" {
			r.warnf(path+".workspace.mode", "worktree mode needs a git workspace; spec.workspace.git will be assumed")
		}
		for j, t := range n.Tools {
			if t.Name == "" {
				r.errf(fmt.Sprintf("%s.tools[%d]", path, j), "tool grant needs a name")
			}
			if t.Name == "*" {
				r.warnf(fmt.Sprintf("%s.tools[%d]", path, j), "grants every tool, including shell.exec and git.write")
			}
		}
		for _, tok := range PromptTokens(n.Prompt) {
			if reserved[tok] || params[tok] {
				continue
			}
			r.warnf(path+".prompt", "@%s is neither a parameter nor a reserved name; it is left as written", tok)
		}
		for j, s := range n.Subscribe {
			if s == "" {
				r.errf(fmt.Sprintf("%s.subscribe[%d]", path, j), "empty topic pattern")
			}
		}
	}
	chans := map[string]bool{}
	for i := range g.Spec.Channels {
		c := &g.Spec.Channels[i]
		path := fmt.Sprintf("spec.channels[%d]", i)
		if !regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,62}$`).MatchString(c.ID) {
			r.errf(path+".id", "%q must be lowercase letters, digits, '-' or '_'", c.ID)
		}
		if chans[c.ID] {
			r.errf(path+".id", "duplicate channel id %q", c.ID)
		}
		chans[c.ID] = true
		switch c.Type {
		case domain.ChannelQueue, domain.ChannelTopic, domain.ChannelFanout, domain.ChannelBroadcast:
		default:
			r.errf(path+".type", "unknown channel type %q", c.Type)
		}
		if c.From != "" && nodes[c.From] == nil {
			r.errf(path+".from", "unknown node %q", c.From)
		}
		for j, to := range c.To {
			if nodes[to] == nil {
				r.errf(fmt.Sprintf("%s.to[%d]", path, j), "unknown node %q", to)
			}
		}
		if len(g.Consumers(c)) == 0 {
			r.warnf(path, "channel has no consumers; messages will accumulate until one subscribes")
		}
		if c.When != "" {
			if _, err := CompileCondition(c.When); err != nil {
				r.errf(path+".when", "%v", err)
			}
		}
	}
	validateInstructions(g, r)
	r.Valid = len(r.Errors) == 0
	if r.Errors == nil {
		r.Errors = []Issue{}
	}
	if r.Warnings == nil {
		r.Warnings = []Issue{}
	}
	return r
}

// Group is one consumer group a node replica reads from.
type Group struct {
	Channel string
	Subject string
	Group   string
	Node    string
	Replica int // -1 unless broadcast
}

// ConsumerGroups returns the groups a given replica of node consumes. This
// is where channel semantics become broker topology (see package queue).
func ConsumerGroups(g *domain.Graph, nodeID string, replica int) []Group {
	var out []Group
	for _, c := range g.Incoming(nodeID) {
		gr := Group{Channel: c.ID, Subject: queue.Subject(g.Metadata.ID, c.ID), Node: nodeID, Replica: -1}
		switch c.Type {
		case domain.ChannelQueue:
			gr.Group = queue.QueueGroup(c.ID)
		case domain.ChannelTopic, domain.ChannelFanout:
			gr.Group = queue.NodeGroup(c.ID, nodeID)
		case domain.ChannelBroadcast:
			gr.Group = queue.ReplicaGroup(c.ID, nodeID, replica)
			gr.Replica = replica
		}
		out = append(out, gr)
	}
	return out
}

// SubjectGroups lists every group a channel must deliver to, given the
// current replica counts (broadcast groups depend on them).
func SubjectGroups(g *domain.Graph, c *domain.Channel, replicas func(node string) int) []string {
	set := map[string]bool{}
	for _, node := range g.Consumers(c) {
		switch c.Type {
		case domain.ChannelQueue:
			set[queue.QueueGroup(c.ID)] = true
		case domain.ChannelTopic, domain.ChannelFanout:
			set[queue.NodeGroup(c.ID, node)] = true
		case domain.ChannelBroadcast:
			n := replicas(node)
			for i := 0; i < n; i++ {
				set[queue.ReplicaGroup(c.ID, node, i)] = true
			}
		}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

var tokenRe = regexp.MustCompile(`@([A-Za-z_][A-Za-z0-9_-]*)`)

// PromptTokens lists @names used in a prompt.
func PromptTokens(prompt string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range tokenRe.FindAllStringSubmatch(prompt, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	return out
}

// Substitute replaces @param tokens with their values. A parameter with no
// value keeps its token, so "research @topic" never silently turns into
// "research " - the system preamble tells the agent the parameter is unset.
// Reserved names (@input, @output, @workspace, ...) are left in place and
// defined by the preamble instead, because they stand for structures too
// large to inline into a sentence.
func Substitute(prompt string, params map[string]string) string {
	return tokenRe.ReplaceAllStringFunc(prompt, func(tok string) string {
		name := tok[1:]
		if v, ok := params[name]; ok && v != "" {
			return v
		}
		return tok
	})
}

// Params returns parameters as a map.
func Params(g *domain.Graph) map[string]string {
	m := make(map[string]string, len(g.Spec.Parameters))
	for _, p := range g.Spec.Parameters {
		m[p.Name] = p.Value
	}
	return m
}
