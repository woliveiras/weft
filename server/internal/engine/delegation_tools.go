package engine

import (
	"context"
	"errors"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/llm"
)

// Child calls retain the parent identity for shared rate limits and workspace
// policy, but use the child's execution ID for approvals, history and attribution.
// Platform memory/emit tools are never offered or executed by this bounded loop.
func (t *turnEnv) delegatedChat(ctx context.Context, x *domain.Execution, grants []domain.ToolGrant, req *llm.ChatRequest, provider string, first *tokenReservation, maxBytes int) (*llm.ChatResponse, error) {
	node := *t.n
	node.Runtime = domain.RuntimeRef{Kind: domain.RuntimeNative}
	node.Tools = grants
	node.Model.MaxTokens = req.MaxTokens
	node.Limits.MaxToolCalls = 16
	if t.n.Limits.MaxToolCalls > 0 {
		node.Limits.MaxToolCalls = min(16, t.n.Limits.MaxToolCalls-t.toolCalls())
	}
	if len(grants) > 0 && node.Limits.MaxToolCalls <= 0 {
		return nil, errors.New("delegation: parent tool budget exhausted")
	}
	child := newTurnEnv(t.r, t.gr, &node, x)
	child.executionCtx = ctx
	child.budgetRootID = t.budgetRootID
	defer func() {
		x.ToolCalls = child.toolCalls()
		descendants := child.usage()
		x.Usage.Add(descendants)
		t.mu.Lock()
		t.use.Add(descendants)
		t.calls += x.ToolCalls
		t.mu.Unlock()
	}()
	allowance := req.MaxTokens
	rounds := max(1, node.Limits.MaxToolCalls+1)
	for round := 0; round < rounds; round++ {
		reservation := first
		if round > 0 {
			var err error
			req.MaxTokens = allowance - x.Usage.OutputTokens - child.usage().OutputTokens
			if req.MaxTokens <= 0 {
				return nil, errors.New("delegation result exceeded its output limit")
			}
			reservation, err = t.reserveTokens(ctx, req)
			if err != nil {
				return nil, err
			}
		}
		// Money is reserved before the token reservation is marked
		// dispatched, so a refused call releases its tokens in full.
		money, err := t.reserveMoney(ctx, provider, req)
		if err == nil {
			if err = reservation.dispatch(ctx); err != nil {
				err = errors.Join(err, money.Release(ctx))
			}
		}
		if err != nil {
			if round > 0 {
				err = errors.Join(err, reservation.settle(ctx))
			}
			return nil, err
		}
		response, err := t.gr.e.opt.Models.Chat(ctx, provider, req)
		reservation.report(response)
		err = errors.Join(err, t.settleMoney(ctx, money, response))
		if round > 0 {
			err = errors.Join(err, reservation.settle(ctx))
		}
		if response != nil {
			use := domainUsage(response.Usage)
			x.Usage.Add(use)
			t.mu.Lock()
			t.use.Add(use)
			t.mu.Unlock()
		}
		if err != nil {
			return nil, errors.New("delegation model request or settlement failed")
		}
		if response == nil {
			return nil, errors.New("delegation returned no result")
		}
		if x.Usage.OutputTokens+child.usage().OutputTokens > allowance {
			return nil, errors.New("delegation result exceeded its output limit")
		}
		if len(response.Message.ToolCalls) == 0 || len(grants) == 0 {
			return response, nil
		}
		if response.StopReason == "length" || x.Usage.OutputTokens+child.usage().OutputTokens >= allowance {
			return nil, errors.New("delegation result exceeded its output limit")
		}
		message := response.Message
		message.Role = llm.RoleAssistant
		req.Messages = append(req.Messages, message)
		for _, call := range message.ToolCalls {
			if child.toolCalls() >= node.Limits.MaxToolCalls {
				return nil, errors.New("delegation tool call budget exhausted")
			}
			out, err := child.InvokeTool(ctx, call.Name, call.Arguments)
			if err != nil {
				return nil, errors.New("delegated tool call failed; inspect the child tool history")
			}
			if out == nil || out.IsError || out.Yield || out.Truncated {
				return nil, errors.New("delegated tool returned an incomplete or failed result")
			}
			if len(out.Content) > maxBytes {
				return nil, errors.New("delegated tool result exceeds size limit")
			}
			req.Messages = append(req.Messages, llm.ChatMessage{Role: llm.RoleTool, ToolCallID: call.ID, Name: call.Name, Content: out.Content})
		}
	}
	return nil, errors.New("delegation model round limit exhausted")
}
