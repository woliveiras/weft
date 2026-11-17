package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/graph"
	"github.com/woliveiras/weft/server/internal/observability"
	"github.com/woliveiras/weft/server/internal/queue"
	"github.com/woliveiras/weft/server/internal/runtime"
)

// UnroutedChannel is the history pseudo-channel for dropped messages.
const UnroutedChannel = "_unrouted"

// reserved metadata keys an agent may not set through Emit.Metadata.
var reservedMeta = map[string]bool{
	domain.MetaGraph: true, domain.MetaGraphVersion: true, domain.MetaChannel: true, domain.MetaRunID: true,
	domain.MetaCausationID: true, domain.MetaExecutionID: true, domain.MetaHops: true, domain.MetaCreatedAt: true,
	domain.MetaTraceParent: true, domain.MetaTraceState: true, domain.MetaInjectedBy: true, domain.MetaAttempt: true,
	domain.MetaReplica: true, domain.MetaHeldFor: true,
}

// emit turns one runtime.Emit into a message, routes it and publishes it on
// every matching channel. It returns the ids published (one message id per
// emit; the same message goes to several channels).
func (e *Engine) emit(ctx context.Context, gr *graphRun, n *domain.Node, replica int, x *domain.Execution, parent *domain.Message, em runtime.Emit) ([]string, error) {
	var m *domain.Message
	var err error
	if parent != nil {
		m, err = parent.Child(em.Type, n.ID, em.Target, em.Payload)
	} else {
		m, err = domain.NewMessage(em.Type, n.ID, em.Target, em.Payload)
	}
	if err != nil {
		return nil, err
	}
	for k, v := range em.Metadata {
		if !reservedMeta[k] {
			m.SetMeta(k, v)
		}
	}
	gid := gr.g.Metadata.ID
	m.SetMeta(domain.MetaGraph, gid)
	m.SetMeta(domain.MetaGraphVersion, gr.version)
	m.SetMeta(domain.MetaRunID, gr.runID)
	m.SetMeta(domain.MetaExecutionID, x.ID)
	m.SetMeta(domain.MetaReplica, replica)
	observability.Inject(ctx, m.Metadata)

	if hops := m.Hops(); hops > gr.g.Spec.MaxHops {
		e.drop(ctx, gid, n.ID, m, fmt.Sprintf("hop limit %d exceeded (cycle guard)", gr.g.Spec.MaxHops), "hop_limit")
		return nil, nil
	}
	res := gr.router.Route(n.ID, m, em.Channel)
	if len(res.Channels) == 0 {
		e.drop(ctx, gid, n.ID, m, res.Reason, "no_route")
		return nil, nil
	}
	var ids []string
	for _, c := range res.Channels {
		gate := c.Approval || n.Approval.BeforeEmit
		if err := e.publish(ctx, gr.g, c, m.Clone(), gate, n.Approval); err != nil {
			return ids, err
		}
		ids = []string{m.ID}
	}
	return ids, nil
}

func (e *Engine) drop(ctx context.Context, gid, node string, m *domain.Message, reason, metric string) {
	observability.MessagesDropped.WithLabelValues(gid, node, metric).Inc()
	// Unrouted messages are kept in history under a pseudo-channel so that
	// "where did my message go" always has an answer.
	e.recordMessage(ctx, gid, UnroutedChannel, "", m, domain.MsgDropped, 0, m.MetaString(domain.MetaExecutionID), reason)
	e.bus.Publish(ctx, domain.NewEvent(domain.EvMsgDropped, gid, map[string]any{"type": m.Type, "reason": reason}).
		WithNode(node, m.MetaInt(domain.MetaReplica)).WithMessage(m.ID))
}

