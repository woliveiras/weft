// Package hitl implements human-in-the-loop primitives: approval gates on
// tools, turns and messages, and agent questions to a human.
//
// An approval is a durable record first and a wait second. A caller that
// blocks on one (a tool call, a turn) waits on a local channel that is
// signalled when this instance decides it, and also polls the store, so a
// decision taken through another instance - or after this one restarted -
// still releases it. Non-blocking approvals (held messages) are completed by
// a decision hook that performs the deferred action on whichever instance
// received the decision.
package hitl

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/events"
	"github.com/woliveiras/weft/server/internal/observability"
	"github.com/woliveiras/weft/server/internal/storage"
)

// DecisionHook runs after an approval is decided (on the deciding instance).
type DecisionHook func(ctx context.Context, a *domain.Approval)

type Service struct {
	store storage.Store
	bus   *events.Bus
	log   *slog.Logger

	mu      sync.Mutex
	waiters map[string]chan *domain.Approval
	hooks   []DecisionHook
	// DefaultTimeout applies when a request carries no ExpiresAt (0 = wait forever).
	DefaultTimeout time.Duration
}

func New(store storage.Store, bus *events.Bus, log *slog.Logger) *Service {
	return &Service{store: store, bus: bus, log: log, waiters: map[string]chan *domain.Approval{}}
}

// OnDecision registers a hook.
func (s *Service) OnDecision(h DecisionHook) {
	s.mu.Lock()
	s.hooks = append(s.hooks, h)
	s.mu.Unlock()
}

// Create persists a pending approval and announces it, without waiting.
func (s *Service) Create(ctx context.Context, a *domain.Approval) (*domain.Approval, error) {
	if a.ID == "" {
		a.ID = domain.NewID("apr")
	}
	a.Status = domain.ApprovalPending
	a.CreatedAt = time.Now().UTC()
	if a.ExpiresAt == nil && s.DefaultTimeout > 0 {
		t := a.CreatedAt.Add(s.DefaultTimeout)
		a.ExpiresAt = &t
	}
	if err := s.store.PutApproval(ctx, a); err != nil {
		return nil, err
	}
	s.bus.Publish(ctx, domain.NewEvent(domain.EvApprovalReq, a.GraphID, a).
		WithNode(a.NodeID, a.Replica).WithExecution(a.ExecutionID))
	return a, nil
}

// RequestApproval implements tools.Approver: create and block until decided.
// If the caller ends first, close the pending approval without applying its
// timeout-approval policy. A cancelled execution cannot accept late approval.
func (s *Service) RequestApproval(ctx context.Context, a *domain.Approval) (*domain.Approval, error) {
	a, err := s.Create(ctx, a)
	if err != nil {
		return nil, err
	}
	out, err := s.Wait(ctx, a.ID)
	if err != nil && ctx.Err() != nil {
		cleanup, done := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer done()
		_, closeErr := s.CancelPending(cleanup, a.ID)
		err = errors.Join(err, closeErr)
	}
	return out, err
}

// Wait blocks until the approval leaves pending.
func (s *Service) Wait(ctx context.Context, id string) (*domain.Approval, error) {
	ch := make(chan *domain.Approval, 1)
	s.mu.Lock()
	s.waiters[id] = ch
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.waiters, id)
		s.mu.Unlock()
	}()
	poll := time.NewTicker(2 * time.Second)
	defer poll.Stop()
	for {
		a, err := s.store.GetApproval(ctx, id)
		if err != nil {
			return nil, err
		}
		if a.Status != domain.ApprovalPending {
			return a, nil
		}
		if a.ExpiresAt != nil && time.Now().After(*a.ExpiresAt) {
			if d, err := s.Expire(ctx, a); err == nil {
				return d, nil
			}
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case d := <-ch:
			return d, nil
		case <-poll.C:
		}
	}
}

// ErrNotPending is returned when deciding an already-decided approval.
var ErrNotPending = errors.New("approval is no longer pending")

// Decide approves or rejects. answer carries a free-text reply (questions)
// or an edited payload (held messages).
func (s *Service) Decide(ctx context.Context, id string, approve bool, by, comment, answer string) (*domain.Approval, error) {
	st := domain.ApprovalRejected
	if approve {
		st = domain.ApprovalApproved
	}
	a, err := s.store.DecideApproval(ctx, id, st, by, comment, answer)
	if errors.Is(err, storage.ErrConflict) {
		return nil, ErrNotPending
	}
	if err != nil {
		return nil, err
	}
	s.decided(ctx, a)
	return a, nil
}

// Expire resolves a timed-out approval according to its OnTimeout policy
// (stored in Detail.onTimeout by the requester; default reject).
func (s *Service) Expire(ctx context.Context, a *domain.Approval) (*domain.Approval, error) {
	var d struct {
		OnTimeout string `json:"onTimeout"`
	}
	_ = json.Unmarshal(a.Detail, &d)
	st := domain.ApprovalExpired
	if d.OnTimeout == "approve" {
		st = domain.ApprovalApproved
	}
	out, err := s.store.DecideApproval(ctx, a.ID, st, "system:timeout", "expired", "")
	if err != nil {
		if errors.Is(err, storage.ErrConflict) {
			return s.store.GetApproval(ctx, a.ID)
		}
		return nil, err
	}
	s.decided(ctx, out)
	return out, nil
}

// Sweep expires overdue approvals and refreshes the pending gauge from the
// store (called periodically by the engine). The gauge is derived, never
// incremented locally, so it is the same on every instance.
func (s *Service) Sweep(ctx context.Context) {
	list, err := s.store.ListApprovals(ctx, storage.ApprovalFilter{Status: domain.ApprovalPending, Limit: 1000})
	if err != nil {
		return
	}
	observability.ApprovalsPending.Set(float64(len(list)))
	now := time.Now()
	for _, a := range list {
		if a.ExpiresAt != nil && now.After(*a.ExpiresAt) {
			_, _ = s.Expire(ctx, a)
		}
	}
}

func (s *Service) decided(ctx context.Context, a *domain.Approval) {
	observability.Approvals.WithLabelValues(string(a.Kind), string(a.Status)).Inc()
	actor := a.DecidedBy
	if actor == "" {
		actor = "system"
	}
	s.bus.Publish(ctx, domain.NewEvent(domain.EvApprovalDone, a.GraphID, a).
		WithNode(a.NodeID, a.Replica).WithExecution(a.ExecutionID).WithActor(actor))
	s.mu.Lock()
	ch := s.waiters[a.ID]
	hooks := append([]DecisionHook(nil), s.hooks...)
	s.mu.Unlock()
	if ch != nil {
		select {
		case ch <- a:
		default:
		}
	}
	for _, h := range hooks {
		h(ctx, a)
	}
}

// CancelPending never applies the timeout approval policy.
func (s *Service) CancelPending(ctx context.Context, id string) (*domain.Approval, error) {
	a, err := s.store.DecideApproval(ctx, id, domain.ApprovalExpired, "system:cancel", "execution cancelled", "")
	if errors.Is(err, storage.ErrConflict) {
		return s.store.GetApproval(ctx, id)
	}
	if err != nil {
		return nil, err
	}
	s.decided(ctx, a)
	return a, nil
}
