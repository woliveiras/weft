package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/workspace"
)

const GitPublish = "git.publish"

type gitPublishTool struct{}

func (gitPublishTool) Name() string       { return GitPublish }
func (gitPublishTool) Capability() string { return GitPublish }
func (gitPublishTool) Description() string {
	return "Publish the committed tip of your private replica branch to the graph repository. Supply the full source commit and the previously observed destination commit (empty string to create)."
}
func (gitPublishTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"commit":{"type":"string"},"previous":{"type":"string"}},"required":["commit","previous"],"additionalProperties":false}`)
}
func (t gitPublishTool) SchemaFor(p *Principal) (llm.ToolSchema, bool) {
	if p == nil || p.PublishBranch == nil || p.WorkspaceReadOnly {
		return llm.ToolSchema{}, false
	}
	return llm.ToolSchema{Name: t.Name(), Description: t.Description(), Parameters: t.Schema()}, true
}
func (gitPublishTool) Invoke(ctx context.Context, inv *Invocation) (*Result, error) {
	if inv.Principal == nil || inv.Principal.PublishBranch == nil {
		return nil, fmt.Errorf("%w: publication requires an active writable private replica", ErrDenied)
	}
	req, err := workspace.DecodeBranchPublication(bytes.NewReader(inv.Args))
	if err != nil {
		return nil, err
	}
	receipt, err := inv.Principal.PublishBranch(ctx, req)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(receipt)
	if err != nil {
		return nil, err
	}
	return &Result{Content: "Published " + receipt.Commit + " to " + receipt.Branch, Data: data}, nil
}