// publish puts m on channel c, or holds it at an approval gate.
func (e *Engine) publish(ctx context.Context, g *domain.Graph, c *domain.Channel, m *domain.Message, gate bool, ap domain.ApprovalSpec) error {
	gid := g.Metadata.ID
	m.SetMeta(domain.MetaChannel, c.ID)
	if gate {
		a, err := e.opt.Approvals.Create(ctx, &domain.Approval{
			GraphID: gid, NodeID: m.Source, Replica: m.MetaInt(domain.MetaReplica), ExecutionID: m.MetaString(domain.MetaExecutionID),
			Kind:      domain.ApprovalMessage,
			Title:     fmt.Sprintf("%s → %s: %s", displaySource(m.Source), c.ID, m.Type),
			Detail:    mustJSON(heldMessage{GraphID: gid, Channel: c.ID, Message: m, OnTimeout: ap.OnTimeout}),
			ExpiresAt: expiry(ap.Timeout),
		})
		if err != nil {
			return err
		}
		m.SetMeta(domain.MetaHeldFor, a.ID)
		e.recordMessage(ctx, gid, c.ID, "", m, domain.MsgHeld, 0, m.MetaString(domain.MetaExecutionID), "")
		e.bus.Publish(ctx, domain.NewEvent(domain.EvMsgHeld, gid, map[string]any{"channel": c.ID, "type": m.Type, "approval": a.ID}).
			WithNode(m.Source, m.MetaInt(domain.MetaReplica)).WithMessage(m.ID))
		return nil
	}
	delete(m.Metadata, domain.MetaHeldFor)
	subject := queue.Subject(gid, c.ID)
	err := e.opt.Broker.Publish(ctx, subject, m)
	if errors.Is(err, queue.ErrNoGroups) {
		// Topology not declared yet on this broker (e.g. the graph was
		// created before the broker was reset). Declare and retry once.
		if derr := e.declareTopology(ctx, g, e.replicasFor(ctx, g)); derr == nil {
			err = e.opt.Broker.Publish(ctx, subject, m)
		}
	}
	if err != nil {
		return fmt.Errorf("publish on %s: %w", c.ID, err)
	}
	observability.MessagesPublished.WithLabelValues(gid, c.ID, m.Type).Inc()
	e.recordMessage(ctx, gid, c.ID, "", m, domain.MsgPublished, 0, m.MetaString(domain.MetaExecutionID), "")
	// The event carries the whole message (so observers need no second
	// lookup) plus where it went.
	e.bus.Publish(ctx, domain.NewEvent(domain.EvMsgPublished, gid, struct {
		*domain.Message
		Channel   string   `json:"channel"`
		Consumers []string `json:"consumers"`
	}{m, c.ID, g.Consumers(c)}).WithNode(m.Source, m.MetaInt(domain.MetaReplica)).WithMessage(m.ID).WithTrace(traceIDOf(m)))
	return nil
}

func displaySource(s string) string {
	if s == "" {
		return "external"
	}
	return s
}

func replicasOf(g *domain.Graph, node string) int {
	n := g.Node(node)
	if n == nil {
		return 1
	}
	if n.Cluster.Mode == domain.ClusterAutoscale {
		return n.Cluster.MaxReplicas
	}
	return max(n.Cluster.Replicas, 1)
}

type heldMessage struct {
	GraphID   string          `json:"graphId"`
	Channel   string          `json:"channel"`
	Message   *domain.Message `json:"message"`
	OnTimeout string          `json:"onTimeout,omitempty"`
}

