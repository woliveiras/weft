package engine

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/instructions"
	"github.com/woliveiras/weft/server/internal/tools"
)

type turnGuidance struct {
	*instructions.Session
	env *turnEnv
}

func (g *turnGuidance) Load(ctx context.Context, skill, ref string) (string, error) {
	g.env.toolMu.Lock()
	defer g.env.toolMu.Unlock()
	ctx, done, err := g.env.controlled(ctx)
	defer done()
	if err != nil {
		return "", err
	}
	if err := g.env.countTool(); err != nil {
		return err.Error(), nil
	}
	return g.Session.Load(ctx, skill, ref)
}

func (t *turnEnv) readGuidanceReference(ctx context.Context, checkout, path string, read func() (string, string)) (string, string, error) {
	gateway, ok := t.gr.e.opt.Tools.(interface {
		ReadGuidanceReference(context.Context, *tools.Principal, string, func()) (*tools.Result, error)
	})
	rel, err := filepath.Rel(t.principal.Workdir, filepath.Join(checkout, filepath.FromSlash(path)))
	if !ok || err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", "permission_denied", nil
	}
	if t.gr.e.opt.Tools.NeedsApproval(t.principal, "filesystem.read") {
		t.r.setState(domain.ReplicaAwaitingApproval, "waiting for approval: filesystem.read")
		defer t.r.setState(domain.ReplicaRunning, "")
	}
	var content, reason string
	result, err := gateway.ReadGuidanceReference(ctx, t.principal, filepath.ToSlash(rel), func() {
		content, reason = read()
	})
	switch {
	case errors.Is(err, tools.ErrDenied), errors.Is(err, tools.ErrUnknownTool):
		return "", "permission_denied", nil
	case errors.Is(err, tools.ErrRejected):
		return "", "approval_rejected", nil
	case errors.Is(err, tools.ErrRateLimited):
		return "", "rate_limited", nil
	case err != nil:
		return "", "", err
	case result.IsError:
		return "", "unreadable", nil
	default:
		return content, reason, nil
	}
}
