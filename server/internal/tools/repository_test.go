package tools

import (
	"encoding/json"
	"errors"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage/fsstore"
	"github.com/woliveiras/weft/server/internal/workspace"
)

func TestMemberToolRequiresReviewedAlias(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "infra"), 0700); err != nil {
		t.Fatal(err)
	}
	p := &Principal{Workdir: root, Repositories: []domain.TaskRepository{{Alias: "infra"}}, Grants: []domain.ToolGrant{{Name: "git.read"}, {Name: "checkpoint.list"}}}
	reg := Builtin(BuiltinConfig{})
	reg.MustRegister(CheckpointTools(nil, nil)...)
	for _, tool := range reg.Schemas(p) {
		if (tool.Name == "checkpoint.list" || tool.Name == "git.status") && !strings.Contains(string(tool.Parameters), `"required":["repository"]`) {
			t.Fatal(tool.Name, string(tool.Parameters))
		}
	}
	for _, name := range []string{"", "backend", "../infra"} {
		args, _ := json.Marshal(map[string]string{"repository": name})
		if _, err := gitRepository(&Invocation{Principal: p, Workdir: root, Args: args}); !errors.Is(err, ErrDenied) {
			t.Fatal("ambiguous or unknown member", name, err)
		}
	}
	if got, err := gitRepository(&Invocation{Principal: p, Workdir: root, Args: json.RawMessage(`{"repository":"infra"}`)}); err != nil || got != "infra" {
		t.Fatal(got, err)
	}
	if _, _, err := jailWrite(&Invocation{Principal: p, Workdir: root}, "infra"); !errors.Is(err, ErrDenied) {
		t.Fatal("allowed replacing checkout mount", err)
	}
}

func TestCheckNeedsEveryRecordedMember(t *testing.T) {
	states := map[string]CheckWorkspace{"backend": {Head: "a", State: "1"}, "frontend": {Head: "b", State: "2"}}
	run := CheckRun{Passed: true, Repositories: states}
	if !run.Current(states) {
		t.Fatal("exact evidence refused")
	}
	for _, candidate := range []map[string]CheckWorkspace{
		{"backend": states["backend"]},
		{"backend": states["backend"], "frontend": {Head: "b", State: "modified"}},
		{"backend": states["backend"], "frontend": {Head: "b", State: "2", Partial: true}},
	} {
		if run.Current(candidate) {
			t.Fatal("incomplete or stale evidence accepted", candidate)
		}
	}
}

func TestRepositoryCheckEvidenceOutcomes(t *testing.T) {
	store, err := fsstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	current := map[string]workspace.RepositoryState{"infra": {Head: "a", State: "1"}, "backend": {Head: "b", State: "2"}}
	snapshots := map[string]CheckWorkspace{"infra": {Head: "a", State: "1"}, "backend": {Head: "b", State: "2"}}
	for _, row := range []CheckRun{
		{Check: "infra.pass", Passed: true, Repositories: snapshots},
		{Check: "infra.fail", Repositories: snapshots},
		{Check: "infra.changed", Passed: true, Repositories: map[string]CheckWorkspace{"infra": snapshots["infra"]}},
		{Check: "infra.unavailable", InfraError: true},
	} {
		if err := store.AppendEvent(t.Context(), domain.NewEvent(domain.EvCheckCompleted, "g", row)); err != nil {
			t.Fatal(err)
		}
	}
	names := []string{"infra.pass", "infra.fail", "infra.changed", "infra.unavailable", "infra.missing"}
	got, err := RepositoryCheckEvidence(t.Context(), store, "g", names, current)
	want := map[string]string{"infra.pass": "passed", "infra.fail": "failed", "infra.changed": "stale", "infra.unavailable": "could not run", "infra.missing": "not run"}
	if err != nil || !maps.Equal(got, want) {
		t.Fatal(got, err)
	}
}

func TestCheckGatewayCapturesMemberMutationAndUnavailableRunner(t *testing.T) {
	root := t.TempDir()
	var repos []domain.TaskRepository
	for _, alias := range []string{"infra", "backend"} {
		dir := filepath.Join(root, alias)
		for _, args := range [][]string{
			{"init", "-q", dir}, {"-C", dir, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "--allow-empty", "-qm", "base"},
		} {
			if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
				t.Fatal(string(out), err)
			}
		}
		head, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
		if err != nil {
			t.Fatal(err)
		}
		repos = append(repos, domain.TaskRepository{Alias: alias, BaseCommit: strings.TrimSpace(string(head))})
	}
	inv := checkInvocation(&ProcessSandbox{Root: root}, domain.ToolGrant{Name: "shell.exec"}, domain.Check{Name: "infra.joint", Cwd: "infra", Command: "touch ../backend/changed"})
	inv.Args = json.RawMessage(`{"name":"infra.joint"}`)
	inv.Principal.Repositories = repos
	inv.Workdir, inv.Principal.Workdir = root, root
	result, err := (checkTool{}).Invoke(t.Context(), inv)
	if err != nil {
		t.Fatal(err)
	}
	var run CheckRun
	if err = json.Unmarshal(result.Data, &run); err != nil || !run.Stale || !run.Passed || run.Repositories["infra"].Partial || run.Repositories["backend"].Partial {
		t.Fatal(run, err)
	}
	inv.Sandbox = &scriptedSandbox{err: errors.New("sandbox unavailable")}
	result, err = (checkTool{}).Invoke(t.Context(), inv)
	if err != nil {
		t.Fatal(err)
	}
	var failed CheckRun
	if err = json.Unmarshal(result.Data, &failed); err != nil || !failed.InfraError || failed.Passed || len(failed.Repositories) != 2 {
		t.Fatal(failed, err)
	}
}
