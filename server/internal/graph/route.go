package graph

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
	"github.com/robfig/cron/v3"

	"github.com/woliveiras/weft/server/internal/domain"
)

// Condition is a compiled `when` expression. Expressions use expr-lang
// syntax over a sandboxed environment {type, source, target, payload,
// metadata}: no I/O, no function calls beyond the builtins, bounded
// execution. That keeps dynamic branching declarative and safe to evaluate
// inside the control plane.
type Condition struct {
	src string
	prg *vm.Program
}

type condEnv struct {
	Type     string         `expr:"type"`
	Source   string         `expr:"source"`
	Target   string         `expr:"target"`
	Payload  map[string]any `expr:"payload"`
	Metadata map[string]any `expr:"metadata"`
}

// CompileCondition parses and type-checks a `when` expression.
func CompileCondition(src string) (*Condition, error) {
	prg, err := expr.Compile(src, expr.Env(condEnv{}), expr.AsBool())
	if err != nil {
		return nil, fmt.Errorf("invalid condition: %w", err)
	}
	return &Condition{src: src, prg: prg}, nil
}

// Eval evaluates the condition for a message. Evaluation errors (missing
// keys used in arithmetic, etc.) evaluate to false: a branch that cannot be
// decided is not taken, and the message is recorded as dropped if no other
// branch matches - visible, rather than delivered somewhere surprising.
func (c *Condition) Eval(m *domain.Message) (bool, error) {
	env := condEnv{Type: m.Type, Source: m.Source, Target: m.Target, Metadata: m.Metadata}
	if len(m.Payload) > 0 {
		var p any
		if err := json.Unmarshal(m.Payload, &p); err == nil {
			if pm, ok := p.(map[string]any); ok {
				env.Payload = pm
			} else {
				env.Payload = map[string]any{"value": p}
			}
		}
	}
	if env.Payload == nil {
		env.Payload = map[string]any{}
	}
	if env.Metadata == nil {
		env.Metadata = map[string]any{}
	}
	out, err := expr.Run(c.prg, env)
	if err != nil {
		return false, err
	}
	b, _ := out.(bool)
	return b, nil
}

func (c *Condition) String() string { return c.src }

// Router decides which channels an emitted message goes to.
type Router struct {
	g     *domain.Graph
	mu    sync.Mutex
	conds map[string]*Condition
}

func NewRouter(g *domain.Graph) (*Router, error) {
	r := &Router{g: g, conds: map[string]*Condition{}}
	for _, c := range g.Spec.Channels {
		if c.When == "" {
			continue
		}
		cond, err := CompileCondition(c.When)
		if err != nil {
			return nil, fmt.Errorf("channel %s: %w", c.ID, err)
		}
		r.conds[c.ID] = cond
	}
	return r, nil
}

// RouteResult explains a routing decision (recorded for observability).
type RouteResult struct {
	Channels []*domain.Channel
	Reason   string // why nothing matched, when Channels is empty
}

// Route picks channels for a message emitted by node:
//
//  1. channel named explicitly  → that channel (must be one of node's outputs)
//  2. target named              → node's outputs that reach target
//  3. otherwise                 → every output whose type filter and
//     condition match (fan-out across branches is intentional)
//
// Type filters and conditions apply in cases 2 and 3; in case 1 the author
// of the agent chose the channel and the filters are checked as a guard.
func (r *Router) Route(node string, m *domain.Message, channel string) RouteResult {
	outs := r.g.Outgoing(node)
	if len(outs) == 0 {
		return RouteResult{Reason: "node has no outgoing channels"}
	}
	var cands []*domain.Channel
	switch {
	case channel != "":
		for _, c := range outs {
			if c.ID == channel {
				cands = append(cands, c)
			}
		}
		if len(cands) == 0 {
			return RouteResult{Reason: fmt.Sprintf("channel %q is not an output of %s", channel, node)}
		}
	case m.Target != "":
		for _, c := range outs {
			for _, consumer := range r.g.Consumers(c) {
				if consumer == m.Target {
					cands = append(cands, c)
					break
				}
			}
		}
		if len(cands) == 0 {
			return RouteResult{Reason: fmt.Sprintf("no output of %s reaches %q", node, m.Target)}
		}
	default:
		cands = outs
	}
	var out []*domain.Channel
	var rejected []string
	for _, c := range cands {
		if !typeMatches(c.Types, m.Type) {
			rejected = append(rejected, c.ID+": type filter")
			continue
		}
		if cond := r.conds[c.ID]; cond != nil {
			ok, err := cond.Eval(m)
			if err != nil || !ok {
				reason := c.ID + ": condition false"
				if err != nil {
					reason = c.ID + ": condition error: " + err.Error()
				}
				rejected = append(rejected, reason)
				continue
			}
		}
		out = append(out, c)
	}
	if len(out) == 0 {
		return RouteResult{Reason: "no channel matched (" + strings.Join(rejected, "; ") + ")"}
	}
	return RouteResult{Channels: out}
}

func typeMatches(filters []string, typ string) bool {
	if len(filters) == 0 {
		return true
	}
	for _, f := range filters {
		if domain.GlobMatch(f, typ) {
			return true
		}
	}
	return false
}

// Schedule is a parsed loop schedule.
type Schedule interface {
	Next(time.Time) time.Time
}

var cronParser = cron.NewParser(cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

// ParseSchedule accepts 5/6-field cron expressions and descriptors
// ("@hourly", "@every 15m").
func ParseSchedule(s string) (Schedule, error) {
	sch, err := cronParser.Parse(s)
	if err != nil {
		return nil, fmt.Errorf("invalid schedule %q: %w", s, err)
	}
	return sch, nil
}
