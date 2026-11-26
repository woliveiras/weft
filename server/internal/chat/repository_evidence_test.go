package chat

import (
	"encoding/json"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/scm"
	"github.com/woliveiras/weft/server/internal/taskconfig"
	"github.com/woliveiras/weft/server/internal/taskrun"
	"strings"
	"testing"
)

func TestRepositoryEvidenceRejectsUnconfirmedPRsAndRawDetails(t *testing.T) {
	out := TaskSummary{ID: "tsk_sample"}
	d := &taskconfig.Draft{ID: out.ID, ExecutionGraphID: domain.TaskExecutionID(out.ID), Input: taskconfig.Input{Repositories: []domain.TaskRepository{{Alias: "docs", ProjectID: "prj_one", BaseCommit: strings.Repeat("a", 40), SCMConnection: "origin"}}}}
	v := taskrun.View{Task: d, Repositories: []taskrun.MemberProgress{{Alias: "docs", Preparation: "ready", Delivery: "pull_request unverified", Details: "raw provider payload", PullRequest: &scm.PullRequest{Number: 418, URL: "https://untrusted.invalid"}}}}
	ProjectTaskEvidence(&out, &v)
	b, _ := json.Marshal(out)
	if len(out.Repositories) != 1 || out.Repositories[0].PullRequest != 0 || strings.Contains(string(b), "raw provider") || strings.Contains(string(b), "untrusted.invalid") {
		t.Fatal(string(b))
	}
	out.Repositories = nil
	v.Repositories[0].Delivery = "pull_request confirmed"
	ProjectTaskEvidence(&out, &v)
	if out.Repositories[0].PullRequest != 418 {
		t.Fatal(out)
	}
	out.Repositories = nil
	d.ExecutionGraphID = "other"
	ProjectTaskEvidence(&out, &v)
	if len(out.Repositories) != 0 {
		t.Fatal(out)
	}
}
