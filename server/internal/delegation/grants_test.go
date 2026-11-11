package delegation

import (
	"github.com/woliveiras/weft/server/internal/domain"
	"reflect"
	"testing"
)

func TestInheritedGrantsPreserveEveryConstraintAndOwnTheirSlices(t *testing.T) {
	parent := []domain.ToolGrant{{Name: "filesystem.read", Paths: []string{"docs/**"}, RequiresApproval: true, RateLimit: 2}, {Name: "shell.exec", Commands: []string{"go test"}, Hosts: []string{"example.com"}, RequiresApproval: true, RateLimit: 1}, {Name: "mcp.personal.get_me"}, {Name: "filesystem.write"}}
	got, err := InheritGrants(parent, []string{"filesystem.read", "shell.exec", "mcp.personal.get_me"})
	if err != nil || !reflect.DeepEqual(got, parent[:3]) {
		t.Fatalf("%+v %v", got, err)
	}
	got[0].Paths[0] = "**"
	got[1].Commands[0] = "*"
	got[1].Hosts[0] = "*"
	if parent[0].Paths[0] != "docs/**" || parent[1].Commands[0] != "go test" || parent[1].Hosts[0] != "example.com" {
		t.Fatal("aliased parent grants")
	}
	empty, err := InheritGrants(parent, nil)
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("%v %v", empty, err)
	}
}
func TestInheritedGrantsRejectAmplificationAndUnsupportedControls(t *testing.T) {
	parent := []domain.ToolGrant{{Name: "*"}, {Name: "mcp.work.*"}, {Name: "filesystem.read"}}
	for _, names := range [][]string{{"*"}, {"mcp.work.read"}, {"filesystem.write"}, {"filesystem.read", "filesystem.read"}, {"review.request"}, {"environment.propose"}, {"environment.status"}, {"bad"}, make([]string, 33)} {
		if _, err := InheritGrants(parent, names); err == nil {
			t.Fatalf("accepted %v", names)
		}
	}
}
