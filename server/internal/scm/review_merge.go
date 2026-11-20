package scm

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/woliveiras/weft/server/internal/tools"
)

const CapPullRequestMerge = "scm.pull_request.merge"

type mergeArgs struct {
	Connection    string `json:"connection"`
	Number        int    `json:"number"`
	HeadSHA       string `json:"headSha"`
	BaseSHA       string `json:"baseSha"`
	ReviewSummary string `json:"reviewSummary"`
}

func (a mergeArgs) review() summaryArgs {
	return summaryArgs{Connection: a.Connection, Number: a.Number, HeadSHA: a.HeadSHA, BaseSHA: a.BaseSHA, Verdict: "approved", Summary: a.ReviewSummary}
}

// mergePullRequestTool exposes an explicit unavailable result. No built-in
// adapter currently proves atomic head AND destination protection. A preflight
// followed by an expected-head-only mutation cannot close a retarget race.
type mergePullRequestTool struct{ m Manager }

func (mergePullRequestTool) Name() string       { return CapPullRequestMerge }
func (mergePullRequestTool) Capability() string { return CapPullRequestMerge }
func (mergePullRequestTool) Description() string {
	return "Automatic merge is currently unavailable: no adapter has a qualified atomic reviewed-head and destination contract. Publish a review summary and use the provider's reviewed merge process. This tool never sends a remote mutation."
}
func (mergePullRequestTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"connection":{"type":"string"},"number":{"type":"integer","minimum":1},"headSha":{"type":"string"},"baseSha":{"type":"string"},"reviewSummary":{"type":"string","minLength":1,"maxLength":15872}},"required":["connection","number","headSha","baseSha","reviewSummary"],"additionalProperties":false}`)
}
func (t mergePullRequestTool) Invoke(ctx context.Context, inv *tools.Invocation) (*tools.Result, error) {
	if inv.Grant.Name != CapPullRequestMerge {
		return nil, fmt.Errorf("%w: automatic merge requires an exact scm.pull_request.merge grant", tools.ErrDenied)
	}
	var a mergeArgs
	if err := json.Unmarshal(inv.Args, &a); err != nil {
		return nil, err
	}
	if err := tools.CheckRepository(inv.Grant, a.Connection); err != nil {
		return nil, err
	}
	fail := func(err error) (*tools.Result, error) { return &tools.Result{IsError: true, Content: err.Error()}, nil }
	if err := a.review().validate(); err != nil {
		return fail(err)
	}
	if _, err := t.m.Get(ctx, a.Connection); err != nil {
		return fail(err)
	}
	return fail(fmt.Errorf("automatic merge is unavailable: an atomic reviewed-head and destination contract has not been qualified; no remote operation was sent"))
}
