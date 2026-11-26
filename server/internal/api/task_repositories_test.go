package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/projectconfig"
	"github.com/woliveiras/weft/server/internal/scm"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/storage/stores"
	"github.com/woliveiras/weft/server/internal/taskauth"
	"github.com/woliveiras/weft/server/internal/taskconfig"
	"github.com/woliveiras/weft/server/internal/taskrun"
	"github.com/woliveiras/weft/server/internal/tools"
)

func memberGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-C", dir}, args...)...)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=/nonexistent", "GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_CONFIG_NOSYSTEM=1"}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatal(args, err, string(out))
	}
	return strings.TrimSpace(string(out))
}
func memberInput(t *testing.T, srv *httptest.Server, alias string, files map[string]string) (domain.TaskRepository, string) {
	t.Helper()
	root := t.TempDir()
	source := filepath.Join(root, "source")
	remote := filepath.Join(root, alias+".git")
	memberGit(t, root, "init", "-q", "--bare", remote)
	memberGit(t, root, "init", "-q", "-b", "main", source)
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(source, name), []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
	}
	memberGit(t, source, "add", ".")
	memberGit(t, source, "commit", "-qm", "base")
	memberGit(t, source, "remote", "add", "origin", "file://"+remote)
	memberGit(t, source, "push", "-q", "origin", "main")
	input, _ := json.Marshal(projectconfig.Input{Name: alias, Path: source})
	var p projectconfig.Project
	if c := call(t, srv, "POST", "/api/v1/projects", adminTok, "application/json", string(input), &p); c != 201 {
		t.Fatal(c)
	}
	return domain.TaskRepository{Alias: alias, ProjectID: p.ID, BaseCommit: p.Head}, remote
}

func TestRepositoryDraftThroughAPI(t *testing.T) {
	srv, _ := setupApp(t, nil)
	var in taskconfig.Input
	if err := json.Unmarshal([]byte(taskBody), &in); err != nil {
		t.Fatal(err)
	}
	for _, alias := range []string{"infra", "backend", "frontend"} {
		r, _ := memberInput(t, srv, alias, map[string]string{"version": alias})
		in.Repositories = append(in.Repositories, r)
	}
	var graph domain.Graph
	if err := json.Unmarshal([]byte(taskGraph), &graph); err != nil {
		t.Fatal(err)
	}
	graph.Spec.Task = &domain.TaskDefaults{ProjectIDs: domain.RepositoryProjectIDs(in.Repositories)}
	b, _ := json.Marshal(graph)
	if c := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/json", string(b), nil); c != 201 {
		t.Fatal(c)
	}
	b, _ = json.Marshal(in)
	var saved taskconfig.Draft
	if c := call(t, srv, "POST", "/api/v1/tasks", operatorTok, "application/json", string(b), &saved); c != 201 {
		t.Fatal(c)
	}
	path := "/api/v1/tasks/" + saved.ID
	put := func(body []byte, rev int64) int {
		req, _ := http.NewRequest("PUT", srv.URL+path, strings.NewReader(string(body)))
		req.Header.Set("Authorization", "Bearer "+operatorTok)
		req.Header.Set("If-Match", strconv.FormatInt(rev, 10))
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		return res.StatusCode
	}
	if c := put([]byte(taskBody), saved.Revision); c/100 == 2 {
		t.Fatal("legacy update dropped repositories")
	}
	var v taskrun.Review
	if c := call(t, srv, "GET", path+"/review", viewerTok, "", "", &v); c != 200 || v.CanStart || len(v.Task.Repositories) != 3 || !strings.Contains(strings.Join(v.Blockers, " "), "Docker") {
		t.Fatal(c, v.Blockers)
	}
	var old taskauth.Review
	if c := call(t, srv, "GET", path+"/authorization", viewerTok, "", "", &old); c != 200 {
		t.Fatal(c)
	}
	in.Repositories[0], in.Repositories[2] = in.Repositories[2], in.Repositories[0]
	b, _ = json.Marshal(in)
	if c := put(b, saved.Revision); c/100 != 2 {
		t.Fatal(c)
	}
	var reopened taskconfig.Draft
	if c := call(t, srv, "GET", path, viewerTok, "", "", &reopened); c != 200 || reopened.Repositories[0].Alias != "frontend" {
		t.Fatal(c, reopened)
	}
	var fresh taskauth.Review
	if c := call(t, srv, "GET", path+"/authorization", viewerTok, "", "", &fresh); c != 200 || old.Scope == fresh.Scope {
		t.Fatal("old consent survived edit")
	}
	if code := taskStart(t, srv.URL, saved.ID, adminTok, saved.Revision); code/100 == 2 {
		t.Fatal("stale start")
	}
	in.Repositories[0].BaseCommit = strings.Repeat("1", 40)
	b, _ = json.Marshal(in)
	if c := put(b, reopened.Revision); c/100 != 2 {
		t.Fatal(c)
	}
	var invalid taskrun.Review
	if c := call(t, srv, "GET", path+"/review", viewerTok, "", "", &invalid); c != 200 || invalid.CanStart || !strings.Contains(strings.Join(invalid.Blockers, " "), "frontend:") || len(invalid.Task.Repositories) != 3 {
		t.Fatal("missing commit must identify its member", c, invalid.Blockers)
	}
	reopened = *invalid.Task
	in.Repositories = []domain.TaskRepository{}
	b, _ = json.Marshal(in)
	if c := put(b, reopened.Revision); c/100 != 2 {
		t.Fatal(c)
	}
	var empty taskconfig.Draft
	if c := call(t, srv, "GET", path, viewerTok, "", "", &empty); c != 200 || empty.Repositories == nil || len(empty.Repositories) != 0 {
		t.Fatal("explicit empty selection lost", c)
	}
	if c := put([]byte(taskBody), empty.Revision); c/100 != 2 {
		t.Fatal(c)
	}
	var legacyUpdate taskconfig.Draft
	if c := call(t, srv, "GET", path, viewerTok, "", "", &legacyUpdate); c != 200 || legacyUpdate.Repositories == nil || len(legacyUpdate.Repositories) != 0 {
		t.Fatal("lost explicit empty selection after legacy edit")
	}
}

