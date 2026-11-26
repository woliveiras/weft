package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/engine"
	"github.com/woliveiras/weft/server/internal/scm"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/tools"
)

type manualPRTarget struct {
	proposal chat.PRAnalysis
	grant    domain.ToolGrant
}

var errPRScope = errors.New("no unique eligible review destination")
var errPRRead = errors.New("review evidence unavailable")

func (s *Server) selectPRTarget(ctx context.Context, selector chat.PRSelector) (manualPRTarget, error) {
	var target manualPRTarget
	records, err := s.Store.ListGraphRecords(ctx)
	if err != nil {
		return target, err
	}
	connections, err := s.SCM.List(ctx)
	if err != nil {
		return target, err
	}
	number, err := strconv.Atoi(selector.Number)
	if err != nil || !selector.Valid() {
		return target, errPRScope
	}
	for _, rec := range records {
		if rec.DesiredState != domain.GraphRunning || rec.State != domain.GraphRunning || rec.RunID == "" || strings.HasPrefix(rec.ID, domain.TaskRunGraphPrefix) || (selector.Workflow != "" && selector.Workflow != rec.ID) {
			continue
		}
		g, _, err := s.Engine.GetGraph(ctx, rec.ID, rec.Version)
		if err != nil {
			return target, err
		}
		if g.Metadata.Labels[domain.LabelTask] != "" {
			continue
		}
		for i := range g.Spec.Nodes {
			n := &g.Spec.Nodes[i]
			if n.Disabled || n.Runtime.Kind != domain.RuntimeNative || n.Loop.Mode != domain.LoopEvent || n.Loop.BatchSize > 1 || len(g.Outgoing(n.ID)) != 0 || n.Model.Provider == "" || n.Model.Name == "" {
				continue
			}
			ingress := g.Incoming(n.ID)
			if len(ingress) != 1 || ingress[0].From != "" || !slices.Equal(g.Consumers(ingress[0]), []string{n.ID}) {
				continue
			}
			for _, c := range connections {
				if selector.Connection != "" && c.Name != selector.Connection {
					continue
				}
				if c.Kind != "github" && c.Kind != "bitbucket-cloud" && c.Kind != "bitbucket-dc" {
					continue
				}
				for _, grant := range n.Tools {
					if grant.Name != scm.CapPullRequestRead || tools.CheckRepository(grant, c.Name) != nil {
						continue
					}
					if target.proposal.Graph != "" {
						return manualPRTarget{}, errPRScope
					}
					target = manualPRTarget{chat.PRAnalysis{Number: number, Connection: c.Name, ConnectionRevision: c.UpdatedAt.UTC().Format(time.RFC3339Nano), Provider: c.Kind, Repository: c.Repository, Graph: rec.ID, Version: rec.Version, Run: rec.RunID, Node: n.ID, Channel: ingress[0].ID, ModelProvider: n.Model.Provider, ModelName: n.Model.Name}, grant}
				}
			}
		}
	}
	if target.proposal.Graph == "" {
		return target, errPRScope
	}
	return target, nil
}

func (s *Server) readChatPR(ctx context.Context, actor Identity, t manualPRTarget, diff bool) (scm.ReviewSnapshot, string, error) {
	var result struct {
		PullRequest                                                scm.ReviewSnapshot `json:"pullRequest"`
		Connection, ConnectionRevision, Repository, Provider, Diff string
	}
	p := t.proposal
	if s.Gateway == nil {
		return result.PullRequest, "", errPRRead
	}
	args, _ := json.Marshal(map[string]any{"connection": p.Connection, "connectionRevision": p.ConnectionRevision, "number": p.Number, "diff": diff})
	response, err := s.Gateway.Invoke(tools.WithoutApproval(ctx), &tools.Principal{Actor: actor.Name, GraphID: p.Graph, GraphVersion: p.Version, NodeID: p.Node, WorkspaceReadOnly: true, Grants: []domain.ToolGrant{t.grant}}, scm.CapPullRequestRead, args)
	if err != nil || response == nil || response.IsError || response.Truncated || json.Unmarshal(response.Data, &result) != nil ||
		result.Connection != p.Connection || result.Provider != p.Provider || result.ConnectionRevision != p.ConnectionRevision || result.Repository != p.Repository || result.PullRequest.Number != p.Number || result.PullRequest.State != "open" {
		return scm.ReviewSnapshot{}, "", errPRRead
	}
	return result.PullRequest, result.Diff, nil
}

func (s *Server) reviewChatPR(ctx context.Context, actor Identity, id string, plan chat.Plan) chat.Interpretation {
	out := chat.Interpretation{Plan: plan}
	if !actor.Role.Allows(config.RoleOperator) {
		out.Text = "An operator is required to request a pull request analysis."
		return out
	}
	target, err := s.selectPRTarget(ctx, plan.PRSelector)
	if errors.Is(err, errPRScope) {
		out.Plan.Kind, out.Destination, out.Text = chat.Clarify, "workflows", "Which running review workflow and SCM connection should read this PR? Choose a native event reviewer with a single ingress and a repository read grant."
		return out
	}
	if err != nil {
		out.Text, out.Retryable = "The review destination could not be inspected.", true
		return out
	}
	snapshot, _, err := s.readChatPR(ctx, actor, target, false)
	if err != nil {
		out.Text, out.Destination = "The selected gateway could not read this open PR with the current grant. Inspect the connection and any required tool approval.", "connections"
		return out
	}
	p := target.proposal
	p.Head, p.Base, p.HeadRef, p.BaseRef, p.Draft = snapshot.HeadSHA, snapshot.BaseSHA, snapshot.Head, snapshot.Base, snapshot.Draft
	out.Review, err = chat.NewPRReview(actor.chatOwner, id, p, time.Now())
	if err != nil {
		out.Text = "The selected PR lacks a complete, displayable revision identity."
		return out
	}
	out.Text = fmt.Sprintf("Review the source and destination for analysis of PR #%d.", p.Number)
	return out
}

