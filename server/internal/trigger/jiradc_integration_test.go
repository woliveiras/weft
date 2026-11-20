package trigger_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/jiradc"
	"github.com/woliveiras/weft/server/internal/secrets"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/tools"
	"github.com/woliveiras/weft/server/internal/trigger"
)

type nativeTrackerFixture struct {
	site                  string
	labels                []string
	comments              []jiradc.Comment
	requests, puts, posts int
	status                int
	foreign               bool
	assignee              *jiradc.Actor
	blocker               string
	historyOverflow       bool
	historyScans          int
	now                   func() time.Time
}

func (f *nativeTrackerFixture) issue() jiradc.Issue {
	var i jiradc.Issue
	i.ID, i.Key = "10007", "APP-7"
	i.Fields.Project.ID, i.Fields.IssueType.ID = "10001", "10002"
	if f.foreign {
		i.Fields.Project.ID = "10099"
	}
	i.Fields.Summary, i.Fields.Description = "Review the parser", "Only change parser; tests must pass."
	i.Fields.Status.ID, i.Fields.Status.Name, i.Fields.Status.StatusCategory.Key = "10", "Review", "new"
	i.Fields.Labels, i.Fields.Assignee = slices.Clone(f.labels), f.assignee
	i.Fields.IssueLinks = []jiradc.IssueLink{}
	if f.blocker != "" {
		var link jiradc.IssueLink
		raw := fmt.Sprintf(`{"type":{"inward":"is blocked by"},"inwardIssue":{"fields":{"status":{"statusCategory":{"key":%q}}}}}`, f.blocker)
		_ = json.Unmarshal([]byte(raw), &link)
		i.Fields.IssueLinks = append(i.Fields.IssueLinks, link)
	}
	i.Fields.Reporter = &jiradc.Actor{Key: "human-key", Name: "author", Active: true}
	i.Fields.Created, i.Fields.Updated = "2026-11-20T09:00:00.000+0000", "2026-11-20T09:00:00.000+0000"
	return i
}
func (f *nativeTrackerFixture) serve(t *testing.T, w http.ResponseWriter, r *http.Request) {
	f.requests++
	if r.Header.Get("Authorization") != "Bearer dc-native-fixture-token" {
		t.Error("incorrect credential audience")
	}
	if f.status != 0 {
		w.WriteHeader(f.status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	path := strings.Replace(r.URL.Path, "/second-context/", "/context/", 1)
	switch path {
	case "/context/rest/api/2/search":
		query := r.URL.Query().Get("jql")
		if !strings.HasPrefix(query, "project = 10001 AND (") || r.URL.Query().Get("startAt") != "0" || strings.Contains(query, "Review") || strings.Contains(query, "To Do") {
			t.Error("unscoped/named-status search", query)
		}
		rows := []jiradc.Issue{}
		if slices.Contains(f.labels, "weft") || strings.Contains(query, `key = "APP-7"`) {
			rows = append(rows, f.issue())
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"startAt": 0, "maxResults": 50, "total": len(rows), "issues": rows})
	case "/context/rest/api/2/issue/APP-7":
		_ = json.NewEncoder(w).Encode(f.issue())
	case "/context/rest/api/2/issue/10007":
		if r.Method != "PUT" {
			t.Error("unexpected issue operation")
		}
		f.puts++
		var body struct {
			Fields json.RawMessage `json:"fields"`
			Update struct {
				Labels []map[string]string `json:"labels"`
			} `json:"update"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Fields != nil {
			t.Error("assignment was changed", err)
		}
		for _, op := range body.Update.Labels {
			if value := op["remove"]; value != "" {
				f.labels = slices.DeleteFunc(f.labels, func(label string) bool { return label == value })
			}
			if value := op["add"]; value != "" && !slices.Contains(f.labels, value) {
				f.labels = append(f.labels, value)
			}
		}
		w.WriteHeader(204)
	case "/context/rest/api/2/issue/10007/comment":
		if r.Method == "POST" {
			f.posts++
			var body struct {
				Body string `json:"body"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			row := jiradc.Comment{ID: strconv.Itoa(10000 + f.posts), Body: body.Body, Author: jiradc.Actor{Key: "bot-key", Name: "weft-bot", Active: true}, Created: f.now().Format(time.RFC3339Nano)}
			f.comments = append(f.comments, row)
			w.WriteHeader(201)
			_ = json.NewEncoder(w).Encode(row)
		} else {
			f.historyScans++
			if f.historyOverflow {
				start, err := strconv.Atoi(r.URL.Query().Get("startAt"))
				if err != nil {
					t.Error(err)
				}
				rows := make([]jiradc.Comment, 100)
				for n := range rows {
					rows[n] = jiradc.Comment{ID: strconv.Itoa(20000 + start + n), Author: jiradc.Actor{Key: "human-key", Name: "author", Active: true}, Body: "Clarification", Created: f.now().Format(time.RFC3339Nano)}
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"startAt": start, "maxResults": 100, "total": 1001, "comments": rows})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"startAt": 0, "maxResults": 100, "total": len(f.comments), "comments": f.comments})
		}
	case "/context/rest/api/2/myself":
		_ = json.NewEncoder(w).Encode(jiradc.Actor{Key: "bot-key", Name: "weft-bot", Active: true})
	case "/context/rest/api/2/project/10001":
		fmt.Fprint(w, `{"id":"10001"}`)
	default:
		t.Error("unexpected native route", r.Method, r.URL.Path)
		w.WriteHeader(404)
	}
}
func nativeRunner(t *testing.T) (*trigger.Runner, *clock, *tasks, *nativeTrackerFixture, jiradc.Connections, trigger.Input) {
	t.Helper()
	st := triggerStore(t)
	c := &clock{t: time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)}
	f := &nativeTrackerFixture{labels: []string{"weft", "bug"}, comments: []jiradc.Comment{}, now: c.now}
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { f.serve(t, w, r) }))
	t.Cleanup(s.Close)
	f.site = s.URL + "/context"
	// Trust only this fixture's CA, still exercising HTTPS certificate validation.
	old := http.DefaultTransport
	http.DefaultTransport = s.Client().Transport
	t.Cleanup(func() { http.DefaultTransport = old })
	box, err := secrets.NewBox(bytes.Repeat([]byte{6}, 32))
	if err != nil {
		t.Fatal(err)
	}
	m := jiradc.Connections{Store: st, Box: box}
	token := "dc-native-fixture-token"
	if _, err = m.Put(t.Context(), jiradc.ConnectionInput{Name: "jira-native", Kind: "jira-dc", SiteURL: f.site, ProjectID: "10001", Enabled: true, ProjectRestricted: true, Token: &token}, 0, "admin"); err != nil {
		t.Fatal(err)
	}
	r := runner(st, c, "native")
	r.Triggers.NativeConnections = &m
	reg := tools.NewRegistry()
	reg.MustRegister(trigger.TrackerTools(nil, m)...)
	r.Gateway = tools.NewGateway(reg, nil, r.Triggers.Bus)
	fake := &tasks{status: map[string]trigger.TaskStatus{}}
	r.Tasks, r.BaseURL = fake, "https://weft.example"
	in := input()
	in.Source = trigger.SourceConfig{Kind: "jira-dc", ConnectionID: "jira-native", SiteURL: f.site, ProjectID: "10001"}
	return r, c, fake, f, m, in
}