type memberModel struct {
	lostReply     bool
	inspectMounts func(context.Context) error
	mu            sync.Mutex
	calls         []llm.ToolCall
	step          int
}

func (*memberModel) Name() string                                    { return "member-model" }
func (*memberModel) Models(context.Context) ([]llm.ModelInfo, error) { return nil, nil }
func (m *memberModel) Chat(ctx context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.step >= len(m.calls) {
		return nil, errors.New("unexpected extra model turn")
	}
	if last := req.Messages[len(req.Messages)-1]; last.Role == llm.RoleTool && !m.lostReply && (strings.HasPrefix(last.Content, "error:") || strings.Contains(last.Content, `"isError":true`)) {
		return nil, fmt.Errorf("fixture tool %s: %.350s", last.ToolCallID, last.Content)
	}
	c := m.calls[m.step]
	if c.Name == "review.request" {
		if err := m.inspectMounts(ctx); err != nil {
			return nil, err
		}
	}
	m.step++
	return &llm.ChatResponse{Message: llm.ChatMessage{ToolCalls: []llm.ToolCall{c}}, StopReason: "tool_calls"}, nil
}
func (m *memberModel) add(tool string, args any) {
	b, _ := json.Marshal(args)
	m.calls = append(m.calls, llm.ToolCall{ID: fmt.Sprintf("step-%d", len(m.calls)+1), Name: tool, Arguments: b})
}

