package scm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/review"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/taskauth"
	"github.com/woliveiras/weft/server/internal/tools"
	"github.com/woliveiras/weft/server/internal/workspace"
)

// Receipt survives a lost response or process restart. A pending intent can
// only be recovered from a matching remote observation, never blindly replayed.
type RepositoryReceipt struct {
	Alias       string       `json:"alias"`
	Phase       string       `json:"phase"`
	State       string       `json:"state"`
	Connection  string       `json:"connection"`
	Repository  string       `json:"repository"`
	Branch      string       `json:"branch"`
	Base        string       `json:"base"`
	Head        string       `json:"head"`
	Candidate   string       `json:"candidate"`
	PullRequest *PullRequest `json:"pullRequest,omitempty"`
	Message     string       `json:"message,omitempty"`
}

func receiptKey(graph, alias, phase string) string {
	return "tasks/scm-delivery/" + graph + "/" + alias + "/" + phase
}
func ReadRepositoryReceipt(ctx context.Context, s storage.Store, graph, alias, phase string) (*RepositoryReceipt, int64, error) {
	row, err := s.GetSetting(ctx, receiptKey(graph, alias, phase))
	if errors.Is(err, storage.ErrNotFound) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	var receipt RepositoryReceipt
	if err = json.Unmarshal(row.Value, &receipt); err != nil {
		return nil, 0, err
	}
	return &receipt, row.Revision, nil
}
func (m Manager) writeReceipt(ctx context.Context, inv *tools.Invocation, r *RepositoryReceipt, revision int64) (int64, error) {
	b, err := json.Marshal(r)
	if err != nil {
		return 0, err
	}
	rev, err := m.Store.PutSetting(ctx, receiptKey(inv.Principal.GraphID, r.Alias, r.Phase), b, revision, inv.Principal.Actor)
	if err != nil {
		return 0, err
	}
	ev := domain.NewEvent("task.repository_delivery", inv.Principal.GraphID, map[string]string{"taskId": inv.Principal.TaskID, "repository": r.Alias, "phase": r.Phase, "state": r.State, "head": r.Head}).WithActor(inv.Principal.Actor)
	return rev, m.Store.AppendEvent(ctx, ev)
}

func remoteHead(ctx context.Context, p Provider, dir, branch string) (string, error) {
	child, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(child, "git", "ls-remote", "--refs", "--", p.RemoteURL(), "refs/heads/"+branch)
	cmd.Dir = dir
	cmd.Env = append(gitEnv(), "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=http.extraHeader", "GIT_CONFIG_VALUE_0=Authorization: "+p.GitAuthHeader())
	data, err := cmd.Output()
	if err != nil {
		return "", errors.New("remote branch state could not be verified")
	}
	if len(data) == 0 {
		return "", nil
	}
	fields := strings.Fields(string(data))
	if len(fields) != 2 || !commitID.MatchString(fields[0]) || fields[1] != "refs/heads/"+branch {
		return "", errors.New("unexpected remote branch identity")
	}
	return fields[0], nil
}

