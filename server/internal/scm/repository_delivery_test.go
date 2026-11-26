package scm_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/review"
	"github.com/woliveiras/weft/server/internal/scm"
	"github.com/woliveiras/weft/server/internal/tools"
)

// Models remote PR persistence independently from the server's receipt store.
// Git upload and ls-remote still run against a different bare repo per project.
type memberProvider struct {
	*fake
	t                 *testing.T
	repository, dir   string
	mu                sync.Mutex
	created           int
	pr                *scm.PullRequest
	loseReply         bool
	lookupUnavailable bool
}

func (p *memberProvider) Identity(context.Context) (scm.Identity, error) {
	return scm.Identity{Repository: p.repository, DefaultBranch: "main", CanPush: true}, nil
}
func (p *memberProvider) RemoteURL() string { return "file://" + p.dir }
func (p *memberProvider) FindPullRequest(_ context.Context, head, base string) (*scm.PullRequest, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.lookupUnavailable {
		return nil, errors.New("temporary lookup failure")
	}
	if p.pr != nil && p.pr.Head == head && p.pr.Base == base {
		copy := *p.pr
		return &copy, nil
	}
	return nil, nil
}
func (p *memberProvider) CreatePullRequest(_ context.Context, in scm.PullRequestInput) (*scm.PullRequest, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.created++
	p.pr = &scm.PullRequest{Head: in.Head, Base: in.Base, HeadSHA: git(p.t, p.dir, "rev-parse", "refs/heads/"+in.Head), Number: p.created, URL: "https://example.invalid/" + p.repository + "/pull/1", State: "open"}
	if p.loseReply {
		p.loseReply = false
		return nil, errors.New("simulated lost response")
	}
	return p.pr, nil
}
func TestReviewedMultiRepositoryDeliveryRecoversOriginalPR(t *testing.T) {
	m, store := manager(t)
	providers := map[string]*memberProvider{}
	scm.RegisterKind("member-fixture", func(c scm.Connection, _ string) (scm.Provider, error) { return providers[c.Name], nil })
	graphID := "task-run-scope"
	dir := filepath.Join(t.TempDir(), graphID)
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	var bindings []domain.TaskRepository
	aliases := []string{"infra", "backend", "frontend"}
	for _, alias := range aliases {
		work, bare := repo(t)
		dest := filepath.Join(dir, alias)
		if err := os.Rename(work, dest); err != nil {
			t.Fatal(err)
		}
		base := git(t, dest, "rev-parse", "HEAD")
		git(t, dest, "checkout", "-qb", "weft/"+graphID)
		name := "acme/" + alias
		providers[alias] = &memberProvider{fake: &fake{}, t: t, repository: name, dir: bare, loseReply: alias == "backend"}
		c, err := m.Save(ctx, scm.Input{Name: alias, Kind: "member-fixture", Repository: name, GitURL: "file://" + bare, Token: ptr(token), RequirePullRequestApproval: ptr(false)}, "admin", false)
		if err != nil {
			t.Fatal(err)
		}
		bindings = append(bindings, domain.TaskRepository{Alias: alias, ProjectID: "prj_" + alias, BaseCommit: base, SCMConnection: alias, Repository: name, TargetBranch: "main", ConnectionRevision: c.UpdatedAt.UTC().Format(time.RFC3339Nano)})
		if alias != "frontend" {
			if err = os.WriteFile(filepath.Join(dest, "a.txt"), []byte(alias), 0600); err != nil {
				t.Fatal(err)
			}
			git(t, dest, "commit", "-qam", "change "+alias)
		}
	}
	g := &domain.Graph{Metadata: domain.GraphMetadata{ID: graphID}, Spec: domain.GraphSpec{Workspace: domain.WorkspaceSpec{Repositories: bindings}, Nodes: []domain.Node{{ID: "result", Isolation: domain.IsolationSpec{Mode: domain.IsolationDocker}}}}}
	reviews := review.New(review.Options{Store: store, Root: t.TempDir()})
	record, err := reviews.Create(ctx, g, "result", 0, "execution", dir, review.Request{Title: "Three projects", Instructions: "Inspect each change."})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = reviews.Decide(ctx, record.ID, "approved", "without_execution", "operator", "", int64(record.Generation)); err != nil {
		t.Fatal(err)
	}
	p := principal(dir, domain.ToolGrant{Name: "git.push", Repositories: aliases}, domain.ToolGrant{Name: "scm.pull_request.create", Repositories: aliases})
	p.GraphID = graphID
	p.Repositories = bindings
	call := func(gw *tools.Gateway, alias, name string) error {
		args, _ := json.Marshal(map[string]string{"connection": alias, "title": "Change"})
		res, err := gw.Invoke(ctx, p, name, args)
		if err == nil && res != nil && res.IsError {
			return errors.New(res.Content)
		}
		return err
	}
	gw := gateway(t, m)
	for _, alias := range aliases {
		if err := call(gw, alias, "git.push"); err != nil {
			t.Fatal(err)
		}
		err := call(gw, alias, "scm.pull_request.create")
		if (err != nil) != (alias == "backend") {
			t.Fatalf("%s: %v", alias, err)
		}
	}
	receipt, _, err := scm.ReadRepositoryReceipt(ctx, store, graphID, "backend", "pull_request")
	if err != nil || receipt == nil || receipt.State != "unverified" {
		t.Fatal(receipt, err)
	}
	reopened := gateway(t, m)
	providers["backend"].lookupUnavailable = true
	if err := call(reopened, "backend", "scm.pull_request.create"); err == nil || providers["backend"].created != 1 {
		t.Fatal("lookup outage repeated an uncertain write", err)
	}
	providers["backend"].lookupUnavailable = false
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { errs <- call(reopened, "backend", "scm.pull_request.create") }()
	}
	// A concurrent CAS may reject one reconciler. Both observe the same PR.
	var confirmed bool
	for i := 0; i < 2; i++ {
		if <-errs == nil {
			confirmed = true
		}
	}
	if !confirmed {
		t.Fatal("neither retry confirmed the existing PR")
	}
	for _, alias := range []string{"infra", "backend"} {
		rr, _, err := scm.ReadRepositoryReceipt(ctx, store, graphID, alias, "pull_request")
		if err != nil || rr == nil || rr.State != "confirmed" || rr.PullRequest.HeadSHA != git(t, providers[alias].dir, "rev-parse", "refs/heads/weft/"+graphID) || providers[alias].created != 1 {
			t.Fatal(alias, rr, err)
		}
	}
	if git(t, providers["frontend"].dir, "for-each-ref") != "" || providers["frontend"].created != 0 {
		t.Fatal("unchanged member received a remote write")
	}
	if err = os.WriteFile(filepath.Join(dir, "frontend", "a.txt"), []byte("candidate changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = call(reopened, "infra", "git.push"); err == nil {
		t.Fatal("accepted stale candidate approval")
	}
}
