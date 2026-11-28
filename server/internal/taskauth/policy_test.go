package taskauth

import (
	"errors"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"

	"github.com/woliveiras/weft/server/internal/tools"
)

func TestApproval(t *testing.T) {
	for _, tc := range []struct {
		mode                        Mode
		capability, isolation       string
		mandatory, approval, denied bool
	}{
		{Ask, "filesystem.read", "process", false, true, false},
		{Workspace, "filesystem.write", "process", false, false, false},
		{Workspace, "shell.exec", "process", false, false, true},
		{Workspace, "shell.exec", "docker", false, false, false},
		{Workspace, "shell.exec", "kubernetes", false, false, true},
		{Workspace, "shell.exec", "kubernetes", true, false, true},
		{Ask, "shell.exec", "kubernetes", false, true, false},
		{Workspace, "mcp.remote.read", "docker", false, true, false},
		{Workspace, "git.write", "process", false, true, false},
		{Full, "shell.exec", "process", false, false, false},
		{Full, "mcp.remote.write", "process", true, true, false},
		{"invalid", "filesystem.read", "process", false, false, true},
	} {
		t.Run(string(tc.mode)+"/"+tc.capability+"/"+tc.isolation, func(t *testing.T) {
			got, err := Approval(tc.mode, tc.capability, tc.isolation, tc.mandatory)
			if (err != nil) != tc.denied || got != tc.approval {
				t.Fatalf("%v %v", got, err)
			}
		})
	}
}

func TestApprovalExplainsWorkspaceNativeShellDenial(t *testing.T) {
	for _, kind := range []string{"process", "kubernetes", ""} {
		approval, err := Approval(Workspace, "shell.exec", kind, true)
		if approval || !errors.Is(err, ErrDenied) || !errors.Is(err, tools.ErrWorkspaceShellIsolation) {
			t.Errorf("sandbox %q: approval %t, error %v", kind, approval, err)
		}
	}
}

func TestReduction(t *testing.T) {
	for _, from := range []Mode{Ask, Workspace, Full, "bad"} {
		for _, to := range []Mode{Ask, Workspace, Full, "bad"} {
			want := (from == Full && (to == Ask || to == Workspace)) || (from == Workspace && to == Ask)
			if Reduction(from, to) != want {
				t.Fatal(from, to)
			}
		}
	}
}

func TestProfileRoutineDecisions(t *testing.T) {
	for _, tc := range []struct {
		name, kind string
		want       bool
	}{
		{"filesystem.write", "process", true}, {"filesystem.delete", "docker", false}, {"git.commit", "process", true}, {"check.run", "docker", true}, {"check.run", "process", false}, {"shell.exec", "docker", false}, {"mcp.custom", "docker", false},
	} {
		if got := routineTool(tc.name, tc.kind); got != tc.want {
			t.Errorf("%s/%s got routine=%t", tc.name, tc.kind, got)
		}
	}
	repos := []domain.TaskRepository{{SCMConnection: "work", ConnectionRevision: "rev", Repository: "team/repo", TargetBranch: "main"}}
	for _, tc := range []struct {
		args string
		want bool
	}{{`{"connection":"work"}`, true}, {`{"connection":"other"}`, false}, {"{", false}} {
		if got := boundDelivery(repos, []byte(tc.args)); got != tc.want {
			t.Errorf("%s got bound=%t", tc.args, got)
		}
	}
	repos[0].TargetBranch = ""
	if boundDelivery(repos, []byte(`{"connection":"work"}`)) {
		t.Fatal("missing bound destination")
	}
	for _, tc := range []struct {
		from, to Mode
		allowed  bool
	}{{Automatic, Assisted, true}, {Automatic, Manual, true}, {Assisted, Manual, true}, {Manual, Automatic, false}, {Assisted, Automatic, false}, {Assisted, Full, false}, {Automatic, Automatic, false}} {
		if Reduction(tc.from, tc.to) != tc.allowed {
			t.Errorf("unexpected reduction %s to %s", tc.from, tc.to)
		}
	}
}
