package tools

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/observability"
)

func workspaceMutation(t Tool) bool {
	switch t.Capability() {
	case "filesystem.write", "filesystem.delete", "git.write", "shell.exec", "environment.propose", "review.request", "checkpoint.restore":
		return true
	}
	return false
}

// checkpointedMutation: tools before which the engine records a worktree
// checkpoint. checkpoint.restore records its own.
func checkpointedMutation(t Tool) bool {
	switch t.Capability() {
	case "filesystem.write", "filesystem.delete", "git.write", "shell.exec":
		return true
	}
	return false
}

// Snapshots are bounded observations, not a filesystem journal. No contents or
// symlink targets leave this package. OpenRoot prevents following raced links
// outside the authorized workspace.
const maxWorkspaceFiles = 10000
const maxWorkspaceBytes int64 = 64 << 20
const maxWorkspaceChanges = 200

type workspaceSnapshot struct {
	files    map[string]string
	complete bool
}

func snapshotWorkspace(ctx context.Context, dir string) workspaceSnapshot {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	s := workspaceSnapshot{files: map[string]string{}, complete: true}
	root, err := os.OpenRoot(dir)
	if err != nil {
		s.complete = false
		return s
	}
	defer root.Close()
	var total int64
	entries := 0
	err = fs.WalkDir(root.FS(), ".", func(path string, d fs.DirEntry, walkErr error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		entries++
		if entries > maxWorkspaceFiles {
			return fmt.Errorf("entry limit")
		}
		if walkErr != nil {
			s.complete = false
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return fs.SkipDir
			}
			return nil
		}
		if d.Name() == ".git" {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			s.complete = false
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := root.Readlink(path)
			if err != nil {
				s.complete = false
				return nil
			}
			s.files[path] = fmt.Sprintf("link:%x", sha256.Sum256([]byte(target)))
			return nil
		}
		if !info.Mode().IsRegular() {
			s.complete = false
			return nil
		}
		if info.Size() > maxWorkspaceBytes-total {
			s.complete = false
			return nil
		}
		f, err := openWorkspaceFile(root, path)
		if err != nil {
			s.complete = false
			return nil
		}
		opened, statErr := f.Stat()
		if statErr != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
			f.Close()
			s.complete = false
			return nil
		}
		h := sha256.New()
		n, readErr := io.Copy(h, io.LimitReader(f, maxWorkspaceBytes-total+1))
		after, statErr := f.Stat()
		closeErr := f.Close()
		total += n
		if readErr != nil || statErr != nil || closeErr != nil || total > maxWorkspaceBytes || after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) {
			s.complete = false
			return nil
		}
		s.files[filepath.ToSlash(path)] = fmt.Sprintf("%s:%x", info.Mode(), h.Sum(nil))
		return nil
	})
	if err != nil {
		s.complete = false
	}
	return s
}

type workspaceChange struct {
	Path      string `json:"path"`
	Operation string `json:"operation"`
}

func diffWorkspace(before, after workspaceSnapshot) ([]workspaceChange, bool) {
	changes := []workspaceChange{}
	for path, value := range after.files {
		old, exists := before.files[path]
		if exists && old != value {
			changes = append(changes, workspaceChange{path, "modified"})
		}
		if !exists && before.complete {
			changes = append(changes, workspaceChange{path, "created"})
		}
	}
	if after.complete {
		for path := range before.files {
			if _, exists := after.files[path]; !exists {
				changes = append(changes, workspaceChange{path, "deleted"})
			}
		}
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })
	complete := before.complete && after.complete && len(changes) <= maxWorkspaceChanges
	if len(changes) > maxWorkspaceChanges {
		changes = changes[:maxWorkspaceChanges]
	}
	return changes, complete
}

func (g *Gateway) auditWorkspace(ctx context.Context, p *Principal, tool string, before, after workspaceSnapshot) {
	changes, complete := diffWorkspace(before, after)
	status := "complete"
	if !complete {
		status = "partial"
	}
	// An empty observation is retained, including failed/cancelled commands.
	ev := domain.NewEvent("workspace.observed", p.GraphID, map[string]any{
		"tool": tool, "changes": changes, "status": status,
		"attribution": "observed_during_tool", "scope": "workspace_except_git_metadata",
	}).WithNode(p.NodeID, p.Replica).WithExecution(p.ExecutionID).WithTrace(observability.TraceID(ctx))
	if p.Actor != "" {
		ev.WithActor(p.Actor)
	} else if p.NodeID != "" {
		ev.WithActor(domain.ReplicaKey(p.GraphID, p.NodeID, p.Replica))
	}
	g.Auditor.Audit(ctx, ev)
}

func observesWorkspace(t Tool) bool {
	return strings.HasPrefix(t.Name(), "filesystem.") && workspaceMutation(t) || t.Capability() == "git.write" || t.Name() == "shell.exec" || t.Name() == CheckToolName || t.Capability() == "checkpoint.restore"
}
