package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/observability"
	"github.com/woliveiras/weft/server/internal/queue"
	"github.com/woliveiras/weft/server/internal/storage"
)

const reviewDispatchMeta = "review_dispatch"
const analysisOnlyMeta = "review_analysis_only"

var (
	ErrReviewDispatchStale    = fmt.Errorf("%w: review destination revision or run changed", ErrConflict)
	ErrReviewDispatchInactive = fmt.Errorf("%w: review destination is not running", ErrConflict)
)

// ReviewDispatch is an internal request, not the public injection contract.
// The caller must reserve its durable receipt before dispatch and must not
// automatically replay an uncertain publication. Message.ID must remain stable.
type ReviewDispatch struct {
	AnalysisOnly  bool
	GraphID       string
	Version       int
	Channel       string
	Node          string
	ExpectedRunID string
	Message       *domain.Message
}

// DispatchReview publishes only to the exact running workflow snapshot. It
// never starts or resumes a graph. Channel and before-turn approvals still apply.
func (e *Engine) DispatchReview(ctx context.Context, in ReviewDispatch) (string, error) {
	if in.GraphID == "" || in.Version < 1 || in.Channel == "" || in.Node == "" || in.ExpectedRunID == "" || in.Message.Validate() != nil {
		return "", fmt.Errorf("%w: exact review destination, run and stable message are required", ErrInvalid)
	}
	ctx, span := observability.Start(ctx, "message.review_dispatch")
	defer span.End()
	var runID string
	err := e.WithGraphTransition(ctx, in.GraphID, func(ctx context.Context) error {
		rec, err := e.store.GetGraphRecord(ctx, in.GraphID)
		if err != nil {
			return err
		}
		if rec.Version != in.Version || rec.RunID != in.ExpectedRunID {
			return ErrReviewDispatchStale
		}
		if rec.State != domain.GraphRunning || rec.DesiredState != domain.GraphRunning {
			return ErrReviewDispatchInactive
		}
		e.mu.Lock()
		gr := e.graphs[in.GraphID]
		e.mu.Unlock()
		if gr == nil || gr.version != in.Version || gr.runID != in.ExpectedRunID {
			return ErrReviewDispatchStale
		}
		n := gr.g.Node(in.Node)
		c := gr.g.Channel(in.Channel)
		if n == nil || n.Disabled || n.Loop.Mode != domain.LoopEvent || c == nil || !slices.Equal(gr.g.Consumers(c), []string{in.Node}) {
			return fmt.Errorf("%w: review channel must have exactly the selected event agent as consumer", ErrInvalid)
		}
		m := in.Message.Clone()
		createdAt := m.CreatedAt()
		if createdAt.IsZero() {
			createdAt = time.Now().UTC()
		}
		for key := range reservedMeta {
			delete(m.Metadata, key)
		}
		m.Source, m.Target = "system:review-router", in.Node
		m.SetMeta(reviewDispatchMeta, true)
		if in.AnalysisOnly {
			if n.Runtime.Kind != domain.RuntimeNative || n.Loop.BatchSize > 1 || len(gr.g.Outgoing(n.ID)) != 0 {
				return fmt.Errorf("%w: review requires a single-input native analysis destination", ErrInvalid)
			}
			m.SetMeta(analysisOnlyMeta, true)
		}
		m.SetMeta(domain.MetaGraph, in.GraphID)
		m.SetMeta(domain.MetaGraphVersion, in.Version)
		m.SetMeta(domain.MetaRunID, in.ExpectedRunID)
		m.SetMeta(domain.MetaInjectedBy, "system:review-router")
		m.SetMeta(domain.MetaCreatedAt, createdAt.UTC().Format(time.RFC3339Nano))
		m.SetMeta(domain.MetaHops, 0)
		observability.Inject(ctx, m.Metadata)
		if err := e.reviewMessageFence(ctx, gr, in.Node, m); err != nil {
			return err
		}
		if err := e.publish(ctx, gr.g, c, m, c.Approval, domain.ApprovalSpec{}); err != nil {
			return err
		}
		runID = in.ExpectedRunID
		return nil
	})
	return runID, err
}

// reviewMessageFence is deliberately limited to engine-authored review inputs.
// Ordinary Inject and runtime outputs retain their existing queue semantics.
func (e *Engine) reviewMessageFence(ctx context.Context, gr *graphRun, node string, m *domain.Message) error {
	if m.Metadata[reviewDispatchMeta] != true {
		return nil
	}
	if m.MetaString(domain.MetaGraph) != gr.g.Metadata.ID || m.MetaInt(domain.MetaGraphVersion) != gr.version || m.MetaString(domain.MetaRunID) != gr.runID || m.Target != node {
		return ErrReviewDispatchStale
	}
	rec, err := e.store.GetGraphRecord(ctx, gr.g.Metadata.ID)
	if err != nil {
		return err
	}
	if rec.Version != gr.version || rec.RunID != gr.runID {
		return ErrReviewDispatchStale
	}
	if rec.State != domain.GraphRunning || rec.DesiredState != domain.GraphRunning || gr.isPaused() {
		return ErrReviewDispatchInactive
	}
	raw, err := e.store.KVGet(ctx, controlNS(gr.g.Metadata.ID), "node/"+node)
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		return err
	}
	if err == nil {
		var control nodeControl
		if json.Unmarshal(raw, &control) != nil {
			return errors.New("review destination control is invalid")
		}
		if control.Paused || control.Stopped {
			return ErrReviewDispatchInactive
		}
	}
	return nil
}

func (r *replica) rejectReviewDelivery(ctx context.Context, s *sub, d queue.Delivery) bool {
	gr := r.run_l()
	err := gr.e.reviewMessageFence(ctx, gr, r.nodeID, d.Message())
	if err == nil {
		return false
	}
	if errors.Is(err, ErrReviewDispatchStale) {
		if termErr := d.Term(ctx, err.Error()); termErr != nil {
			gr.e.log.Warn("stale review input termination failed", "message", d.Message().ID, "err", termErr)
		}
		gr.e.recordMessage(ctx, gr.g.Metadata.ID, s.group.Channel, s.group.Group, d.Message(), domain.MsgDeadLettered, d.Attempt(), "", err.Error())
	} else {
		if nackErr := d.Nack(ctx, gr.e.opt.RetryBackoff); nackErr != nil {
			gr.e.log.Warn("review input deferral failed", "message", d.Message().ID, "err", nackErr)
		}
		if !errors.Is(err, ErrReviewDispatchInactive) {
			gr.e.log.Warn("review destination check failed", "graph", gr.g.Metadata.ID, "message", d.Message().ID, "err", err)
		}
	}
	return true
}
