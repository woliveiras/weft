package trigger_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/jiracloud"
	"github.com/woliveiras/weft/server/internal/jiradc"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/storage/fsstore"
	"github.com/woliveiras/weft/server/internal/tools"
	"github.com/woliveiras/weft/server/internal/trigger"
)

const workflowCloudID = "11111111-1111-1111-1111-111111111111"

type cloudWorkflowAPI struct {
	store        storage.Store
	tracker      *tracker
	posts        int
	denied, lost bool
}

func (f *cloudWorkflowAPI) Revision(ctx context.Context, id, kind string) (int64, error) {
	if id != "jira-mcp" || kind != "jira" {
		return 0, tools.ErrDenied
	}
	v, err := f.store.GetSetting(ctx, "mcp/connections/"+id)
	if err != nil {
		return 0, err
	}
	return v.Revision, nil
}
func (f *cloudWorkflowAPI) Call(ctx context.Context, id, kind string, revision int64, method, path string, body any) (json.RawMessage, int, error) {
	rev, err := f.Revision(ctx, id, kind)
	if err != nil || rev != revision {
		return nil, 0, storage.ErrConflict
	}
	u, err := url.Parse(path)
	if err != nil || !strings.HasPrefix(u.Path, "/ex/jira/"+workflowCloudID+"/rest/api/3/") {
		return nil, 0, tools.ErrDenied
	}
	if strings.HasSuffix(u.Path, "/transitions") {
		if method == "GET" {
			if f.denied {
				return nil, 403, errors.New("fixture transition denied")
			}
			return json.RawMessage(`{"transitions":[{"id":"32","name":"Working","to":{"id":"15","name":"Working"},"fields":{}},{"id":"31","name":"Révision","to":{"id":"20","name":"Review"},"fields":{}}]}`), 200, nil
		}
		raw, _ := json.Marshal(body)
		var in struct {
			Transition struct {
				ID string `json:"id"`
			} `json:"transition"`
		}
		if method != "POST" || json.Unmarshal(raw, &in) != nil {
			return nil, 400, errors.New("unexpected transition")
		}
		f.posts++
		target, category := "20", "done"
		if in.Transition.ID == "32" {
			target, category = "15", "indeterminate"
		}
		f.tracker.mu.Lock()
		status := f.tracker.issues[0]["fields"].(map[string]any)["status"].(map[string]any)
		status["id"] = target
		status["statusCategory"] = map[string]any{"key": category}
		f.tracker.mu.Unlock()
		if f.lost {
			return nil, 0, errors.New("response lost after remote transition")
		}
		return nil, 204, nil
	}
	return f.tracker.request(ctx, id, kind, revision, method, path, body)
}

type persistedCloudTasks struct {
	tasks
	store storage.Store
}

func (f *persistedCloudTasks) Status(ctx context.Context, id string) (trigger.TaskStatus, error) {
	s := trigger.TaskStatus{State: "running"}
	if x, err := f.store.GetExecution(ctx, "exe-cloud"); err == nil && x.RuntimeStartedAt != nil {
		s.WorkExecutionID = x.ID
	}
	events, err := f.store.ListEvents(ctx, storage.EventFilter{Types: []domain.EventType{domain.EvPullRequestOpened}, GraphID: id, Limit: 1})
	if err != nil {
		return s, err
	}
	if len(events) > 0 {
		s.State = "succeeded"
		s.DeliveryEventID = events[0].ID
		s.PullRequestURL = "https://bitbucket.example/pr/2"
	}
	return s, nil
}

type cloudLifecycleRig struct {
	root     string
	store    storage.Store
	r        *trigger.Runner
	c        *clock
	tr       *tracker
	api      *cloudWorkflowAPI
	profiles jiracloud.Profiles
	tasks    *persistedCloudTasks
	in       trigger.Input
}

