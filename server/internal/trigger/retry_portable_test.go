package trigger_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/taskconfig"
	"github.com/woliveiras/weft/server/internal/trigger"
)

func TestPortableStoppedTaskRetryPreservesIdentityAndDeliveryFences(t *testing.T) {
	for _, kind := range []string{"jira", "jira-dc", "github"} {
		for _, scenario := range []string{"undelivered", "delivered", "running", "pending-approval", "active-execution", "wrong-connection", "wrong-instance", "wrong-key", "wrong-workflow", "wrong-project", "dependency-failure"} {
			t.Run(kind+"/"+scenario, func(t *testing.T) {
				if scenario == "wrong-project" && kind != "jira-dc" {
					return
				}
				st := triggerStore(t)
				m := trigger.Manager{Store: st}
				tr, err := m.Create(t.Context(), input(), "alice")
				if err != nil {
					t.Fatal(err)
				}
				source := trigger.SourceConfig{Kind: kind, ConnectionID: "issues"}
				origin := taskconfig.IssueOrigin{Kind: kind, ConnectionID: source.ConnectionID, ConnectionRevision: 1, Key: "APP-7"}
				switch kind {
				case "jira":
					source.CloudID = "11111111-2222-3333-4444-555555555555"
					origin.Instance = "https://api.atlassian.com/ex/jira/" + source.CloudID
				case "jira-dc":
					source.SiteURL = "https://jira.example/context"
					source.ProjectID = "10001"
					origin.Instance = source.SiteURL
					origin.ProjectID = source.ProjectID
				case "github":
					source.Repository = "acme/app"
					origin.Instance = "https://api.github.com"
					origin.Key = "acme/app#7"
				}
				tm := taskconfig.Manager{Store: st}
				d, err := tm.SaveTriggeredIssue(t.Context(), taskconfig.Input{Title: "Issue", Request: "Fix parser", Outcome: "implement", GraphID: "factory", GraphVersion: 1, NodeID: "planner"}, "alice", origin)
				if err != nil {
					t.Fatal(err)
				}
				d.ExecutionGraphID = domain.TaskRunGraphPrefix + strings.TrimPrefix(d.ID, "tsk_")
				d, err = tm.Transition(t.Context(), d, "cancelled", "alice")
				if err != nil {
					t.Fatal(err)
				}
				g := &domain.Graph{Metadata: domain.GraphMetadata{ID: d.ExecutionGraphID, Name: "Task execution", Labels: map[string]string{domain.LabelTask: d.ID}}, Spec: domain.GraphSpec{Nodes: []domain.Node{{ID: "planner", Runtime: domain.RuntimeRef{Kind: domain.RuntimeEcho}}}}}
				if _, err = st.SaveGraph(t.Context(), g, 0); err != nil {
					t.Fatal(err)
				}
				rec := &domain.GraphRecord{ID: d.ExecutionGraphID, State: domain.GraphStopped, DesiredState: domain.GraphStopped}
				if scenario == "running" {
					rec.State = domain.GraphRunning
				}
				if err = st.PutGraphRecord(t.Context(), rec); err != nil {
					t.Fatal(err)
				}
				execution := &domain.Execution{ID: "exe_original", GraphID: d.ExecutionGraphID, NodeID: "planner", Status: domain.ExecFailed, StartedAt: time.Now().UTC()}
				if scenario == "active-execution" {
					execution.Status = domain.ExecRunning
				}
				if err = st.PutExecution(t.Context(), execution); err != nil {
					t.Fatal(err)
				}
				if scenario == "delivered" {
					if err = st.AppendEvent(t.Context(), domain.NewEvent(domain.EvPullRequestOpened, d.ExecutionGraphID, map[string]any{"url": "https://bitbucket.org/acme/app/pull-requests/1"})); err != nil {
						t.Fatal(err)
					}
				}
				if scenario == "pending-approval" {
					if err = st.PutApproval(t.Context(), &domain.Approval{ID: "apr_pending", GraphID: d.ExecutionGraphID, Status: domain.ApprovalPending}); err != nil {
						t.Fatal(err)
					}
				}
				key := trigger.DedupKey(&trigger.Trigger{Workflow: tr.Workflow, Source: source}, trigger.Issue{Source: kind, Key: origin.Key})
				tr, err = m.Update(t.Context(), tr.ID, "alice", func(current *trigger.Trigger) error {
					current.Source = source
					current.State = trigger.Paused
					current.Processed = map[string]string{key: d.ID}
					switch scenario {
					case "wrong-connection":
						current.Source.ConnectionID = "other-issues"
					case "wrong-instance":
						if kind == "jira" {
							current.Source.CloudID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
						} else if kind == "jira-dc" {
							current.Source.SiteURL = "https://other.example/context"
						} else {
							current.Source.Repository = "other/app"
						}
					case "wrong-workflow":
						current.Workflow = "other-workflow"
					case "wrong-project":
						current.Source.ProjectID = "10002"
					}
					return nil
				})
				if err != nil {
					t.Fatal(err)
				}
				issue := origin.Key
				if scenario == "wrong-key" {
					issue = "APP-8"
					if kind == "github" {
						issue = "acme/app#8"
					}
				}
				before, err := tm.Get(t.Context(), d.ID)
				if err != nil {
					t.Fatal(err)
				}
				if scenario == "dependency-failure" {
					m.Store = retryGraphFailureStore{Store: st}
				}
				out, err := m.RetryFailed(t.Context(), tr.ID, issue, d.ID, "alice")
				if scenario != "undelivered" {
					if scenario == "dependency-failure" {
						if !errors.Is(err, errRetryGraphDependency) {
							t.Fatalf("storage failure not propagated: %v", err)
						}
					} else if !errors.Is(err, storage.ErrConflict) {
						t.Fatalf("unsafe retry accepted: %v", err)
					}
					stored, e := m.Get(t.Context(), tr.ID)
					if e != nil || len(stored.FailedRetries) != 0 {
						t.Fatal("rejected retry mutated trigger", stored, e)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				retry := out.FailedRetries[key]
				if retry.TaskID != d.ID || retry.Generation == "" || retry.Consumed || out.Processed[key] != d.ID {
					t.Fatal("retry lost identity or outcome", out)
				}
				restarted := trigger.Manager{Store: st}
				if _, err = restarted.RetryFailed(t.Context(), tr.ID, issue, d.ID, "alice"); !errors.Is(err, storage.ErrConflict) {
					t.Fatal("retry replay accepted", err)
				}
				after, e := tm.Get(t.Context(), d.ID)
				if e != nil || !reflect.DeepEqual(before, after) {
					t.Fatal("retry rewrote historical task", e)
				}
				retained, e := st.GetGraph(t.Context(), d.ExecutionGraphID, 0)
				if e != nil || !reflect.DeepEqual(g, retained) {
					t.Fatal("retry rewrote execution copy", e)
				}
				executions, e := st.ListExecutions(t.Context(), storage.ExecutionFilter{GraphID: d.ExecutionGraphID})
				if e != nil || len(executions) != 1 || executions[0].ID != execution.ID || executions[0].Status != execution.Status {
					t.Fatal("retry rewrote executions", e)
				}
			})
		}
	}
}

var errRetryGraphDependency = errors.New("fixture graph storage unavailable")

type retryGraphFailureStore struct{ storage.Store }

func (s retryGraphFailureStore) GetGraph(context.Context, string, int) (*domain.Graph, error) {
	return nil, errRetryGraphDependency
}

func TestJiraCloudRetryGenerationIsConsumedOnlyAfterVerifiedRequeue(t *testing.T) {
	st := triggerStore(t)
	c := &clock{t: time.Now().UTC()}
	jira := newTracker("jira-team")
	issue := jiraIssue("APP-7", "Fix parser", "weft")
	issue["fields"].(map[string]any)["project"] = map[string]any{"id": "101"}
	jira.issues = []map[string]any{issue}
	r := runner(st, c, "jira-retry", jira)
	starter := &tasks{fail: errors.New("fixture admission failure")}
	r.Tasks = starter
	in := input()
	in.Source = trigger.SourceConfig{Kind: "jira", ConnectionID: jira.conn, CloudID: "11111111-2222-3333-4444-555555555555"}
	tr, err := r.Triggers.Create(t.Context(), in, "alice")
	if err != nil {
		t.Fatal(err)
	}
	tm := taskconfig.Manager{Store: st}
	original, err := tm.SaveTriggeredIssue(t.Context(), taskconfig.Input{Title: "Issue", Request: "Fix parser", Outcome: "implement", GraphID: "factory", GraphVersion: 1, NodeID: "planner"}, "alice", taskconfig.IssueOrigin{Kind: "jira", ConnectionID: jira.conn, ConnectionRevision: 1, Instance: "https://api.atlassian.com/ex/jira/" + in.Source.CloudID, Key: "APP-7"})
	if err != nil {
		t.Fatal(err)
	}
	tr = advance(r, c, t, tr.ID)
	admissions, err := st.ListSettings(t.Context(), "trigger-admissions/")
	if err != nil || len(admissions) != 1 {
		t.Fatal(admissions, err)
	}
	first := admissions[0]
	tr, err = r.Triggers.RetryFailed(t.Context(), tr.ID, "APP-7", original.ID, "alice")
	if err != nil {
		t.Fatal(err)
	}
	key := trigger.DedupKey(tr, trigger.Issue{Source: "jira", Key: "APP-7"})
	retry := tr.FailedRetries[key]
	if retry.Generation == "" || retry.Consumed {
		t.Fatal("missing generation", tr)
	}
	if _, err = r.Triggers.Resume(t.Context(), tr.ID, "alice"); err != nil {
		t.Fatal(err)
	}
	starter.fail = nil
	jira.find("APP-7")["fields"].(map[string]any)["labels"] = []string{"weft", trigger.LabelTodo}
	tr = advance(r, c, t, tr.ID)
	if len(starter.started) != 0 || tr.FailedRetries[key].Consumed {
		t.Fatal("old label authorized retry", tr)
	}
	history := func(accountType string, at time.Time) []map[string]any {
		return []map[string]any{{"id": "77", "created": at.Format(time.RFC3339Nano), "author": map[string]any{"accountId": "maintainer", "accountType": accountType, "active": true}, "items": []any{map[string]string{"field": "labels", "fromString": "weft", "toString": "weft, weft:todo"}}}}
	}
	jira.labelEvents["APP-7"] = history("atlassian", retry.RecordedAt.Add(-time.Second))
	tr = advance(r, c, t, tr.ID)
	if len(starter.started) != 0 {
		t.Fatal("historical event authorized retry")
	}
	jira.labelEvents["APP-7"] = history("app", c.t.Add(time.Second))
	tr = advance(r, c, t, tr.ID)
	if len(starter.started) != 0 {
		t.Fatal("app actor authorized retry")
	}
	jira.labelEvents["APP-7"] = history("atlassian", c.t.Add(time.Second))
	tr = advance(r, c, t, tr.ID)
	if len(starter.started) != 1 || tr.CurrentTask == "" || !tr.FailedRetries[key].Consumed || tr.Processed[key] != tr.CurrentTask {
		t.Fatalf("verified Jira retry not consumed: %+v %+v", tr, starter)
	}
	after, err := st.ListSettings(t.Context(), "trigger-admissions/")
	if err != nil || len(after) != 2 {
		t.Fatal("fresh generation missing", after, err)
	}
	retained, err := st.GetSetting(t.Context(), first.Key)
	if err != nil || string(retained.Value) != string(first.Value) || retained.Revision != first.Revision {
		t.Fatal("original admission changed", err)
	}
	tr = advance(r, c, t, tr.ID)
	if len(starter.started) != 1 {
		t.Fatal("generation replayed", tr)
	}
}
