package taskrun

import (
	"github.com/woliveiras/weft/server/internal/environment"
	"github.com/woliveiras/weft/server/internal/taskconfig"
	"testing"
	"time"
)

func TestPreparedBuildRequiresExactTaskAndReadyDefinition(t *testing.T) {
	when := time.Now()
	r := Review{Task: &taskconfig.Draft{ID: "tsk_fixture", Status: "environment_pending"}, Environment: &environment.View{Status: environment.Status{Graph: ExecutionID("tsk_fixture"), Hash: "env1", FinishedAt: &when}, CurrentHash: "env1", UpToDate: true}}
	if v := r.PreparedBuild(); v == nil || v.Completed != when.UTC().Format(time.RFC3339Nano) {
		t.Fatal(v)
	}
	r.Environment.CurrentHash = "env2"
	if r.PreparedBuild() != nil {
		t.Fatal("accepted stale build")
	}
	r.Environment.CurrentHash = "env1"
	r.Environment.Graph = "other"
	if r.PreparedBuild() != nil {
		t.Fatal("accepted a different task")
	}
}
