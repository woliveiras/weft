package engine

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/woliveiras/weft/server/internal/delegation"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/llm"
)

type tokenReservation struct {
	manager  delegation.Manager
	root, id string
	reported int
	known    bool
}

func (t *turnEnv) hasTreeBudget() bool {
	if !delegationRuntime(t.n.Runtime.Kind) {
		return false
	}
	for _, g := range t.n.Tools {
		if g.Name == "delegation.request" {
			return true
		}
	}
	return false
}
func (t *turnEnv) reserveTokens(ctx context.Context, req *llm.ChatRequest) (*tokenReservation, error) {
	m := delegation.Manager{Store: t.gr.e.store}
	policy, e := m.Policy(ctx)
	if e != nil {
		return nil, e
	}
	limit := policy.Policy.MaxTreeTokens
	if t.n.Limits.MaxTokens > 0 {
		limit = min(limit, t.n.Limits.MaxTokens)
	}
	if req.MaxTokens == 0 {
		req.MaxTokens = policy.Policy.MaxOutputTokens
	}
	if req.MaxTokens < 1 || req.MaxTokens > delegation.MaxTreeTokens {
		return nil, errors.New("invalid model output token allowance")
	}
	// Includes system text, conversation, tool schemas and framing. This is a
	// conservative admission estimate, not a provider tokenizer or billing cap.
	raw, e := json.Marshal(req)
	if e != nil {
		return nil, e
	}
	amount := len(raw) + 512 + req.MaxTokens
	if amount > limit {
		return nil, delegation.ErrBudget
	}
	root := t.budgetRootID
	if root == "" {
		root = t.x.ID
	}
	r := &tokenReservation{manager: m, root: root, id: domain.NewID("tokens")}
	if e = m.ReserveTokens(ctx, r.root, r.id, limit, amount); e != nil {
		return nil, e
	}
	return r, nil
}
func (r *tokenReservation) dispatch(ctx context.Context) error {
	return r.manager.DispatchTokens(ctx, r.root, r.id)
}
func (r *tokenReservation) report(resp *llm.ChatResponse) {
	if resp != nil && resp.Usage.InputTokens >= 0 && resp.Usage.OutputTokens >= 0 && resp.Usage.InputTokens <= delegation.MaxTreeTokens*50 && resp.Usage.OutputTokens <= delegation.MaxTreeTokens*50 {
		r.reported = resp.Usage.InputTokens + resp.Usage.OutputTokens
		r.known = r.reported > 0
	}
}
func (r *tokenReservation) settle(ctx context.Context) error {
	cleanup, done := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer done()
	return r.manager.SettleTokens(cleanup, r.root, r.id, r.reported, r.known)
}
