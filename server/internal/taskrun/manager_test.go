package taskrun

import (
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/engine"
	"github.com/woliveiras/weft/server/internal/taskconfig"
	"testing"
)

func TestPreparePreservesAuthorityAndSeparatesIdentity(t *testing.T) {
	source := &domain.Graph{Metadata: domain.GraphMetadata{ID: "source", Version: 2}, Spec: domain.GraphSpec{Defaults: domain.NodeDefaults{Tools: []domain.ToolGrant{{Name: "filesystem.read", Paths: []string{"docs/**"}, RequiresApproval: true}}}, Nodes: []domain.Node{{ID: "worker", Runtime: domain.RuntimeRef{Kind: domain.RuntimeEcho}, Workspace: domain.NodeWorkspace{ReadOnly: true}}}}}
	d := &taskconfig.Draft{ID: "tsk_abc", Input: taskconfig.Input{Title: "Task", GraphVersion: 2, NodeID: "worker", ResultNodeID: "worker"}}
	copy, blocks, err := Prepare(d, source)
	if err != nil || len(blocks) > 0 {
		t.Fatal(blocks, err)
	}
	if copy.Metadata.ID != "task-run-abc" || copy.Metadata.Labels["weft.task"] != d.ID || !copy.Spec.Nodes[0].Workspace.ReadOnly || len(copy.Spec.Nodes[0].Tools) != 1 || !copy.Spec.Nodes[0].Tools[0].RequiresApproval {
		t.Fatal(copy)
	}
	copy.Spec.Nodes[0].Tools[0].Paths[0] = "other"
	if source.Metadata.ID != "source" || source.Spec.Defaults.Tools[0].Paths[0] != "docs/**" {
		t.Fatal("source mutated")
	}
}
func TestPrepareRejectsUnsupportedLaunches(t *testing.T) {
	for _, change := range []func(*domain.Graph){func(g *domain.Graph) { g.Metadata.Version = 2 }, func(g *domain.Graph) { g.Spec.Workspace.Path = "/existing" }, func(g *domain.Graph) {
		g.Spec.Environment = &domain.EnvironmentSpec{Origin: &domain.EnvironmentOrigin{Source: "agent"}}
	}, func(g *domain.Graph) { g.Spec.Nodes[0].Disabled = true }, func(g *domain.Graph) { g.Spec.Nodes[0].Loop.Mode = domain.LoopContinuous }} {
		g := &domain.Graph{Metadata: domain.GraphMetadata{ID: "source", Version: 1}, Spec: domain.GraphSpec{Nodes: []domain.Node{{ID: "worker", Runtime: domain.RuntimeRef{Kind: domain.RuntimeEcho}}}}}
		change(g)
		_, blocks, err := Prepare(&taskconfig.Draft{ID: "tsk_abc", Input: taskconfig.Input{GraphVersion: 1, NodeID: "worker", ResultNodeID: "worker"}}, g)
		if err != nil || len(blocks) == 0 {
			t.Fatal(blocks, err)
		}
	}
}

func TestPrepareChannelBinding(t *testing.T) {
	g := &domain.Graph{Metadata: domain.GraphMetadata{ID: "source", Version: 1}, Spec: domain.GraphSpec{Nodes: []domain.Node{{ID: "worker", Runtime: domain.RuntimeRef{Kind: domain.RuntimeEcho}}}, Channels: []domain.Channel{{ID: "requests", To: []string{"worker"}, Types: []string{"feature.*"}}}}}
	d := &taskconfig.Draft{ID: "tsk_x", Input: taskconfig.Input{GraphVersion: 1, NodeID: "worker", ResultNodeID: "worker", ChannelID: "requests", MessageType: "feature.request", PayloadJSON: `{"title":"test"}`}}
	if _, b, e := Prepare(d, g); e != nil || len(b) > 0 {
		t.Fatal(b, e)
	}
	d.MessageType = "other"
	if _, b, e := Prepare(d, g); e != nil || len(b) == 0 {
		t.Fatal(b, e)
	}
	d.MessageType = "feature.request"
	g.Spec.Channels[0].From = "worker"
	if _, b, e := Prepare(d, g); e != nil || len(b) == 0 {
		t.Fatal(b, e)
	}
}