// A local Docker image with Git and BusyBox is supplied by the qualification
// runner. Nothing in this scenario contacts a live model or SCM account.
func TestMultiRepositoryDockerTask(t *testing.T) {
	image := os.Getenv("WEFT_TEST_TASK_DOCKER_IMAGE")
	if image == "" {
		t.Skip("set WEFT_TEST_TASK_DOCKER_IMAGE to a local Git + BusyBox image")
	}
	for _, mode := range []string{"complete", "recover", "cancel", "unchanged", "local"} {
		t.Run(mode, func(t *testing.T) { runRepositoryDockerTask(t, image, mode) })
	}
}
func runRepositoryDockerTask(t *testing.T, image, mode string) {
	loseReply := mode == "recover" || mode == "cancel"
	srv, a := setupApp(t, func(c *config.Config) { c.Isolation.Docker.Image = image })
	go a.Reviews.Run(t.Context())
	const script = `set -eu
handler=$(mktemp)
cp ../backend/serve.sh "$handler"
chmod +x "$handler"
(cd ../backend && exec nc -lk -s 127.0.0.1 -p 18731 -e "$handler") &
pid=$!
trap 'kill "$pid"; rm -f "$handler"' EXIT
for i in 1 2 3 4 5; do wget -qO- http://127.0.0.1:18731/ready >/dev/null && break; sleep 0.1; done
(cd ../frontend && sh client.sh)
test "$(wget -qO- "http://127.0.0.1:18731/$(cat probe)")" = ready
`
	files := map[string]map[string]string{"infra": {"probe": "old", "verify.sh": script}, "frontend": {"path": "old", "client.sh": `test "$(wget -qO- "http://127.0.0.1:18731/$(cat path)")" = ready`}, "backend": {"endpoint": "old", "serve.sh": `#!/bin/sh
read -r method path rest || exit 1
if [ "$path" = "/$(cat endpoint)" ]; then
 printf 'HTTP/1.0 200 OK\r\nContent-Length: 5\r\n\r\nready'
else
 printf 'HTTP/1.0 404 Not Found\r\nContent-Length: 0\r\n\r\n'
fi
`}}
	remote := map[string]string{}
	bases := map[string]string{"infra": "production", "backend": "main", "frontend": "release/web"}
	prs := map[string]map[string]any{}
	var mu sync.Mutex
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		enc := json.NewEncoder(w)
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if r.URL.Path == "/user" {
			enc.Encode(map[string]string{"login": "fixture"})
			return
		}
		if len(parts) < 3 || parts[0] != "repos" {
			http.NotFound(w, r)
			return
		}
		alias := parts[2]
		if len(parts) == 3 {
			enc.Encode(map[string]any{"full_name": "fixture/" + alias, "default_branch": "main", "permissions": map[string]bool{"push": true}})
			return
		}
		if parts[3] != "pulls" {
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if r.Method == "GET" {
			rows := []map[string]any{}
			if p := prs[alias]; p != nil {
				rows = append(rows, p)
			}
			enc.Encode(rows)
			return
		}
		var in struct{ Head, Base string }
		if json.NewDecoder(r.Body).Decode(&in) != nil || in.Base != bases[alias] || prs[alias] != nil {
			http.Error(w, "unexpected duplicate or base", 422)
			return
		}
		out, err := exec.CommandContext(r.Context(), "git", "-C", remote[alias], "rev-parse", "--verify", "refs/heads/"+in.Head).Output()
		if err != nil {
			http.Error(w, "missing pushed ref", http.StatusUnprocessableEntity)
			return
		}
		head := strings.TrimSpace(string(out))
		pr := map[string]any{"number": 1, "state": "open", "html_url": "https://fixture.invalid/" + alias + "/pull/1", "head": map[string]string{"ref": in.Head, "sha": head}, "base": map[string]string{"ref": in.Base}}
		prs[alias] = pr
		if loseReply && alias == "backend" {
			http.Error(w, "response lost after creation", 503)
			return
		}
		w.WriteHeader(201)
		enc.Encode(pr)
	}))
	defer provider.Close()
	aliases := []string{"infra", "backend", "frontend"}
	model := &memberModel{lostReply: loseReply}
	var in taskconfig.Input
	json.Unmarshal([]byte(taskBody), &in)
	in.GraphID = "member-workflow"
	in.NodeID = "editor"
	in.ResultNodeID = "editor"
	for _, alias := range aliases {
		binding, bare := memberInput(t, srv, alias, files[alias])
		remote[alias] = bare
		memberGit(t, bare, "update-ref", "refs/heads/"+bases[alias], binding.BaseCommit)
		checks := []domain.Check{{Name: "test", Command: "true"}}
		if alias == "infra" {
			checks = append(checks, domain.Check{Name: "joint", Command: "sh verify.sh"})
			binding.RequiredChecks = []string{"joint"}
			if mode == "unchanged" {
				binding.RequiredChecks = []string{"test"}
			}
		}
		projects := projectconfig.Manager{Store: a.Store}
		if _, err := projects.SetChecks(t.Context(), binding.ProjectID, checks, 1, "admin"); err != nil {
			t.Fatal(err)
		}
		binding.Checks = checks
		synthetic := "fixture-only"
		approval := false
		c, err := a.SCM.Save(t.Context(), scm.Input{Name: alias, Kind: "github", Repository: "fixture/" + alias, APIURL: provider.URL, GitURL: "file://" + bare, Token: &synthetic, RequirePullRequestApproval: &approval}, "admin", false)
		if err != nil {
			t.Fatal(err)
		}
		binding.SCMConnection = alias
		binding.Repository = c.Repository
		binding.TargetBranch = bases[alias]
		binding.ConnectionRevision = c.UpdatedAt.UTC().Format(time.RFC3339Nano)
		if mode == "local" {
			binding.SCMConnection, binding.ConnectionRevision, binding.Repository, binding.TargetBranch = "", "", "", ""
		}
		in.Repositories = append(in.Repositories, binding)
	}
	model.add("filesystem.read", map[string]string{"path": "backend/endpoint"})
	for _, change := range [][2]string{{"backend/endpoint", "ready"}, {"frontend/path", "ready"}} {
		model.add("filesystem.write", map[string]string{"path": change[0], "content": change[1]})
	}
	for _, alias := range aliases {
		model.add("filesystem.write", map[string]string{"path": alias + "/contract-version", "content": alias + "-ready"})
		model.add("check.run", map[string]string{"name": alias + ".test"})
	}
	model.add("check.run", map[string]string{"name": "infra.joint"})
	model.add("filesystem.write", map[string]string{"path": "infra/probe", "content": "ready"})
	for _, alias := range aliases {
		model.add("git.add", map[string]any{"repository": alias, "paths": []string{"."}})
		model.add("git.commit", map[string]any{"repository": alias, "message": "change health endpoint", "all": true})
	}
	model.add("check.run", map[string]string{"name": "infra.joint"})
	if mode == "unchanged" {
		model.calls = nil
		model.add("check.run", map[string]string{"name": "infra.test"})
	}
	model.add("review.request", map[string]string{"title": "Health endpoint", "instructions": "Check the client and probe."})
	for _, alias := range aliases {
		if mode == "local" || mode == "unchanged" || alias == "frontend" && loseReply {
			break
		}
		model.add("git.push", map[string]string{"connection": alias})
		model.add("scm.pull_request.create", map[string]string{"connection": alias, "title": "Update health endpoint"})
	}
	model.add("finish", map[string]string{"signal": "confirm_stop", "summary": "Delivered all three repositories"})
	a.Models.Add(llm.ProviderConfig{Name: model.Name(), Type: "mock", DefaultModel: "fixture"}, model)
	graph := fmt.Sprintf(`{"metadata":{"id":"member-workflow","name":"Health checks"},"spec":{"nodes":[{"id":"editor","runtime":{"kind":"native"},"prompt":"Implement the cross-repository endpoint.","model":{"provider":"member-model","name":"fixture"},"loop":{"mode":"once"},"isolation":{"mode":"docker","image":%q},"limits":{"turnTimeout":"180s","maxToolCalls":50},"tools":["filesystem.read","filesystem.write","git.read","git.write","shell.exec","review.request"]}]}}`, image)
	if c := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/json", graph, nil); c != 201 {
		t.Fatalf("workflow %d", c)
	}
	var d taskconfig.Draft
	b, _ := json.Marshal(in)
	if c := call(t, srv, "POST", "/api/v1/tasks", operatorTok, "application/json", string(b), &d); c != 201 {
		t.Fatalf("task %d", c)
	}
	var review taskrun.Review
	path := "/api/v1/tasks/" + d.ID
	if c := call(t, srv, "GET", path+"/review", adminTok, "", "", &review); c != 200 || !review.CanStart {
		t.Fatalf("review %d %+v", c, review.Blockers)
	}
	model.inspectMounts = func(ctx context.Context) error {
		graph := taskrun.ExecutionID(d.ID)
		raw, err := exec.CommandContext(ctx, "docker", "inspect", "--format", "{{json .Mounts}}", "weft-sbx-"+graph+"-editor-0").Output()
		if err != nil {
			return fmt.Errorf("inspect editor mounts: %w", err)
		}
		var mounts []struct {
			Source, Destination string
			RW                  bool
		}
		if err = json.Unmarshal(raw, &mounts); err != nil {
			return err
		}
		expected := map[string]bool{"/workspace": false}
		for _, alias := range aliases {
			expected["/workspace/"+alias] = true
		}
		for _, mount := range mounts {
			if mount.Destination == "/workspace" || strings.HasPrefix(mount.Destination, "/workspace/") {
				suffix := graph + strings.TrimPrefix(mount.Destination, "/workspace")
				writable, ok := expected[mount.Destination]
				if !ok || writable != mount.RW || !strings.HasSuffix(mount.Source, "/"+suffix) {
					return fmt.Errorf("unexpected checkout mount: %+v", mount)
				}
				delete(expected, mount.Destination)
			}
		}
		if len(expected) != 0 {
			return fmt.Errorf("missing mounts: %v", expected)
		}
		return nil
	}
	auth := taskauth.Manager{Store: a.Store, Tools: a.Tools}
	scope, err := auth.Review(t.Context(), d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = auth.Save(t.Context(), d.ID, taskauth.Full, scope.Scope, 0, "fixture-admin", true); err != nil {
		t.Fatal(err)
	}
	type startResult struct {
		code int
		body string
	}
	starts := make(chan startResult, 2)
	for i := 0; i < 2; i++ {
		go func() {
			code, body := taskActionResponse(t, srv.URL, d.ID, "start", adminTok, d.Revision)
			starts <- startResult{code, body}
		}()
	}
	one, two := <-starts, <-starts
	if (one.code != 202 && two.code != 202) || one.code/100 >= 5 || two.code/100 >= 5 {
		t.Fatalf("concurrent starts: %+v %+v", one, two)
	}
	recovered := false
	deadline := time.Now().Add(150 * time.Second)
	var progress taskrun.View
	for time.Now().Before(deadline) {
		reviews, err := a.Reviews.List(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range reviews {
			if r.State == "pending" {
				required := "infra.joint"
				if mode == "unchanged" {
					required = "infra.test"
				}
				if len(r.Repositories) != 3 || r.VerifiedChecks[required] != "passed" {
					t.Fatalf("incomplete review, checks %+v", r.VerifiedChecks)
				}
				for _, alias := range aliases {
					if mode == "unchanged" {
						break
					}
					var evidence struct{ Diff string }
					code := call(t, srv, "GET", "/api/v1/reviews/"+r.ID+"/repositories/"+alias+"/diff", operatorTok, "", "", &evidence)
					if code != 200 || !strings.Contains(evidence.Diff, "+ready") {
						t.Fatalf("%s frozen diff: %d %q", alias, code, evidence.Diff)
					}
				}
				if _, err = a.Reviews.Decide(t.Context(), r.ID, "approved", "without_execution", "fixture-admin", "", int64(r.Generation)); err != nil {
					t.Fatal(err)
				}
			}
		}
		if c := call(t, srv, "GET", path+"/progress", adminTok, "", "", &progress); c != 200 {
			t.Fatal(c)
		}
		if progress.State == "delivery_pending" && loseReply && !recovered {
			if progress.Repositories[0].PullRequest == nil || progress.Repositories[1].Delivery != "pull_request unverified" || progress.Repositories[2].Delivery != "pending" {
				t.Fatalf("partial outcome %+v", progress.Repositories)
			}
			reopened, err := stores.OpenWithoutMigrations(t.Context(), stores.Config{Type: "sqlite", Dir: a.Config.Storage.Dir})
			if err != nil {
				t.Fatal(err)
			}
			runner := a.API.TaskRunner()
			runner.Store = reopened
			restored, err := runner.View(t.Context(), d.ID)
			reopened.Close()
			if err != nil || restored.Repositories[0].PullRequest.URL != progress.Repositories[0].PullRequest.URL || restored.Repositories[1].Delivery != "pull_request unverified" {
				t.Fatal("lost persisted delivery", err)
			}
			if mode == "cancel" {
				if _, err := a.API.TaskRunner().Cancel(t.Context(), d.ID, "admin"); err != nil {
					t.Fatal(err)
				}
				for time.Now().Before(deadline) {
					v, err := a.API.TaskRunner().View(t.Context(), d.ID)
					if err != nil {
						t.Fatal(err)
					}
					if v.State == "cancelled" {
						if v.Repositories[0].PullRequest.URL != progress.Repositories[0].PullRequest.URL || v.Repositories[1].Delivery != "pull_request unverified" || v.Repositories[2].Delivery != "pending" {
							t.Fatal("cancellation discarded delivery", v.Repositories)
						}
						return
					}
					time.Sleep(50 * time.Millisecond)
				}
				t.Fatal("task did not settle after cancellation")
			}
			for _, alias := range []string{"backend", "frontend"} {
				for _, credential := range []string{viewerTok, adminTok} {
					req, _ := http.NewRequest("POST", srv.URL+path+"/repositories/"+alias+"/deliver", strings.NewReader(`{"confirm":true}`))
					req.Header.Set("If-Match", strconv.FormatInt(progress.Task.Revision, 10))
					req.Header.Set("Authorization", "Bearer "+credential)
					res, err := http.DefaultClient.Do(req)
					if err != nil {
						t.Fatal(err)
					}
					res.Body.Close()
					if credential == viewerTok && res.StatusCode != 403 || credential == adminTok && res.StatusCode != 200 {
						t.Fatal("retry", alias, res.StatusCode)
					}
				}
			}
			recovered = true
		}
		if mode == "unchanged" && strings.Contains(strings.Join(progress.Blockers, " "), "no repository changed") {
			if progress.State == "succeeded" {
				t.Fatal("empty PR task succeeded")
			}
			for _, row := range progress.Repositories {
				if row.Delivery != "unchanged" || row.PullRequest != nil {
					t.Fatal(row)
				}
			}
			return
		}
		if progress.State == "succeeded" {
			break
		}
		if progress.State == "failed" || progress.State == "blocked" || progress.State == "interrupted" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if progress.State != "succeeded" || len(progress.Repositories) != 3 {
		model.mu.Lock()
		step := model.step
		model.mu.Unlock()
		t.Fatalf("outcome %s after model step %d: %+v, result %+v", progress.State, step, progress.Blockers, progress.Result)
	}
	if mode == "local" {
		for _, row := range progress.Repositories {
			if row.PullRequest != nil || row.Delivery != "" {
				t.Fatal(row)
			}
		}
		return
	}
	for _, row := range progress.Repositories {
		if row.PullRequest == nil || row.Delivery != "pull_request confirmed" || row.PullRequest.Base != bases[row.Alias] {
			t.Fatal(row)
		}
	}
	startsRecorded, err := a.Store.ListEvents(t.Context(), storage.EventFilter{GraphID: progress.Task.ExecutionGraphID, Types: []domain.EventType{domain.EvGraphStarted}, Limit: 3})
	if err != nil || len(startsRecorded) != 1 {
		t.Fatalf("expected one admitted run: %d %v", len(startsRecorded), err)
	}
	if recovered != loseReply {
		t.Fatal("lost delivery was not recovered")
	}
	evidence, err := a.Store.ListEvents(t.Context(), storage.EventFilter{GraphID: progress.Task.ExecutionGraphID, Types: []domain.EventType{domain.EvCheckCompleted}, Ascending: true, Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	writes, err := a.Store.ListEvents(t.Context(), storage.EventFilter{GraphID: progress.Task.ExecutionGraphID, Types: []domain.EventType{domain.EvToolInvoked}, Limit: 200})
	if err != nil {
		t.Fatal(err)
	}
	execution := ""
	for _, event := range writes {
		var data struct{ Tool string }
		if err := json.Unmarshal(event.Data, &data); err != nil {
			t.Fatal(err)
		}
		if data.Tool != "filesystem.write" {
			continue
		}
		if event.ExecutionID == "" || execution != "" && event.ExecutionID != execution {
			t.Fatal("member edits belong to different executions")
		}
		execution = event.ExecutionID
	}
	if execution == "" {
		t.Fatal("no native edits recorded")
	}
	var joint []tools.CheckRun
	locals := map[string]bool{}
	for _, e := range evidence {
		var run tools.CheckRun
		if err := json.Unmarshal(e.Data, &run); err != nil {
			t.Fatal(err)
		}
		if run.Check == "infra.joint" {
			joint = append(joint, run)
		} else {
			locals[run.Check] = run.Passed
		}
	}
	if len(joint) != 2 || joint[0].Passed || !joint[1].Passed || joint[1].Stale || len(joint[1].Repositories) != 3 {
		t.Fatal("incorrect joint evidence", joint)
	}
	for _, r := range progress.Repositories {
		if !locals[r.Alias+".test"] || joint[1].Repositories[r.Alias].Head != r.PullRequest.HeadSHA {
			t.Fatal("check inputs differ from PR", r.Alias)
		}
		var diff struct{ Content string }
		if c := call(t, srv, "GET", "/api/v1/graphs/"+progress.Task.ExecutionGraphID+"/git/diff?repository="+r.Alias, viewerTok, "", "", &diff); c != 200 || !strings.Contains(diff.Content, "+ready") || !strings.Contains(diff.Content, "+"+r.Alias+"-ready") {
			t.Fatal("missing reviewed diff", r.Alias, c, diff.Content)
		}
	}
	// Exercise a fresh checkout of the delivered commits, independently of
	// the workspaces and recorded test result.
	exported := t.TempDir()
	for _, row := range progress.Repositories {
		dir := filepath.Join(exported, row.Alias)
		memberGit(t, exported, "clone", "-q", "--no-checkout", remote[row.Alias], dir)
		memberGit(t, dir, "checkout", "-q", row.PullRequest.HeadSHA)
	}
	cmd := exec.Command("docker", "run", "--rm", "--network", "none", "-v", exported+":/review:ro", "-w", "/review/infra", image, "sh", "verify.sh")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
}

func TestLastCheckoutFailureRetainsPriorMembersWithoutDispatch(t *testing.T) {
	// Only daemon readiness is consulted. A real Git binding conflict stops
	// admission before the engine creates a graph or invokes a sandbox.
	srv, a := setupApp(t, func(c *config.Config) { c.Isolation.Docker.Bin = "/bin/true" })
	graph := strings.Replace(taskGraph, `"loop":`, `"isolation":{"mode":"docker","image":"fixture"},"loop":`, 1)
	if c := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/json", graph, nil); c != 201 {
		t.Fatal(c)
	}
	var in taskconfig.Input
	if err := json.Unmarshal([]byte(taskBody), &in); err != nil {
		t.Fatal(err)
	}
	for _, alias := range []string{"infra", "backend", "frontend"} {
		binding, _ := memberInput(t, srv, alias, map[string]string{"file": alias})
		in.Repositories = append(in.Repositories, binding)
	}
	var d taskconfig.Draft
	b, _ := json.Marshal(in)
	if c := call(t, srv, "POST", "/api/v1/tasks", operatorTok, "application/json", string(b), &d); c != 201 {
		t.Fatal(c)
	}
	graphID := domain.TaskExecutionID(d.ID)
	if _, err := a.Store.PutSetting(t.Context(), "project-checkouts/"+graphID+"/frontend", []byte(`{}`), 0, "fixture"); err != nil {
		t.Fatal(err)
	}
	c, _ := taskActionResponse(t, srv.URL, d.ID, "start", adminTok, d.Revision)
	if c/100 == 2 {
		t.Fatal("partial workspace accepted")
	}
	view, err := a.API.TaskRunner().View(t.Context(), d.ID)
	if err != nil || view.State != "interrupted" || len(view.Repositories) != 3 || view.Repositories[0].Preparation != "ready" || view.Repositories[1].Preparation != "ready" || view.Repositories[2].Preparation != "failed" || !strings.Contains(view.Task.LaunchError, "frontend") {
		t.Fatal(view, err)
	}
	msgs, err := a.Store.ListOperatorMessages(t.Context(), graphID, in.NodeID, 10)
	if err != nil || len(msgs) != 0 {
		t.Fatal(msgs, err)
	}
	if _, err := a.Store.GetGraph(t.Context(), graphID, 0); !errors.Is(err, storage.ErrNotFound) {
		t.Fatal("partial task deployed", err)
	}
	if _, err = a.API.TaskRunner().Cancel(t.Context(), d.ID, "fixture-admin"); err != nil {
		t.Fatal(err)
	}
	if _, err = a.API.TaskRunner().PreviewCleanup(t.Context(), d.ID, "", 0, "infra"); err != nil {
		t.Fatal("known owned member could not be reviewed for cleanup", err)
	}
}
