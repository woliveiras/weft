package review

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/tools"
	"github.com/woliveiras/weft/server/internal/workspace"
)

// Capture the diff against each reviewed base before the second fingerprint.
// Content-addressed evidence remains readable after the checkout changes.
func (m *Manager) captureRepositoryEvidence(ctx context.Context, g *domain.Graph, source, dir string, states map[string]workspace.RepositoryState) (map[string]string, map[string]string, error) {
	if len(states) == 0 {
		return nil, nil, nil
	}
	destination := filepath.Join(dir, "diffs")
	if err := os.MkdirAll(destination, 0700); err != nil {
		return nil, nil, err
	}
	wm := workspace.Manager{GitBin: "git"}
	diffs := map[string]string{}
	var names []string
	for _, check := range g.Spec.Checks {
		names = append(names, check.Name)
	}
	for _, repo := range g.Spec.Workspace.Repositories {
		for _, check := range repo.Checks {
			names = append(names, repo.Alias+"."+check.Name)
		}
		if !states[repo.Alias].Changed {
			continue
		}
		checkout := filepath.Join(source, repo.Alias)
		diff, err := wm.Inspect(ctx, checkout, checkout, "diff", "", repo.BaseCommit)
		if err != nil {
			return nil, nil, fmt.Errorf("%s diff: %w", repo.Alias, err)
		}
		contents := diff.Content
		if states[repo.Alias].Dirty {
			status, err := wm.Inspect(ctx, checkout, checkout, "status", "")
			if err != nil {
				return nil, nil, err
			}
			contents = status.Content + "\n" + contents
		}
		digest := fmt.Sprintf("%x", sha256.Sum256([]byte(contents)))
		if err := os.WriteFile(filepath.Join(destination, digest), []byte(contents), 0600); err != nil {
			return nil, nil, err
		}
		diffs[repo.Alias] = digest
	}
	checks, err := tools.RepositoryCheckEvidence(ctx, m.Store, g.Metadata.ID, names, states)
	return diffs, checks, err
}
func (r *Record) RepositoryDiff(alias string) (string, error) {
	digest, ok := r.RepositoryDiffs[alias]
	if !ok {
		return "", storage.ErrNotFound
	}
	decoded, err := hex.DecodeString(digest)
	if err != nil || len(decoded) != sha256.Size {
		return "", errors.New("invalid stored diff fingerprint")
	}
	data, err := os.ReadFile(filepath.Join(r.Directory, "diffs", digest))
	if err != nil {
		return "", err
	}
	if fmt.Sprintf("%x", sha256.Sum256(data)) != digest {
		return "", errors.New("review diff fingerprint changed")
	}
	return string(data), nil
}