func TestNativeJiraTriggerClaimsCompletesAndAuditsThroughGateway(t *testing.T) {
	r, c, fake, f, _, in := nativeRunner(t)
	f.assignee = &jiradc.Actor{Key: "bot-key", Name: in.Owner, Active: true}
	tr, err := r.Triggers.Create(t.Context(), in, "admin")
	if err != nil {
		t.Fatal(err)
	}
	preview, _, _, err := r.Match(t.Context(), tr)
	if err != nil || preview.Next == nil || preview.Next.Key != "APP-7" || f.puts+f.posts != 0 {
		t.Fatal(preview, err)
	}
	tr = advance(r, c, t, tr.ID)
	if len(fake.started) != 1 || tr.CurrentTask == "" || tr.LastError != "" || !slices.Equal(f.labels, []string{"bug", trigger.LabelInProgress}) || f.assignee.Name != in.Owner || f.posts != 1 {
		t.Fatalf("claim %+v %+v labels=%v posts=%d", tr, fake.started, f.labels, f.posts)
	}
	key := trigger.DedupKey(tr, trigger.Issue{Source: "jira-dc", Key: "APP-7"})
	if tr.Processed[key] != tr.CurrentTask {
		t.Fatal("missing durable admission")
	}
	fake.status[tr.CurrentTask] = trigger.TaskStatus{State: "succeeded", PullRequestURL: "https://bitbucket.example/projects/TEAM/repos/app/pull-requests/2"}
	tr = advance(r, c, t, tr.ID)
	if tr.CurrentTask != "" || tr.LastError != "" || !slices.Contains(f.labels, trigger.LabelReview) || f.posts != 2 || !strings.Contains(f.comments[1].Body, "bitbucket.example") {
		t.Fatal("completion", tr.LastError, f.labels, f.comments)
	}
	advance(r, c, t, tr.ID)
	if len(fake.started) != 1 {
		t.Fatal("duplicate admission")
	}
	events, err := r.Store.ListEvents(t.Context(), storage.EventFilter{GraphID: "trigger-" + tr.ID, Types: []domain.EventType{domain.EvToolCompleted}})
	if err != nil || len(events) < 8 {
		t.Fatal("missing gateway audit", len(events), err)
	}
	for _, ev := range events {
		raw, _ := json.Marshal(ev)
		if strings.Contains(string(raw), "dc-native-fixture-token") {
			t.Fatal("credential in event")
		}
	}
}