func cloudRig(t *testing.T) *cloudLifecycleRig {
	t.Helper()
	f := &cloudLifecycleRig{root: t.TempDir(), c: &clock{t: time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)}, tr: newTracker("jira-mcp")}
	var err error
	f.store, err = fsstore.Open(f.root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := f.store.Close(); err != nil {
			t.Error(err)
		}
	})
	g := &domain.Graph{Metadata: domain.GraphMetadata{ID: "factory"}, Spec: domain.GraphSpec{Nodes: []domain.Node{{ID: "planner", Runtime: domain.RuntimeRef{Kind: domain.RuntimeEcho}}, {ID: "integrator", Runtime: domain.RuntimeRef{Kind: domain.RuntimeEcho}}}}}
	if _, err = f.store.SaveGraph(t.Context(), g, 0); err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.PutSetting(t.Context(), "mcp/connections/jira-mcp", []byte(`{}`), 0, "fixture"); err != nil {
		t.Fatal(err)
	}
	issue := jiraIssue("APP-7", "Review parser", "weft")
	issue["id"], issue["self"] = "10007", "https://team.atlassian.net/rest/api/3/issue/10007"
	fields := issue["fields"].(map[string]any)
	fields["project"] = map[string]any{"id": "10001"}
	fields["issuetype"] = map[string]any{"id": "10002"}
	fields["updated"] = "2026-11-20T09:00:00.000+0000"
	fields["status"].(map[string]any)["id"] = "10"
	f.tr.issues = []map[string]any{issue}
	f.api = &cloudWorkflowAPI{store: f.store, tracker: f.tr}
	f.profiles = jiracloud.Profiles{Store: f.store, API: f.api}
	p, err := f.profiles.Put(t.Context(), jiracloud.Input{Name: "jira-native", MCPConnection: "jira-mcp", MCPRevision: 1, CloudID: workflowCloudID, SiteURL: "https://api.atlassian.com/ex/jira/" + workflowCloudID, ProjectID: "10001", Enabled: true, ProjectRestricted: true}, 0, "admin")
	if err != nil {
		t.Fatal(err)
	}
	f.tasks = &persistedCloudTasks{store: f.store}
	f.in = input()
	f.in.ReadinessDisabled = true
	f.in.Source = trigger.SourceConfig{Kind: "jira", ConnectionID: p.MCPConnection, CloudID: p.CloudID, SiteURL: p.SiteURL, ProjectID: p.ProjectID, ProfileID: p.Name, ProfileRevision: p.Revision}
	f.wire()
	return f
}
func (f *cloudLifecycleRig) wire() {
	f.r = runner(f.store, f.c, "cloud", f.tr)
	f.r.Triggers.CloudProfiles = &f.profiles
	f.r.Triggers.TrackerRevision = f.api.Revision
	reg := tools.NewRegistry()
	reg.MustRegister(f.tr.tools("searchJiraIssuesUsingJql")...)
	reg.MustRegister(trigger.TrackerToolsWithCloud(f.api.Call, jiradc.Connections{}, f.profiles)...)
	f.r.Gateway = tools.NewGateway(reg, nil, f.r.Triggers.Bus)
	f.r.Tasks = f.tasks
	f.r.BaseURL = "https://weft.example"
}
func (f *cloudLifecycleRig) mapping(t *testing.T, name, from, tr, target string) trigger.MappingPin {
	t.Helper()
	p, err := (jiradc.WorkflowMappings{Backend: f.profiles.Backend()}).Put(t.Context(), jiradc.MappingInput{Name: name, Connection: f.in.Source.ProfileID, ConnectionRevision: f.in.Source.ProfileRevision, SampleIssueKey: "APP-7", SampleIssueID: "10007", IssueTypeID: "10002", FromStatusID: from, TransitionID: tr, TargetStatusID: target, IssueUpdated: "2026-11-20T09:00:00.000+0000", Enabled: true}, 0, "admin")
	if err != nil {
		t.Fatal(err)
	}
	return trigger.MappingPin{Name: p.Name, Revision: p.Revision}
}
func (f *cloudLifecycleRig) status(id string) {
	f.tr.issues[0]["fields"].(map[string]any)["status"].(map[string]any)["id"] = id
}
func (f *cloudLifecycleRig) runtime(t *testing.T) {
	t.Helper()
	at := f.c.t
	if err := f.store.PutExecution(t.Context(), &domain.Execution{ID: "exe-cloud", GraphID: "task-cloud", NodeID: "planner", Status: domain.ExecRunning, StartedAt: at, RuntimeStartedAt: &at}); err != nil {
		t.Fatal(err)
	}
}
func (f *cloudLifecycleRig) delivered(t *testing.T, task string) {
	t.Helper()
	if err := f.store.AppendEvent(t.Context(), domain.NewEvent(domain.EvPullRequestOpened, task, map[string]any{"url": "https://bitbucket.example/pr/2", "number": 2})); err != nil {
		t.Fatal(err)
	}
}
func (f *cloudLifecycleRig) reopen(t *testing.T) {
	t.Helper()
	if err := f.store.Close(); err != nil {
		t.Fatal(err)
	}
	st, err := fsstore.Open(f.root)
	if err != nil {
		t.Fatal(err)
	}
	f.store = st
	f.api.store = st
	f.profiles.Store = st
	f.tasks.store = st
	f.wire()
}

