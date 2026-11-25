package api_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/projectconfig"
	"github.com/woliveiras/weft/server/internal/taskauth"
	"github.com/woliveiras/weft/server/internal/taskconfig"
	"github.com/woliveiras/weft/server/internal/taskrun"
)

func TestRetryLongIdentityCanReviewPrepareAndStart(t *testing.T) {
	for _, sandbox := range []bool{false, true} {
		t.Run(fmt.Sprint("sandbox-", sandbox), func(t *testing.T) {
			bin, _ := fakeDaemonCLI(t)
			srv, a := setupApp(t, func(c *config.Config) { c.Isolation.Docker.Bin = bin })
			repo := t.TempDir()
			for _, args := range [][]string{{"init", "-q"}, {"-c", "user.name=Fixture", "-c", "user.email=fixture@example.test", "commit", "--allow-empty", "-qm", "initial"}} {
				cmd := exec.Command("git", args...)
				cmd.Dir = repo
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatal(err, string(out))
				}
			}
			projectBody, _ := json.Marshal(projectconfig.Input{Name: "Retry repository", Path: repo})
			var project projectconfig.Project
			if c := call(t, srv, "POST", "/api/v1/projects", adminTok, "application/json", string(projectBody), &project); c != 201 {
				t.Fatal(c)
			}
			var workflow domain.Graph
			if err := json.Unmarshal([]byte(taskGraph), &workflow); err != nil {
				t.Fatal(err)
			}
			if sandbox {
				workflow.Spec.Environment = &domain.EnvironmentSpec{Image: "alpine:3.22"}
				workflow.Spec.Nodes[0].Isolation.Mode = domain.IsolationDocker
			}
			body, _ := json.Marshal(workflow)
			if c := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/json", string(body), nil); c != 201 {
				t.Fatal(c)
			}
			var input taskconfig.Input
			if err := json.Unmarshal([]byte(taskBody), &input); err != nil {
				t.Fatal(err)
			}
			input.ResultNodeID = input.NodeID
			input.ProjectID = project.ID
			input.BaseCommit = project.Head
			if sandbox {
				input.ExecutionMode = "sandbox"
			}
			body, _ = json.Marshal(input)
			var source taskconfig.Draft
			if c := call(t, srv, "POST", "/api/v1/tasks", operatorTok, "application/json", string(body), &source); c != 201 {
				t.Fatal(c)
			}
			failed, err := (taskconfig.Manager{Store: a.Store}).Transition(t.Context(), &source, "failed", "fixture")
			if err != nil {
				t.Fatal(err)
			}
			c, _, child := retryTaskRequest(t, srv.URL, source.ID, operatorTok, fmt.Sprint(failed.Revision), `{"confirm":true}`)
			if c != 201 || len(child.ID) != 68 {
				t.Fatal("retry identity changed", c, child)
			}
			c, _, replay := retryTaskRequest(t, srv.URL, source.ID, operatorTok, fmt.Sprint(failed.Revision), `{"confirm":true}`)
			if c != 200 || replay.ID != child.ID {
				t.Fatal("existing retry identity not preserved", c, replay)
			}
			var review taskrun.Review
			if c = call(t, srv, "GET", "/api/v1/tasks/"+child.ID+"/review", viewerTok, "", "", &review); c != 200 || len(review.ExecutionGraphID) > 63 {
				t.Fatal("invalid retry graph review", c, review)
			}
			for _, validation := range review.Validation.Errors {
				if !sandbox || validation.Path != "spec.environment" || !strings.HasPrefix(validation.Message, "environment not ready:") {
					t.Fatal("unexpected retry validation", validation)
				}
			}
			if sandbox {
				if !review.CanPrepareEnvironment {
					t.Fatal("retry environment cannot be prepared", review)
				}
				if c = prepareTaskEnvironment(t, srv.URL, *child, adminTok); c != 202 {
					t.Fatal(c)
				}
				waitFor(t, 5*time.Second, "retry environment ready", func() bool {
					var v taskrun.View
					call(t, srv, "GET", "/api/v1/tasks/"+child.ID+"/progress", viewerTok, "", "", &v)
					return v.Environment != nil && v.Environment.UpToDate
				})
				if c = call(t, srv, "GET", "/api/v1/tasks/"+child.ID+"/review", viewerTok, "", "", &review); c != 200 {
					t.Fatal(c)
				}
			}
			if !review.CanStart || len(review.Validation.Errors) > 0 {
				t.Fatal("retry blocked by identity", review.Blockers, review.Validation)
			}
			if c = taskStart(t, srv.URL, child.ID, adminTok, review.Task.Revision); c != 202 {
				t.Fatal(c)
			}
			var progress taskrun.View
			waitFor(t, 5*time.Second, "retry result", func() bool {
				call(t, srv, "GET", "/api/v1/tasks/"+child.ID+"/progress", viewerTok, "", "", &progress)
				return progress.State == "succeeded" && len(progress.Executions) == 1
			})
			g, err := a.Store.GetGraph(t.Context(), progress.Task.ExecutionGraphID, 0)
			if err != nil || len(g.Metadata.ID) > 63 || g.Metadata.Labels[domain.LabelTask] != child.ID {
				t.Fatal("retry copy ownership", g, err)
			}
			if _, err = os.Stat(filepath.Join(progress.WorkspacePath, ".git")); err != nil {
				t.Fatal("retry project checkout was not created", err)
			}
			var authorization taskauth.Review
			if c = call(t, srv, "GET", "/api/v1/tasks/"+child.ID+"/authorization", viewerTok, "", "", &authorization); c != 200 || authorization.Record == nil || authorization.Record.Mode != taskauth.Ask {
				t.Fatal("fresh retry authorization", c, authorization)
			}
		})
	}
}
