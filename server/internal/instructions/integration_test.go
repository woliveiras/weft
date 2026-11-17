package instructions

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/storage/sqlstore"
)

func manager(t *testing.T) Manager {
	t.Helper()
	db, err := sqlstore.OpenSQLite(t.Context(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return Manager{Store: db}
}
func TestPersistenceCASAndSnapshot(t *testing.T) {
	m := manager(t)
	in := Input{ID: "code", Title: "Code", Body: "first", Enabled: true}
	v, err := m.Save(t.Context(), in, 0, "alice", false)
	if err != nil {
		t.Fatal(err)
	}
	in.Body = "second"
	v, err = m.Save(t.Context(), in, v.Revision, "bob", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.History) != 1 || v.History[0].Body != "first" || v.History[0].Actor != "alice" {
		t.Fatal(v)
	}
	if _, err = m.Save(t.Context(), in, 1, "stale", false); !errors.Is(err, storage.ErrConflict) {
		t.Fatal(err)
	}
	snap := &Snapshot{ExecutionID: "exec", Entries: []Entry{{Body: "first", Included: true}}}
	if err = m.SaveSnapshot(t.Context(), snap); err != nil {
		t.Fatal(err)
	}
	snap.Entries[0].Body = "modified"
	if err = m.SaveSnapshot(t.Context(), snap); !errors.Is(err, storage.ErrConflict) {
		t.Fatal(err)
	}
	saved, err := m.Snapshot(t.Context(), "exec")
	if err != nil || saved.Entries[0].Body != "first" {
		t.Fatal(saved, err)
	}
	v, err = m.Save(t.Context(), in, 2, "bob", true)
	if err != nil || !v.Deleted || v.Enabled || len(v.History) != 2 {
		t.Fatal(v, err)
	}
}
func TestRepositoryIsolationPrecedenceAndOmissions(t *testing.T) {
	m := manager(t)
	root := t.TempDir()
	dir := filepath.Join(root, ".weft", "steering")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"10-project.md": "project guidance", "20-duplicate.md": "project guidance", "30-invalid.md": "\x00", "40-large.md": strings.Repeat("x", MaxBytes+1), "other.txt": "not Markdown"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	outside := filepath.Join(t.TempDir(), "private.md")
	os.WriteFile(outside, []byte("must never load"), 0600)
	if err := os.Symlink(outside, filepath.Join(dir, "50-escape.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Save(t.Context(), Input{ID: "default", Title: "Default", Body: "global guidance", Enabled: true, Global: true}, 0, "admin", false); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Save(t.Context(), Input{ID: "skill", Title: "Skill", Body: "graph guidance", Enabled: true}, 0, "admin", false); err != nil {
		t.Fatal(err)
	}
	g := &domain.Graph{Metadata: domain.GraphMetadata{ID: "one"}, Spec: domain.GraphSpec{Instructions: domain.InstructionRefs{InstallationDefaults: true, SkillIDs: []string{"skill", "missing", "skill"}, ProjectSteering: true}}}
	n := &domain.Node{ID: "worker"}
	s, err := m.Resolve(t.Context(), g, n, root, "agent guidance", []string{"task guidance"})
	if err != nil {
		t.Fatal(err)
	}
	if s.Entries[0].Scope != "installation" || s.Entries[1].Scope != "graph" || s.Entries[len(s.Entries)-1].Scope != "task" {
		t.Fatal(s)
	}
	reasons := map[string]bool{}
	for _, e := range s.Entries {
		reasons[e.Reason] = true
		if strings.Contains(e.Body, "must never") {
			t.Fatal("escaped workspace")
		}
	}
	for _, r := range []string{"missing", "duplicate", "duplicate_content", "invalid_text", "size_limit", "unsafe_path", "unsupported_format"} {
		if !reasons[r] {
			t.Errorf("missing omission %s", r)
		}
	}
	other, err := m.Resolve(t.Context(), g, n, t.TempDir(), "agent", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range other.Entries {
		if e.Scope == "project" && e.Included {
			t.Fatal("cross-workspace leak")
		}
	}
	g.Spec.Instructions = domain.InstructionRefs{}
	s, err = m.Resolve(t.Context(), g, n, root, "agent", nil)
	if err != nil || len(s.Entries) != 1 {
		t.Fatal(s, err)
	}
}
func TestDirectorySymlinkAndCountLimit(t *testing.T) {
	m := manager(t)
	root := t.TempDir()
	os.Symlink(t.TempDir(), filepath.Join(root, ".weft"))
	g := &domain.Graph{Spec: domain.GraphSpec{Instructions: domain.InstructionRefs{ProjectSteering: true}}}
	n := &domain.Node{ID: "n"}
	s, err := m.Resolve(t.Context(), g, n, root, "agent", nil)
	if err != nil || s.Entries[0].Reason != "unsafe_path" {
		t.Fatal(s, err)
	}
}
func TestRepositoryCountAndSpecialFiles(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".weft", "steering")
	os.MkdirAll(dir, 0700)
	for i := 0; i <= MaxEntries; i++ {
		os.WriteFile(filepath.Join(dir, fmt.Sprintf("%03d.md", i)), []byte("text"), 0600)
	}
	s := &Snapshot{Budget: MaxTokens}
	project(s, root)
	if len(s.Entries) != 1 || s.Entries[0].Reason != "file_count_limit" {
		t.Fatal(s)
	}
	root = t.TempDir()
	dir = filepath.Join(root, ".weft", "steering")
	os.MkdirAll(filepath.Join(dir, "nested.md"), 0700)
	if err := syscall.Mkfifo(filepath.Join(dir, "pipe.md"), 0600); err != nil {
		t.Fatal(err)
	}
	s = &Snapshot{Budget: MaxTokens}
	project(s, root)
	for _, e := range s.Entries {
		if e.Reason != "unsafe_path" {
			t.Fatal(s)
		}
	}
}
func TestDisabledMissingAndCorruptContent(t *testing.T) {
	m := manager(t)
	in := Input{ID: "off", Title: "Off", Body: "disabled guidance", Enabled: false}
	if _, err := m.Save(t.Context(), in, 0, "admin", false); err != nil {
		t.Fatal(err)
	}
	g := &domain.Graph{Spec: domain.GraphSpec{Instructions: domain.InstructionRefs{SkillIDs: []string{"off", "missing"}}}}
	n := &domain.Node{ID: "n"}
	s, err := m.Resolve(t.Context(), g, n, "", "required", nil)
	if err != nil || s.Entries[0].Reason != "disabled" || s.Entries[1].Reason != "missing" {
		t.Fatal(s, err)
	}
	n.SkillIDs = domain.SkillSelection{}
	s, err = m.Resolve(t.Context(), g, n, "", "required", nil)
	if err != nil || len(s.Entries) != 1 {
		t.Fatal(s, err)
	}
	if _, err = m.Store.PutSetting(t.Context(), Prefix+"broken", []byte("not json"), 0, "fixture"); err != nil {
		t.Fatal(err)
	}
	g.Spec.Instructions.SkillIDs = []string{"broken"}
	n.SkillIDs = nil
	if _, err = m.Resolve(t.Context(), g, n, "", "required", nil); err == nil {
		t.Fatal("corrupt settings treated as missing")
	}
}
func TestRevisionLimitPreservesHistory(t *testing.T) {
	m := manager(t)
	in := Input{ID: "limited", Title: "Limited", Body: "text"}
	for rev := int64(0); rev < MaxEntries; rev++ {
		if _, err := m.Save(t.Context(), in, rev, "fixture", false); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := m.Save(t.Context(), in, MaxEntries, "fixture", false); !errors.Is(err, storage.ErrConflict) {
		t.Fatal(err)
	}
	s, err := m.Get(t.Context(), in.ID)
	if err != nil || s.Revision != MaxEntries || len(s.History) != MaxEntries-1 {
		t.Fatal(s, err)
	}
}

func TestBuiltinRoleSkillsAreReadOnlyAndResolvable(t *testing.T) {
	m := manager(t)
	all, err := m.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, s := range all {
		if s.Builtin {
			ids = append(ids, s.ID)
			if Validate(s.Input) != nil || s.Global || !s.Enabled || s.Revision != 1 || strings.TrimSpace(s.Body) == "" || len(s.Body) > MaxBytes {
				t.Fatalf("invalid built-in %+v", s.Version)
			}
		}
	}
	if strings.Join(ids, ",") != "weft-implementer,weft-planner,weft-reviewer,weft-verifier" {
		t.Fatalf("%v", ids)
	}
	if s, err := m.Get(t.Context(), "weft-implementer"); err != nil || !strings.Contains(s.Body, "filesystem.edit") {
		t.Fatalf("%v %v", s, err)
	}
	if _, err := m.Get(t.Context(), "weft-unknown"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatal(err)
	}
	for _, del := range []bool{false, true} {
		if _, err := m.Save(t.Context(), Input{ID: "weft-planner", Title: "x", Body: "x", Enabled: true}, 0, "admin", del); !errors.Is(err, ErrBuiltinReadOnly) {
			t.Fatalf("delete=%v: %v", del, err)
		}
	}
	g := &domain.Graph{Spec: domain.GraphSpec{Instructions: domain.InstructionRefs{SkillIDs: []string{"weft-planner"}}}}
	g.Metadata.ID = "g"
	snap, err := m.Resolve(t.Context(), g, &domain.Node{ID: "n"}, t.TempDir(), "plan the change", nil)
	if err != nil {
		t.Fatal(err)
	}
	if ctx := Context(*snap); !strings.Contains(ctx, "[graph weft-planner]") || !strings.Contains(ctx, "You are the planner") {
		t.Fatalf("%s", ctx)
	}
}
