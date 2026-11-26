package chat

import (
	"testing"

	"github.com/woliveiras/weft/server/internal/config"
)

func TestManualDestinationRequiresOneKnownPage(t *testing.T) {
	for _, body := range []string{
		`{"kind":"manual.open"}`,
		`{"kind":"manual.open","destination":"https://example.org"}`,
		`{"kind":"manual.open","destination":"connections","task":"one"}`,
		`{"kind":"manual.open","destination":"connections","question":"anything else?"}`,
		`{"kind":"task.watch","task":"one","destination":"connections"}`,
		`{"kind":"manual.open","destination":"connections","agentName":"another"}`,
	} {
		if _, err := decodePlan(body); err == nil {
			t.Errorf("accepted invalid navigation plan %s", body)
		}
	}
	plan, err := decodePlan(`{"kind":"manual.open","destination":"connection-setup","title":"Connect a service"}`)
	if err != nil {
		t.Fatal(err)
	}
	for role, want := range map[config.Role]string{config.RoleViewer: "connections", config.RoleOperator: "connections", config.RoleAdmin: "connection-setup"} {
		reply := ManualHandoff(plan, role)
		if reply.Destination != want || reply.Review != nil || reply.Task != nil {
			t.Errorf("role %s received %+v", role, reply)
		}
	}
}

func TestChatConfigurationExportRequiresAdministrator(t *testing.T) {
	plan, err := decodePlan(`{"kind":"manual.open","destination":"configuration-export"}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range []config.Role{config.RoleViewer, config.RoleOperator, config.RoleAdmin} {
		got := ManualHandoff(plan, role)
		if (got.Destination == "configuration-export") != (role == config.RoleAdmin) || got.Review != nil {
			t.Fatalf("role %s: %+v", role, got)
		}
	}
}

func TestSecureSetupHandoffsUseAppropriateManualDestination(t *testing.T) {
	for _, tc := range []struct {
		dest string
		role config.Role
		want string
	}{
		{"task-editor", config.RoleViewer, "tasks"}, {"task-editor", config.RoleOperator, "task-editor"},
		{"project-import", config.RoleViewer, "projects"}, {"project-import", config.RoleAdmin, "project-import"},
		{"source-control-setup", config.RoleOperator, "connections"}, {"source-control-setup", config.RoleAdmin, "source-control-setup"},
	} {
		got := ManualHandoff(Plan{Kind: OpenManual, Destination: tc.dest}, tc.role)
		if got.Destination != tc.want || got.Review != nil {
			t.Fatal(tc, got)
		}
	}
}