func TestCloudLifecycleUsesPersistedRuntimeAndPREvidenceThroughPrivateGateway(t *testing.T) {
	f := cloudRig(t)
	work := f.mapping(t, "working", "10", "32", "15")
	f.status("15")
	review := f.mapping(t, "reviewing", "15", "31", "20")
	f.status("10")
	f.in.LifecycleMappings = trigger.LifecycleMappings{WorkStarted: work, PRDelivered: review}
	tr, err := f.r.Triggers.Create(t.Context(), f.in, "admin")
	if err != nil {
		t.Fatal(err)
	}
	tr = advance(f.r, f.c, t, tr.ID)
	task := tr.CurrentTask
	if task == "" || tr.CurrentIssue.ID != "10007" || tr.CurrentIssue.IssueTypeID != "10002" || f.api.posts != 0 {
		t.Fatal(tr, f.api.posts)
	}
	tr = advance(f.r, f.c, t, tr.ID)
	if f.api.posts != 0 {
		t.Fatal("waiting task transitioned")
	}
	f.runtime(t)
	tr = advance(f.r, f.c, t, tr.ID)
	rows, err := f.r.Triggers.ListLifecycleSyncs(t.Context())
	if err != nil || len(rows) != 1 || rows[0].Evidence != "exe-cloud" || rows[0].State != "target_observed" || f.api.posts != 1 {
		t.Fatal(rows, err, f.api.posts)
	}
	f.delivered(t, task)
	tr = advance(f.r, f.c, t, tr.ID)
	rows, err = f.r.Triggers.ListLifecycleSyncs(t.Context())
	if err != nil || len(rows) != 2 || tr.CurrentTask != "" || f.api.posts != 2 || !slices.Contains(f.tr.issues[0]["fields"].(map[string]any)["labels"].([]string), trigger.LabelReview) {
		t.Fatal(tr, rows, err, f.api.posts)
	}
	for _, row := range rows {
		core, err := f.r.Triggers.LifecycleTransition(t.Context(), row)
		if err != nil || core.Intent.Connection != "jira-native" || core.SiteURL != f.in.Source.SiteURL {
			t.Fatal(core, err)
		}
	}
	f.reopen(t)
	advance(f.r, f.c, t, tr.ID)
	if f.api.posts != 2 || len(f.tasks.started) != 1 {
		t.Fatal("restart duplicated work")
	}
	reg := tools.NewRegistry()
	reg.MustRegister(trigger.TrackerToolsWithCloud(f.api.Call, jiradc.Connections{}, f.profiles)...)
	tool, _ := reg.Get("tracker.transition")
	res, err := tool.Invoke(t.Context(), &tools.Invocation{Principal: trigger.Principal(tr), Args: []byte(`{"syncId":"` + rows[0].ID + `"}`)})
	if !errors.Is(err, tools.ErrDenied) || res != nil {
		t.Fatal("public grant forged lifecycle authority", res, err)
	}
	for _, args := range f.tr.args {
		if q, _ := args["jql"].(string); q != "" && (!strings.Contains(q, "project = 10001") || strings.Contains(q, "To Do")) {
			t.Fatal("unscoped/named-status opt-in", q)
		}
	}
}
func TestCloudLifecycleFailureAndUnknownNeverEraseDeliveryOrReplayAfterRestart(t *testing.T) {
	for _, mode := range []string{"disabled", "denied", "lost"} {
		t.Run(mode, func(t *testing.T) {
			f := cloudRig(t)
			f.in.LifecycleMappings.PRDelivered = f.mapping(t, "reviewing", "10", "31", "20")
			tr, err := f.r.Triggers.Create(t.Context(), f.in, "admin")
			if err != nil {
				t.Fatal(err)
			}
			tr = advance(f.r, f.c, t, tr.ID)
			task := tr.CurrentTask
			switch mode {
			case "disabled":
				v, _ := (jiradc.WorkflowMappings{Backend: f.profiles.Backend()}).Get(t.Context(), "reviewing")
				v.Enabled = false
				if _, err = (jiradc.WorkflowMappings{Backend: f.profiles.Backend()}).Put(t.Context(), v.MappingInput, v.Revision, "admin"); err != nil {
					t.Fatal(err)
				}
			case "denied":
				f.api.denied = true
			case "lost":
				f.api.lost = true
			}
			f.delivered(t, task)
			tr = advance(f.r, f.c, t, tr.ID)
			rows, err := f.r.Triggers.ListLifecycleSyncs(t.Context())
			want := "failed"
			if mode == "lost" {
				want = "unknown"
			}
			if err != nil || len(rows) != 1 || rows[0].State != want || tr.CurrentTask != "" || len(f.tasks.started) != 1 {
				t.Fatal(rows, tr, err)
			}
			before := f.api.posts
			f.reopen(t)
			advance(f.r, f.c, t, tr.ID)
			if f.api.posts != before {
				t.Fatal("failed/unknown POST replayed")
			}
			if mode == "denied" {
				f.api.denied = false
				queued, err := f.r.Triggers.RetryLifecycleSync(t.Context(), rows[0].ID, rows[0].Revision, "admin")
				if err != nil || queued.State != "queued" || f.api.posts != before {
					t.Fatal("explicit preflight retry failed", queued, err)
				}
				advance(f.r, f.c, t, tr.ID)
				got, err := f.r.Triggers.GetLifecycleSync(t.Context(), rows[0].ID)
				if err != nil || got.State != "target_observed" || f.api.posts != before+1 || len(f.tasks.started) != 1 {
					t.Fatal("preflight recovery duplicated work or did not sync", got, err)
				}
			}
			if mode == "lost" {
				got, err := f.r.Triggers.ReconcileLifecycleSync(t.Context(), rows[0].ID, rows[0].Revision, "operator")
				if err != nil || got.State != "target_observed" || f.api.posts != before {
					t.Fatal(got, err)
				}
			}
		})
	}
}
func TestCloudLifecycleProfileScopePinsAndEditionCollisionFailClosed(t *testing.T) {
	f := cloudRig(t)
	f.in.LifecycleMappings.PRDelivered = f.mapping(t, "reviewing", "10", "31", "20")
	for _, edit := range []func(*trigger.Input){func(v *trigger.Input) { v.Source.ProfileRevision++ }, func(v *trigger.Input) { v.Source.CloudID = "22222222-2222-2222-2222-222222222222" }, func(v *trigger.Input) { v.Source.ProjectID = "10099" }, func(v *trigger.Input) { v.Source.ConnectionID = "another" }, func(v *trigger.Input) {
		v.Source.Kind = "jira-dc"
		v.Source.CloudID = ""
		v.Source.ProfileID = ""
		v.Source.ProfileRevision = 0
		v.Source.SiteURL = "https://jira.example"
	}} {
		bad := f.in
		edit(&bad)
		if _, err := f.r.Triggers.Create(t.Context(), bad, "admin"); err == nil {
			t.Fatal("foreign scope accepted", bad)
		}
	}
	if f.api.posts != 0 {
		t.Fatal("scope validation wrote remote state")
	}
	// A Cloud mapping with the same local name cannot be resolved from the DC namespace.
	if _, err := (jiradc.WorkflowMappings{Connections: jiradc.Connections{Store: f.store}}).Get(t.Context(), "reviewing"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatal(err)
	}
}