func (m Manager) deliverRepository(ctx context.Context, inv *tools.Invocation, c Connection, branch, title, body, phase string) (*tools.Result, error) {
	p := inv.Principal
	if p == nil || len(p.Repositories) == 0 {
		return nil, tools.ErrDenied
	}
	var repo *domain.TaskRepository
	for i := range p.Repositories {
		r := &p.Repositories[i]
		if r.SCMConnection == c.Name {
			repo = r
			break
		}
	}
	if repo == nil || repo.Repository != c.Repository || repo.ConnectionRevision != c.UpdatedAt.UTC().Format(time.RFC3339Nano) {
		return nil, fmt.Errorf("%w: delivery connection is outside reviewed task scope or changed", tools.ErrDenied)
	}
	expectedBranch := c.Prefix() + p.GraphID
	if (branch != "" && branch != expectedBranch) || !strings.HasPrefix(expectedBranch, c.Prefix()) || expectedBranch == repo.TargetBranch {
		return nil, fmt.Errorf("%w: select the reviewed task branch", tools.ErrDenied)
	}
	dir := inv.Workdir
	if filepath.Base(dir) != p.GraphID {
		return nil, fmt.Errorf("%w: expected the admitted task workspace", tools.ErrDenied)
	}
	var tool tools.Tool = pushTool{m: m}
	if phase == "pull_request" {
		tool = pullRequestTool{m: m}
	}
	authorization := func() (string, error) {
		if p.TaskID == "" {
			return "", nil
		}
		_, scope, err := (taskauth.Manager{Store: m.Store}).Check(ctx, p, tool, false, inv.Args)
		return scope, err
	}
	consent, err := authorization()
	if err != nil {
		return nil, err
	}
	if inv.TaskConsent != "" && consent != inv.TaskConsent {
		return nil, review.ErrStale
	}
	candidate := func() (map[string]workspace.RepositoryState, *review.Record, error) {
		states, err := workspace.RepositoryStates(ctx, dir, p.Repositories)
		if err != nil {
			return nil, nil, err
		}
		for _, r := range p.Repositories {
			s := states[r.Alias]
			if s.Dirty || s.Branch != "weft/"+p.GraphID {
				return nil, nil, fmt.Errorf("%s: commit or preserve all changes on the task branch before delivery", r.Alias)
			}
		}
		blockers, err := tools.RepositoryCheckBlockers(ctx, m.Store, p.GraphID, p.Repositories, states)
		if err != nil {
			return nil, nil, err
		}
		if len(blockers) > 0 {
			return nil, nil, fmt.Errorf("required checks: %s", strings.Join(blockers, ", "))
		}
		approved, err := review.New(review.Options{Store: m.Store}).ApprovedRepositories(ctx, p.GraphID, states)
		return states, approved, err
	}
	states, approved, err := candidate()
	if err != nil {
		return nil, err
	}
	state := states[repo.Alias]
	if !state.Changed {
		return &tools.Result{Content: "Repository " + repo.Alias + " is unchanged; no delivery is required."}, nil
	}
	provider, token, err := m.Open(c)
	if err != nil {
		return nil, err
	}
	identity, err := provider.Identity(ctx)
	if err != nil {
		return nil, errors.New("could not verify delivery identity")
	}
	if !strings.EqualFold(identity.Repository, c.Repository) || expectedBranch == identity.DefaultBranch || !identity.CanPush {
		return nil, fmt.Errorf("%w: invalid remote identity or protected branch", tools.ErrDenied)
	}
	if phase == "pull_request" && c.ApprovalRequired() {
		if err = approve(ctx, inv, c, expectedBranch, repo.TargetBranch, title); err != nil {
			return nil, err
		}
	}
	// A human or network wait may outlive the candidate and connection review.
	fresh, record, err := candidate()
	if err != nil {
		return nil, err
	}
	if record.ID != approved.ID || fresh[repo.Alias] != state {
		return nil, review.ErrStale
	}
	verifyScope := func() error {
		current, err := m.Get(ctx, c.Name)
		if err != nil {
			return err
		}
		authorized, err := authorization()
		if err != nil {
			return err
		}
		if !current.UpdatedAt.Equal(c.UpdatedAt) || authorized != consent {
			return review.ErrStale
		}
		return nil
	}
	if err := verifyScope(); err != nil {
		return nil, err
	}
	receipt := RepositoryReceipt{Alias: repo.Alias, Phase: phase, Connection: c.Name, Repository: c.Repository, Branch: expectedBranch, Base: repo.TargetBranch, Head: state.Head, Candidate: approved.ID}
	err = workspace.WithRepositorySnapshot(ctx, dir, *repo, func(snapshot workspace.RepositoryState, gitDir string) error {
		if snapshot != state {
			return review.ErrStale
		}
		prev, revision, e := ReadRepositoryReceipt(ctx, m.Store, p.GraphID, repo.Alias, phase)
		if e != nil {
			return e
		}
		recordLookupFailure := func(message string) error {
			if prev != nil && prev.State != "failed" {
				// Retain prior intent or confirmed evidence when a read fails.
				return errors.New(message)
			}
			receipt.State, receipt.Message = "failed", message
			_, writeErr := m.writeReceipt(ctx, inv, &receipt, revision)
			return errors.Join(errors.New(message), writeErr)
		}
		remote, e := remoteHead(ctx, provider, gitDir, expectedBranch)
		if e != nil {
			return recordLookupFailure("Remote branch could not be verified.")
		}
		var pr *PullRequest
		if phase == "pull_request" {
			if remote != state.Head {
				return errors.New("push the reviewed commit before opening its pull request")
			}
			pr, e = provider.FindPullRequest(ctx, expectedBranch, repo.TargetBranch)
			if e != nil {
				return recordLookupFailure("Existing pull request could not be verified.")
			}
		}
		confirmed := phase == "push" && remote == state.Head || phase == "pull_request" && pr != nil
		if !confirmed {
			if prev != nil && (prev.State == "pending" || prev.State == "unverified" || prev.State == "confirmed" && prev.Head == state.Head && prev.Candidate == approved.ID) {
				return errors.New("previous delivery outcome is unverified; inspect the remote before retrying")
			}
			if err := verifyScope(); err != nil {
				return err
			}
			latest, gate, err := candidate()
			if err != nil {
				return err
			}
			if gate.ID != approved.ID || latest[repo.Alias] != state {
				return review.ErrStale
			}
			receipt.State = "pending"
			revision, e = m.writeReceipt(ctx, inv, &receipt, revision)
			if e != nil {
				return e
			}
			if phase == "push" {
				// Only the inspected private copy gets a local ref for the destination prefix.
				cmd := exec.CommandContext(ctx, "git", "-c", "core.hooksPath="+os.DevNull, "update-ref", "refs/heads/"+expectedBranch, state.Head)
				cmd.Dir = gitDir
				cmd.Env = gitEnv()
				if err := cmd.Run(); err != nil {
					return errors.New("could not bind the reviewed head for delivery")
				}
				_, e = Push(ctx, c, provider, token, gitDir, expectedBranch, identity.DefaultBranch)
			} else {
				pr, e = provider.CreatePullRequest(ctx, PullRequestInput{Title: title, Body: body, Head: expectedBranch, Base: repo.TargetBranch})
			}
			if e != nil {
				receipt.State = "unverified"
				receipt.Message = "Remote response was inconclusive. Retry to reconcile the existing operation."
				var githubErr *apiError
				var httpErr *providerHTTPError
				var status int
				if errors.As(e, &githubErr) {
					status = githubErr.Status
				} else if errors.As(e, &httpErr) {
					status = httpErr.Status
				}
				switch status {
				case 400, 401, 403, 404, 422, 429:
					receipt.State = "failed"
					receipt.Message = fmt.Sprintf("Provider refused %s (HTTP %d). Correct the cause and retry.", phase, status)
				}
				_, saveErr := m.writeReceipt(context.WithoutCancel(ctx), inv, &receipt, revision)
				return errors.Join(errors.New(receipt.Message), saveErr)
			}
			if phase == "push" {
				verified, e := remoteHead(ctx, provider, gitDir, expectedBranch)
				if e != nil || verified != state.Head {
					return errors.New("remote did not confirm the reviewed commit")
				}
			}
		}
		if phase == "pull_request" {
			if pr == nil || pr.Number <= 0 || !validPullRequestURL(pr.URL) || pr.Head != expectedBranch || pr.Base != repo.TargetBranch || pr.HeadSHA != state.Head || pr.State != "open" {
				return errors.New("the provider did not confirm a PR for the reviewed head and base")
			}
			receipt.PullRequest = &PullRequest{Number: pr.Number, URL: redactReviewURL(pr.URL, token), Head: pr.Head, Base: pr.Base, HeadSHA: pr.HeadSHA, State: pr.State}
		}
		receipt.State = "confirmed"
		if confirmed && prev != nil && prev.State == "confirmed" && prev.Candidate == receipt.Candidate && prev.Head == receipt.Head {
			return nil
		}
		_, e = m.writeReceipt(ctx, inv, &receipt, revision)
		return e
	})
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", repo.Alias, phase, err)
	}
	event := domain.EvBranchPushed
	data := map[string]any{"alias": repo.Alias, "connection": c.Name, "repository": c.Repository, "branch": expectedBranch, "headSha": state.Head}
	content := "Verified push of " + state.Head + " to " + c.Repository
	if receipt.PullRequest != nil {
		event = domain.EvPullRequestOpened
		data = deliveryEvent(c, *receipt.PullRequest, token)
		data["alias"] = repo.Alias
		content = "Verified pull request " + receipt.PullRequest.URL
	}
	m.event(ctx, p, event, data)
	payload, err := json.Marshal(receipt)
	if err != nil {
		return nil, err
	}
	return &tools.Result{Content: content, Data: payload}, nil
}
