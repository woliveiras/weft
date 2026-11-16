package environment

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
)

// A workflow image is transferable only when it contains no task-local setup
// or workspace input. Repository environments use the existing shared pipeline.
func workflowImageReusable(g *domain.Graph) bool {
	e := g.Spec.Environment
	return e != nil && g.Spec.Workspace.Repo == "" && g.Spec.Workspace.Path == "" &&
		e.Context == "" && e.Origin == nil && len(e.Setup) == 0
}

func (m *Manager) reuseWorkflowImage(ctx context.Context, g *domain.Graph, st *Status, log io.Writer) (string, []CacheMount, bool, error) {
	sourceID := g.Metadata.Labels[domain.LabelTaskWorkflow]
	if sourceID == "" || g.Metadata.Labels[domain.LabelTask] == "" || !strings.HasPrefix(g.Metadata.ID, domain.TaskRunGraphPrefix) || !workflowImageReusable(g) {
		return "", nil, false, nil
	}
	// Hold the source environment lease through inspect/tag so reset/rebuild
	// cannot remove its reference before the task owns a separate reference.
	holder := domain.NewID("task-image")
	key := leasePrefix + sourceID
	ok, err := m.opt.Store.AcquireLease(ctx, key, holder, leaseTTL)
	if err != nil {
		return "", nil, false, err
	}
	if !ok {
		return "", nil, false, ErrBuildRunning
	}
	defer func() { _ = m.opt.Store.ReleaseLease(context.WithoutCancel(ctx), key, holder) }()
	source, err := m.opt.Store.GetGraph(ctx, sourceID, 0)
	if errors.Is(err, storage.ErrNotFound) {
		return "", nil, false, nil
	}
	if err != nil {
		return "", nil, false, err
	}
	if !workflowImageReusable(source) || definitionDigest(source.Spec.Environment) != definitionDigest(g.Spec.Environment) {
		return "", nil, false, nil
	}
	v, err := m.Get(ctx, source)
	if err != nil {
		return "", nil, false, err
	}
	if !v.UpToDate {
		return "", nil, false, nil
	}
	if err = m.docker(ctx, nil, io.Discard, "image", "inspect", v.Image); err != nil {
		return "", nil, false, fmt.Errorf("prepared workflow image unavailable: %w", err)
	}
	final := ImageRef(g.Metadata.ID, Hash(g.Spec.Environment))
	if err = m.docker(ctx, nil, log, "tag", v.Image, final); err != nil {
		return "", nil, false, err
	}
	var caches []CacheMount
	for _, dir := range g.Spec.Environment.Caches {
		volume := CacheVolume(g.Metadata.ID, dir)
		if err = m.docker(ctx, nil, io.Discard, "volume", "create", "--label", "weft.graph="+g.Metadata.ID, volume); err != nil {
			return "", nil, false, err
		}
		caches = append(caches, CacheMount{Path: dir, Volume: volume})
	}
	st.Reused = true
	setProgress(ctx, "preparing", "compatible_workflow")
	fmt.Fprintln(log, "==> Reusing prepared workflow image with task-private caches")
	return final, caches, true, nil
}