// onApprovalDecided completes held messages: approved ones are published
// (with an edited payload if the approver supplied one), rejected or
// expired ones are recorded as rejected.
func (e *Engine) onApprovalDecided(ctx context.Context, a *domain.Approval) {
	if a.Kind != domain.ApprovalMessage {
		return
	}
	var h heldMessage
	if err := json.Unmarshal(a.Detail, &h); err != nil || h.Message == nil {
		return
	}
	g, err := e.store.GetGraph(ctx, h.GraphID, 0)
	if err != nil {
		return
	}
	graph.Normalize(g)
	c := g.Channel(h.Channel)
	if c == nil {
		e.recordMessage(ctx, h.GraphID, h.Channel, "", h.Message, domain.MsgRejected, 0, "", "channel no longer exists")
		return
	}
	if a.Status != domain.ApprovalApproved {
		e.recordMessage(ctx, h.GraphID, h.Channel, "", h.Message, domain.MsgRejected, 0, "", "rejected by "+a.DecidedBy+": "+a.Comment)
		return
	}
	if strings.TrimSpace(a.Answer) != "" && json.Valid([]byte(a.Answer)) {
		h.Message.Payload = json.RawMessage(a.Answer)
		h.Message.SetMeta("edited_by", a.DecidedBy)
	}
	if err := e.publish(ctx, g, c, h.Message, false, domain.ApprovalSpec{}); err != nil {
		e.log.Error("publishing approved message failed", "approval", a.ID, "err", err)
	}
}

// Inject publishes a message from outside the graph (API, UI, CLI) onto a
// channel - the way work enters a factory, and the way an operator nudges
// a running one. Channel gates still apply.
func (e *Engine) Inject(ctx context.Context, graphID, channel, typ string, payload json.RawMessage, target string, meta map[string]any, actor string) (*domain.Message, error) {
	g, err := e.store.GetGraph(ctx, graphID, 0)
	if err != nil {
		return nil, err
	}
	graph.Normalize(g)
	c := g.Channel(channel)
	if c == nil {
		return nil, fmt.Errorf("%w: channel %q", ErrNotFound, channel)
	}
	if typ == "" {
		return nil, fmt.Errorf("%w: type is required", ErrInvalid)
	}
	m, err := domain.NewMessage(typ, "", target, payload)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	m.Source = "external:" + actor
	for k, v := range meta {
		if !reservedMeta[k] {
			m.SetMeta(k, v)
		}
	}
	m.SetMeta(domain.MetaGraph, graphID)
	m.SetMeta(domain.MetaGraphVersion, g.Metadata.Version)
	if rec, err := e.store.GetGraphRecord(ctx, graphID); err == nil {
		m.SetMeta(domain.MetaRunID, rec.RunID)
	}
	m.SetMeta(domain.MetaInjectedBy, actor)
	ctx, span := observability.Start(ctx, "message.inject")
	defer span.End()
	observability.Inject(ctx, m.Metadata)
	if err := e.declareTopology(ctx, g, e.replicasFor(ctx, g)); err != nil {
		return nil, err
	}
	if err := e.publish(ctx, g, c, m, c.Approval, domain.ApprovalSpec{}); err != nil {
		return nil, err
	}
	e.bus.Publish(ctx, domain.NewEvent(domain.EvMsgInjected, graphID, map[string]any{"channel": channel, "type": typ}).WithMessage(m.ID).WithActor(actor))
	return m, nil
}

// recordMessage writes message history. History is best-effort: a failed
// write is logged and never fails delivery.
func (e *Engine) recordMessage(ctx context.Context, gid, channel, group string, m *domain.Message, st domain.MessageStatus, attempt int, consumer, errMsg string) {
	now := time.Now().UTC()
	rec := &domain.MessageRecord{
		Message: *m, GraphID: gid, Channel: channel, Group: group, Status: st, Attempt: attempt,
		Consumer: consumer, Error: truncate(errMsg, 2000), CreatedAt: now, UpdatedAt: now,
	}
	if t := m.CreatedAt(); !t.IsZero() {
		rec.CreatedAt = t
	}
	if err := e.store.RecordMessage(context.WithoutCancel(ctx), rec); err != nil {
		e.log.Warn("message history not recorded", "message", m.ID, "err", err)
	}
}

func traceIDOf(m *domain.Message) string {
	tp := m.MetaString(domain.MetaTraceParent) // 00-<trace>-<span>-<flags>
	parts := strings.Split(tp, "-")
	if len(parts) == 4 {
		return parts[1]
	}
	return ""
}