func TestNativeTriggerScopeRevocationAndForeignIssuesPreventAdmission(t *testing.T) {
	for _, mode := range []string{"foreign", "403", "stale", "wrong-project", "unconfirmed"} {
		t.Run(mode, func(t *testing.T) {
			r, c, fake, f, m, in := nativeRunner(t)
			if mode == "wrong-project" {
				in.Source.ProjectID = "10099"
			}
			if mode == "unconfirmed" {
				current, _ := m.Get(t.Context(), in.Source.ConnectionID)
				_, err := m.Put(t.Context(), jiradc.ConnectionInput{Name: current.Name, Kind: current.Kind, SiteURL: current.SiteURL, ProjectID: current.ProjectID, Enabled: true}, current.Revision, "admin")
				if err != nil {
					t.Fatal(err)
				}
			}
			tr, err := r.Triggers.Create(t.Context(), in, "admin")
			if mode == "wrong-project" || mode == "unconfirmed" {
				if err == nil || f.requests != 0 {
					t.Fatal("invalid connection granted", err, f.requests)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "foreign":
				f.foreign = true
			case "403":
				f.status = 403
			case "stale":
				current, _ := m.Get(t.Context(), in.Source.ConnectionID)
				_, err = m.Put(t.Context(), jiradc.ConnectionInput{Name: current.Name, Kind: current.Kind, SiteURL: current.SiteURL, ProjectID: current.ProjectID, Enabled: false, ProjectRestricted: true}, current.Revision, "admin")
				if err != nil {
					t.Fatal(err)
				}
			}
			tr = advance(r, c, t, tr.ID)
			if len(fake.started) != 0 || f.puts+f.posts != 0 {
				t.Fatal("unsafe dispatch", tr)
			}
			if mode == "stale" && (tr.State != trigger.Paused || f.requests != 0) {
				t.Fatal("stale consent contacted Jira", tr.State, f.requests)
			}
		})
	}
}

func TestNativeReadinessRepliesAndUnverifiedRelabelRequireOperator(t *testing.T) {
	r, c, fake, f, _, in := nativeRunner(t)
	evaluations := 0
	r.Assessor = assessFunc(func(_ context.Context, _ *trigger.Trigger, text string) trigger.ReadinessAssessment {
		evaluations++
		return trigger.ReadinessAssessment{Missing: []string{"acceptance"}}
	})
	tr, err := r.Triggers.Create(t.Context(), in, "admin")
	if err != nil {
		t.Fatal(err)
	}
	tr = advance(r, c, t, tr.ID)
	if len(tr.Holds) != 1 || tr.Holds[0].Publication != "confirmed" || evaluations != 1 {
		t.Fatal("readiness publication", tr.Holds, tr.LastError)
	}
	f.comments = append(f.comments, jiradc.Comment{ID: "20001", Author: jiradc.Actor{Key: "human-key", Name: "author", Active: true}, Body: "Parser accepts both forms; add regression tests.", Created: c.t.Add(time.Second).Format(time.RFC3339Nano)})
	tr = advance(r, c, t, tr.ID)
	if evaluations != 2 || len(fake.started) != 0 {
		t.Fatal("author reply not evaluated", evaluations, tr.LastError)
	}
	// A relabel is never evidence that its actor is a project administrator.
	f.labels = append(f.labels, trigger.LabelTodo)
	tr = advance(r, c, t, tr.ID)
	if len(tr.Holds) != 1 || tr.Holds[0].State != "operator" || tr.Holds[0].Reason != "manual_requeue_unverified" || evaluations != 2 || len(fake.started) != 0 {
		t.Fatal("unverified requeue admitted", tr.Holds, evaluations, tr.LastError)
	}
}

func TestNativeAdmissionHistorySeparatesSitesAndSurvivesConnectionRotation(t *testing.T) {
	r, c, fake, f, m, in := nativeRunner(t)
	tr, err := r.Triggers.Create(t.Context(), in, "admin")
	if err != nil {
		t.Fatal(err)
	}
	tr = advance(r, c, t, tr.ID)
	first := trigger.DedupKey(tr, trigger.Issue{Source: "jira-dc", Key: "APP-7"})
	fake.status[tr.CurrentTask] = trigger.TaskStatus{State: "succeeded", PullRequestURL: "https://bitbucket.example/pr/2"}
	tr = advance(r, c, t, tr.ID)
	secondSite := strings.TrimSuffix(f.site, "/context") + "/second-context"
	token := "dc-native-fixture-token"
	if _, err = m.Put(t.Context(), jiradc.ConnectionInput{Name: "second-site", Kind: "jira-dc", SiteURL: secondSite, ProjectID: "10001", Enabled: true, ProjectRestricted: true, Token: &token}, 0, "admin"); err != nil {
		t.Fatal(err)
	}
	in.Source.ConnectionID, in.Source.SiteURL = "second-site", secondSite
	tr, err = r.Triggers.Edit(t.Context(), tr.ID, in, "admin")
	if err != nil {
		t.Fatal(err)
	}
	f.labels = []string{"weft", "bug"}
	tr = advance(r, c, t, tr.ID)
	second := trigger.DedupKey(tr, trigger.Issue{Source: "jira-dc", Key: "APP-7"})
	if len(fake.started) != 2 || first == second || tr.Processed[first] == "" || tr.Processed[second] == "" {
		t.Fatal("site-scoped admission collided", fake.started, tr.Processed, tr.LastError)
	}
	tr = advance(r, c, t, tr.ID) // Complete second task before rotating its credential.
	current, err := m.Get(t.Context(), "second-site")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Put(t.Context(), jiradc.ConnectionInput{Name: current.Name, Kind: current.Kind, SiteURL: current.SiteURL, ProjectID: current.ProjectID, Enabled: true, ProjectRestricted: true, Token: &token}, current.Revision, "admin"); err != nil {
		t.Fatal(err)
	}
	tr, err = r.Triggers.Resume(t.Context(), tr.ID, "admin")
	if err != nil {
		t.Fatal(err)
	}
	f.labels = []string{"weft", "bug"}
	tr = advance(r, c, t, tr.ID)
	if len(fake.started) != 2 || trigger.DedupKey(tr, trigger.Issue{Source: "jira-dc", Key: "APP-7"}) != second {
		t.Fatal("credential rotation reset history", fake.started, tr.LastError)
	}
}

func TestNativeGatewayGrantsCannotForgePrivateTriggerContext(t *testing.T) {
	r, _, _, f, _, in := nativeRunner(t)
	tr, err := r.Triggers.Create(t.Context(), in, "admin")
	if err != nil {
		t.Fatal(err)
	}
	call := trigger.GatewayInvoker(r.Gateway, trigger.Principal(tr))
	for _, name := range tr.Consent.Operations {
		res, err := call(t.Context(), name, map[string]any{"key": "APP-7", "add": "weft:wip"})
		if err == nil && (res == nil || !res.IsError) {
			t.Fatal("forged scope allowed", name)
		}
	}
	if f.requests != 0 {
		t.Fatal("unscoped grant contacted native tracker", f.requests)
	}
}

func TestNativeCommentHistoryBoundCreatesOperatorHoldWithoutRepeatedScan(t *testing.T) {
	r, c, fake, f, _, in := nativeRunner(t)
	r.Assessor = assessFunc(func(context.Context, *trigger.Trigger, string) trigger.ReadinessAssessment {
		return trigger.ReadinessAssessment{Missing: []string{"acceptance"}}
	})
	tr, err := r.Triggers.Create(t.Context(), in, "admin")
	if err != nil {
		t.Fatal(err)
	}
	tr = advance(r, c, t, tr.ID)
	if len(tr.Holds) != 1 || tr.Holds[0].Publication != "confirmed" {
		t.Fatal(tr.Holds, tr.LastError)
	}
	f.historyOverflow = true
	before := f.historyScans
	tr = advance(r, c, t, tr.ID)
	if len(tr.Holds) != 1 || tr.Holds[0].State != "operator" || tr.Holds[0].Reason != "history_limit" || f.historyScans-before != 10 || len(fake.started) != 0 {
		t.Fatal("unbounded history did not stop", tr.Holds, f.historyScans-before, tr.LastError)
	}
	advance(r, c, t, tr.ID)
	if f.historyScans-before != 10 {
		t.Fatal("operator hold rescanned full history", f.historyScans-before)
	}
}

func TestNativeBlockingLinksRespectProviderStatusCategory(t *testing.T) {
	for _, category := range []string{"new", "indeterminate", "unknown", "done"} {
		t.Run(category, func(t *testing.T) {
			r, c, fake, f, _, in := nativeRunner(t)
			f.blocker = category
			tr, err := r.Triggers.Create(t.Context(), in, "admin")
			if err != nil {
				t.Fatal(err)
			}
			tr = advance(r, c, t, tr.ID)
			want := 0
			if category == "done" {
				want = 1
			}
			if len(fake.started) != want {
				t.Fatal("wrong blocker eligibility", category, fake.started, tr.LastError)
			}
			if want == 0 && f.puts+f.posts != 0 {
				t.Fatal("blocked issue was mutated", f.puts, f.posts)
			}
		})
	}
}