func TestWorkspaceScopesOnlyKnownPreparedReplicas(t *testing.T) {
	g := &domain.Graph{Metadata: domain.GraphMetadata{ID: "task-run-fixture"}, Spec: domain.GraphSpec{Nodes: []domain.Node{{ID: "writer", Workspace: domain.NodeWorkspace{Mode: domain.WorkspaceWorktree, Subdir: "ignored"}}, {ID: "shared", Workspace: domain.NodeWorkspace{Subdir: "src"}}}}}
	st := &engine.GraphStatus{Nodes: []engine.NodeStatus{{Replicas: []*domain.ReplicaStatus{nil, {GraphID: g.Metadata.ID, NodeID: "writer", Replica: 1, Workdir: "/fixture/writer", Branch: "task-writer"}, {GraphID: g.Metadata.ID, NodeID: "shared", Workdir: "/fixture/src"}, {GraphID: "other", NodeID: "writer", Workdir: "/private"}, {GraphID: g.Metadata.ID, NodeID: "unknown", Workdir: "/unknown"}, {GraphID: g.Metadata.ID, NodeID: "writer"}}}}}
	scopes := workspaceScopes(g, st)
	if len(scopes) != 2 || scopes[0].InspectionNode != "writer" || scopes[0].Replica != 1 || scopes[0].Subdir != "" || scopes[1].InspectionNode != "" || scopes[1].Subdir != "src" {
		t.Fatalf("unexpected scopes: %+v", scopes)
	}
}

func TestPreparationFailureRequiresFailedReplicaInScope(t *testing.T) {
	st := &engine.GraphStatus{GraphID: "task", Nodes: []engine.NodeStatus{{Replicas: []*domain.ReplicaStatus{nil, {GraphID: "other", State: domain.ReplicaFailed}, {GraphID: "task", State: domain.ReplicaStarting}}}}}
	if preparationFailed(st) {
		t.Fatal("foreign or starting replica treated as failed")
	}
	st.Nodes[0].Replicas[2].State = domain.ReplicaFailed
	if !preparationFailed(st) {
		t.Fatal("missing failure")
	}
	st.Nodes[0].Replicas[2].State = domain.ReplicaCompleted
	if preparationFailed(st) {
		t.Fatal("completed replica treated as failed")
	}
}

func TestPrepareRetainsExplicitEnvironmentWithoutMutatingSource(t *testing.T) {
	source := &domain.Graph{Metadata: domain.GraphMetadata{ID: "source", Version: 1}, Spec: domain.GraphSpec{Environment: &domain.EnvironmentSpec{Image: "alpine:3.22", Setup: []string{"echo fixture"}}, Nodes: []domain.Node{{ID: "worker", Runtime: domain.RuntimeRef{Kind: domain.RuntimeEcho}, Isolation: domain.IsolationSpec{Mode: domain.IsolationDocker}}}}}
	d := &taskconfig.Draft{ID: "tsk_fixture", Input: taskconfig.Input{Title: "Environment", GraphVersion: 1, NodeID: "worker", ResultNodeID: "worker"}}
	prepared, blocks, err := Prepare(d, source)
	if err != nil || len(blocks) > 0 || prepared.Spec.Environment == nil || prepared.Spec.Nodes[0].Isolation.Mode != domain.IsolationDocker {
		t.Fatal(prepared, blocks, err)
	}
	prepared.Spec.Environment.Setup[0] = "changed"
	if source.Spec.Environment.Setup[0] != "echo fixture" {
		t.Fatal("source environment mutated")
	}
}

func TestIssueSourceRequiresExactEntryGrantAndPreservesTemplate(t *testing.T) {
	g := &domain.Graph{Metadata: domain.GraphMetadata{ID: "source", Version: 1}, Spec: domain.GraphSpec{Nodes: []domain.Node{{ID: "reader", Runtime: domain.RuntimeRef{Kind: domain.RuntimeNative}, Prompt: "Original"}}}}
	d := &taskconfig.Draft{ID: "tsk_issue", Input: taskconfig.Input{GraphVersion: 1, NodeID: "reader", ResultNodeID: "reader", Source: &taskconfig.IssueSource{Kind: "github", ConnectionID: "personal", Reference: "owner/repo#7"}}}
	if _, b, e := Prepare(d, g); e != nil || len(b) == 0 {
		t.Fatal(b, e)
	}
	g.Spec.Nodes[0].Tools = []domain.ToolGrant{{Name: "mcp.personal.issue_read", RequiresApproval: true}}
	prepared, b, e := Prepare(d, g)
	if e != nil || len(b) != 0 {
		t.Fatal(b, e)
	}
	if prepared.Spec.Nodes[0].Prompt == "Original" || g.Spec.Nodes[0].Prompt != "Original" || !prepared.Spec.Nodes[0].Tools[0].RequiresApproval {
		t.Fatal("source/grants changed or instructions absent")
	}
}
