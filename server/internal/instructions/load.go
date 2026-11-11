package instructions

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"syscall"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
)

var ErrRequiredBudget = errors.New("required agent/task instructions exceed the instruction budget")

// Resolve accepts only the workspace already selected by the platform.
// os.Root prevents traversal, including symlink replacement races.
func (m Manager) Resolve(ctx context.Context, g *domain.Graph, n *domain.Node, root, prompt string, tasks []string) (*Snapshot, error) {
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
	seen := map[string]bool{}
	addSkill := func(id, scope string) error {
		if seen[id] {
			add(s, Entry{Scope: scope, Source: id, Reason: "duplicate"})
			return nil
		}
		seen[id] = true
		v, err := m.Get(ctx, id)
		if errors.Is(err, storage.ErrNotFound) {
			add(s, Entry{Scope: scope, Source: id, Reason: "missing"})
			return nil
		}
		if err != nil {
			return err
		}
		e := Entry{Scope: scope, Source: id, Revision: v.Revision, Body: v.Body}
		if !v.Enabled || v.Deleted {
			e.Reason = "disabled"
		}
		add(s, e)
		return nil
	}
	if refs.InstallationDefaults {
		all, err := m.List(ctx)
		if err != nil {
			return nil, err
		}
		count := 0
		for _, v := range all {
			if v.Global {
				count++
				if count > MaxEntries {
					add(s, Entry{Scope: "installation", Source: "defaults", Reason: "entry_count_limit"})
					break
				}
				if err := addSkill(v.ID, "installation"); err != nil {
					return nil, err
				}
			}
		}
	}
	ids := refs.SkillIDs
	if n.SkillIDs != nil {
		ids = n.SkillIDs
	}
	for _, id := range ids {
		if err := addSkill(id, "graph"); err != nil {
			return nil, err
		}
	}
	if refs.ProjectSteering {
		project(s, root)
	}
	s.Entries = append(s.Entries, mandatory...)
	return s, nil
}
func project(s *Snapshot, root string) {
	const dir = ".weft/steering"
	omit := func(source, reason string) { add(s, Entry{Scope: "project", Source: source, Reason: reason}) }
	stRoot, rootErr := os.Lstat(root)
	if rootErr != nil || !stRoot.IsDir() || stRoot.Mode()&os.ModeSymlink != 0 {
		omit(dir, "workspace_unavailable")
		return
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		omit(dir, "workspace_unavailable")
		return
	}
	defer r.Close()
	openedRoot, err := r.Stat(".")
	if err != nil || !os.SameFile(stRoot, openedRoot) {
		omit(dir, "workspace_unavailable")
		return
	}
	for _, p := range []string{".weft", dir} {
		st, err := r.Lstat(p)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				omit(dir, "missing")
			} else {
				omit(dir, "unreadable")
			}
			return
		}
		if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
			omit(dir, "unsafe_path")
			return
		}
	}
	f, err := r.OpenFile(dir, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		omit(dir, "unreadable")
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
		file, err := r.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
		if err != nil {
			omit(path, "unreadable")
			continue
		}
		st, err := file.Stat()
		if err != nil || !st.Mode().IsRegular() {
			file.Close()
			omit(path, "unsafe_path")
			continue
		}
		data, err := io.ReadAll(io.LimitReader(file, MaxBytes+1))
		file.Close()
		if err != nil {
			omit(path, "unreadable")
			continue
		}
		if len(data) > MaxBytes {
			omit(path, "size_limit")
			continue
		}
		e := Entry{Scope: "project", Source: path, Body: string(data)}
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
