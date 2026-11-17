package instructions

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"sort"
	"strings"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
)

var ErrRequiredBudget = errors.New("required agent/task instructions exceed the instruction budget")

// Resolve accepts only the workspace already selected by the platform.
// Directory-relative opens prevent symlink traversal and replacement races.
func (m Manager) Resolve(ctx context.Context, g *domain.Graph, n *domain.Node, root, prompt string, tasks []string) (*Snapshot, error) {
	s, _, err := m.Prepare(ctx, g, n, root, root, prompt, tasks)
	return s, err
}

func (m Manager) resolveLegacy(ctx context.Context, g *domain.Graph, n *domain.Node, root, prompt string, tasks []string) (*Snapshot, error) {
	s := &Snapshot{GraphID: g.Metadata.ID, NodeID: n.ID, Entries: []Entry{}, Budget: MaxTokens}
	refs := g.Spec.Instructions
	mandatory := []Entry{{Scope: "agent", Source: n.ID, Body: prompt, SHA256: hash(prompt), Included: true}}
	requiredBytes := len(prompt)
	for i, task := range tasks {
		mandatory = append(mandatory, Entry{Scope: "task", Source: fmt.Sprintf("operator-%d", i), Body: task, SHA256: hash(task), Included: true})
		requiredBytes += len(task)
	}
	if !Enabled(g) {
		s.Entries = mandatory
		return s, nil
	}
	requiredBytes += len(GuidancePreamble) + 1
	if requiredBytes > MaxTokens {
		for i := range mandatory {
			mandatory[i].Included = false
			mandatory[i].Reason = "required_content_exceeds_budget"
			mandatory[i].Body = ""
		}
		s.Entries = mandatory
		return s, ErrRequiredBudget
	}
	s.Budget -= requiredBytes

	if err := m.managed(ctx, g, n, false, func(e Entry) {
		e.Description = ""
		add(s, e)
	}); err != nil {
		return nil, err
	}
	if refs.ProjectSteering {
		project(s, root)
	}
	s.Entries = append(s.Entries, mandatory...)
	return s, nil
}
func (m Manager) managed(ctx context.Context, g *domain.Graph, n *domain.Node, preferSelectedRank bool, accept func(Entry)) error {
	refs := g.Spec.Instructions
	selected := selectedSkillIDs(g, n)
	seen := map[string]bool{}
	addSkill := func(id, scope string) error {
		if seen[id] {
			accept(Entry{Scope: scope, Source: id, Reason: "duplicate"})
			return nil
		}
		seen[id] = true
		v, err := m.Get(ctx, id)
		if errors.Is(err, storage.ErrNotFound) {
			accept(Entry{Scope: scope, Source: id, Reason: "missing"})
			return nil
		}
		if err != nil {
			return err
		}
		e := Entry{Scope: scope, Source: id, Revision: v.Revision, Body: v.Body, Description: v.Description}
		if !v.Enabled || v.Deleted {
			e.Reason = "disabled"
		}
		accept(e)
		return nil
	}
	if refs.InstallationDefaults {
		all, err := m.List(ctx)
		if err != nil {
			return err
		}
		count := 0
		for _, v := range all {
			if v.Global {
				count++
				if count > MaxEntries {
					accept(Entry{Scope: "installation", Source: "defaults", Reason: "entry_count_limit"})
					break
				}
				if preferSelectedRank && slices.Contains(selected, v.ID) {
					accept(Entry{Scope: "installation", Source: v.ID, Reason: "duplicate"})
					continue
				}
				if err := addSkill(v.ID, "installation"); err != nil {
					return err
				}
			}
		}
	}
	for _, id := range selected {
		if err := addSkill(id, "graph"); err != nil {
			return err
		}
	}
	return nil
}

func selectedSkillIDs(g *domain.Graph, n *domain.Node) []string {
	if n.SkillIDs != nil {
		return n.SkillIDs
	}
	return g.Spec.Instructions.SkillIDs
}

func project(s *Snapshot, root string) {
	const dir = ".weft/steering"
	omit := func(source, reason string) { add(s, Entry{Scope: "project", Source: source, Reason: reason}) }
	r, err := openRepoRoot(root)
	if err != nil {
		omit(dir, "workspace_unavailable")
		return
	}
	defer r.Close()
	f, err := openRepoPath(r, dir, true)
	if err != nil {
		omit(dir, fileReason(err))
		return
	}
	entries, err := f.ReadDir(MaxEntries + 1)
	f.Close()
	if err != nil && err != io.EOF {
		omit(dir, "unreadable")
		return
	}
	if len(entries) > MaxEntries {
		omit(dir, "file_count_limit")
		return
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	hashes := map[string]bool{}
	for _, de := range entries {
		path := dir + "/" + de.Name()
		if !strings.HasSuffix(de.Name(), ".md") {
			omit(path, "unsupported_format")
			continue
		}
		if de.Type()&os.ModeSymlink != 0 || !de.Type().IsRegular() {
			omit(path, "unsafe_path")
			continue
		}
		file, reason := regularFile(r, path)
		if reason != "" {
			omit(path, reason)
			continue
		}
		body, reason := readLimitedText(file)
		file.Close()
		if reason != "" {
			omit(path, reason)
			continue
		}
		e := Entry{Scope: "project", Source: path, Body: body}
		h := hash(e.Body)
		if hashes[h] {
			e.Reason = "duplicate_content"
		}
		hashes[h] = true
		add(s, e)
	}
}

// Enabled preserves legacy prompt semantics for graphs without instruction opt-in.
func Enabled(g *domain.Graph) bool {
	r := g.Spec.Instructions
	return r.InstallationDefaults || r.ProjectSteering || len(r.SkillIDs) > 0
}