func (s *Server) dispatchChatPR(ctx context.Context, actor Identity, session string, r *chat.OperationReview) (*chat.Session, error) {
	p := *r.PR
	finish := func(state string) (*chat.Session, error) {
		return s.chatStore().ObservePR(ctx, actor.chatOwner, session, r.ID, r.State, state, "", "")
	}
	t, err := s.selectPRTarget(ctx, chat.PRSelector{Workflow: p.Graph, Connection: p.Connection, Number: strconv.Itoa(p.Number)})
	if errors.Is(err, errPRScope) {
		return finish(chat.ReviewConflict)
	}
	if err != nil {
		return finish(chat.ReviewUnknown)
	}
	scope := p
	scope.Head, scope.Base, scope.HeadRef, scope.BaseRef, scope.Draft = "", "", "", "", false
	if scope != t.proposal {
		return finish(chat.ReviewConflict)
	}
	snapshot, diff, err := s.readChatPR(ctx, actor, t, true)
	if err != nil {
		return finish(chat.ReviewConflict)
	}
	t.proposal.Head, t.proposal.Base, t.proposal.HeadRef, t.proposal.BaseRef, t.proposal.Draft = snapshot.HeadSHA, snapshot.BaseSHA, snapshot.Head, snapshot.Base, snapshot.Draft
	if p != t.proposal || len(diff) > 12<<10 || strings.TrimSpace(diff) == "" || !chat.SafeReviewContent(diff) {
		return finish(chat.ReviewConflict)
	}
	payload, err := json.Marshal(struct {
		Repository, Head, Base string
		Number                 int
		Diff                   string
	}{p.Repository, p.Head, p.Base, p.Number, diff})
	if err != nil {
		return nil, err
	}
	m, err := domain.NewMessage("review.requested", "", p.Node, payload)
	if err != nil {
		return nil, err
	}
	m.ID = r.ID
	m.SetMeta(domain.MetaCorrelationID, r.ID)
	_, err = s.Engine.DispatchReview(ctx, engine.ReviewDispatch{AnalysisOnly: true, GraphID: p.Graph, Version: p.Version, Node: p.Node, Channel: p.Channel, ExpectedRunID: p.Run, Message: m})
	if errors.Is(err, engine.ErrConflict) || errors.Is(err, engine.ErrInvalid) {
		return finish(chat.ReviewConflict)
	}
	if err != nil {
		return finish(chat.ReviewUnknown)
	}
	return finish(chat.ReviewQueued)
}

func (s *Server) observeChatPR(ctx context.Context, owner string, session *chat.Session, r *chat.OperationReview) (*chat.Session, error) {
	p := r.PR
	records, err := s.Store.GetMessage(ctx, r.ID)
	update := func(state, execution, summary string) (*chat.Session, error) {
		return s.chatStore().ObservePR(ctx, owner, session.ID, r.ID, r.State, state, execution, summary)
	}
	if errors.Is(err, storage.ErrNotFound) {
		if time.Since(r.ClaimedAt) > time.Minute {
			return update(chat.ReviewUnknown, "", "")
		}
		return session, nil
	}
	if err != nil {
		return nil, err
	}
	matched := false
	for _, m := range records {
		if m.GraphID != p.Graph || m.Channel != p.Channel || m.Message.MetaString(domain.MetaRunID) != p.Run || m.Message.MetaInt(domain.MetaGraphVersion) != p.Version || m.Message.Target != p.Node {
			continue
		}
		matched = true
		switch m.Status {
		case domain.MsgDeadLettered, domain.MsgDropped, domain.MsgRejected:
			return update(chat.ReviewIncomplete, "", "")
		case domain.MsgAcked:
			x, err := s.Store.GetExecution(ctx, m.Consumer)
			if errors.Is(err, storage.ErrNotFound) {
				return update(chat.ReviewUnknown, "", "")
			}
			if err != nil {
				return nil, err
			}
			if x.RunID != p.Run || x.GraphID != p.Graph || x.GraphVer != p.Version || x.NodeID != p.Node || !slices.Equal(x.InputIDs, []string{r.ID}) {
				return update(chat.ReviewUnknown, "", "")
			}
			if x.Status == domain.ExecSucceeded {
				return update(chat.ReviewApplied, x.ID, x.Summary)
			}
		}
	}
	if !matched {
		return update(chat.ReviewUnknown, "", "")
	}
	rec, err := s.Store.GetGraphRecord(ctx, p.Graph)
	if errors.Is(err, storage.ErrNotFound) || (err == nil && (rec.RunID != p.Run || rec.Version != p.Version || rec.State == domain.GraphStopped)) {
		return update(chat.ReviewUnknown, "", "")
	}
	if err != nil {
		return nil, err
	}
	return update(chat.ReviewQueued, "", "")
}