func TestCloudWorkflowConsentDetectsProfileAndMCPRevisionChanges(t *testing.T) {
	for _, kind := range []string{"profile", "mcp"} {
		t.Run(kind, func(t *testing.T) {
			f := cloudRig(t)
			f.in.LifecycleMappings.PRDelivered = f.mapping(t, "reviewing", "10", "31", "20")
			tr, err := f.r.Triggers.Create(t.Context(), f.in, "admin")
			if err != nil {
				t.Fatal(err)
			}
			if kind == "profile" {
				p, err := f.profiles.Get(t.Context(), f.in.Source.ProfileID)
				if err != nil {
					t.Fatal(err)
				}
				p.ProjectRestricted = false
				if _, err = f.profiles.Put(t.Context(), p.Input, p.Revision, "admin"); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := f.store.PutSetting(t.Context(), "mcp/connections/jira-mcp", []byte(`{"rotated":true}`), 1, "admin"); err != nil {
					t.Fatal(err)
				}
			}
			problem, err := f.r.Triggers.ConsentProblem(t.Context(), tr)
			if err != nil || problem == "" {
				t.Fatal("changed authority kept consent", problem, err)
			}
			advance(f.r, f.c, t, tr.ID)
			if len(f.tasks.started) != 0 || f.api.posts != 0 {
				t.Fatal("changed authority dispatched work")
			}
		})
	}
}

func TestCloudWorkflowAdmissionRequiresNumericIdentityInReviewedProject(t *testing.T) {
	for _, kind := range []string{"id", "type", "status", "project"} {
		t.Run(kind, func(t *testing.T) {
			f := cloudRig(t)
			f.in.LifecycleMappings.PRDelivered = f.mapping(t, "reviewing", "10", "31", "20")
			tr, err := f.r.Triggers.Create(t.Context(), f.in, "admin")
			if err != nil {
				t.Fatal(err)
			}
			row := f.tr.issues[0]
			fields := row["fields"].(map[string]any)
			switch kind {
			case "id":
				row["id"] = ""
			case "type":
				fields["issuetype"] = map[string]any{}
			case "status":
				fields["status"].(map[string]any)["id"] = "Review"
			case "project":
				fields["project"] = map[string]any{"id": "10099"}
			}
			tr = advance(f.r, f.c, t, tr.ID)
			if tr.CurrentTask != "" || len(f.tasks.started) != 0 || f.api.posts != 0 || !slices.Contains(fields["labels"].([]string), "weft") {
				t.Fatal("invalid issue claimed", tr, kind)
			}
		})
	}
}
