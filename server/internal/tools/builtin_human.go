package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
)

const capHuman = "human.ask"

func humanTools() []Tool {
	return []Tool{&funcTool{
		name: "human.ask", capability: capHuman,
		description: "Ask a human operator a question and wait for the answer. Use sparingly: only when a decision genuinely needs a person (ambiguous requirements, risky actions). The turn blocks until someone answers.",
		schema:      schema(`{"type":"object","properties":{"question":{"type":"string"}},"required":["question"],"additionalProperties":false}`),
		fn:          humanAsk,
	}}
}

func humanAsk(ctx context.Context, inv *Invocation) (*Result, error) {
	var a struct {
		Question string `json:"question"`
	}
	if err := decodeArgs(inv, &a); err != nil {
		return nil, err
	}
	if strings.TrimSpace(a.Question) == "" {
		return nil, errors.New("question is required")
	}
	if inv.Approver == nil {
		return nil, errors.New("no human operator channel is configured")
	}
	p := inv.Principal
	if p == nil {
		p = &Principal{}
	}
	detail, _ := json.Marshal(map[string]string{"question": a.Question})
	title := a.Question
	if len(title) > 200 {
		title = truncateUTF8(title, 200) + "…"
	}
	decided, err := inv.Approver.RequestApproval(ctx, &domain.Approval{
		ID:          domain.NewID("apr"),
		GraphID:     p.GraphID,
		NodeID:      p.NodeID,
		Replica:     p.Replica,
		ExecutionID: p.ExecutionID,
		Kind:        domain.ApprovalQuestion,
		Status:      domain.ApprovalPending,
		Title:       title,
		Detail:      detail,
		CreatedAt:   time.Now().UTC(),
	})
	if err != nil {
		return nil, err
	}
	if decided == nil || decided.Status != domain.ApprovalApproved {
		msg := "The operator declined to answer."
		if decided != nil && decided.Comment != "" {
			msg += " Comment: " + decided.Comment
		}
		return &Result{Content: msg}, nil
	}
	answer := decided.Answer
	if answer == "" {
		answer = decided.Comment
	}
	if answer == "" {
		answer = "(approved without a written answer)"
	}
	return &Result{Content: answer}, nil
}
