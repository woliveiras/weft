package api

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/chat"
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
	repos, err := s.prLookupTargets(ctx, selector)
	if err != nil {
		return manualPRTarget{}, err
	}
	if len(repos) != 1 || len(repos[0].reviewers) != 1 {
		return manualPRTarget{}, errPRScope
	}
	return repos[0].reviewers[0], nil
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

func (s *Server) dispatchChatPR(ctx context.Context, actor Identity, session string, r *chat.OperationReview) (*chat.Session, error) {
	p := *r.PR
	finish := func(state string) (*chat.Session, error) {
		return s.chatStore().ObservePR(ctx, actor.chatOwner, session, r.ID, r.State, state, "", "")
	}
	t, err := s.selectPRTarget(ctx, chat.PRSelector{Workflow: p.Graph, Connection: p.Connection, Number: strconv.Itoa(p.Number),
		Node: p.Node, WorkflowVersion: p.Version, Run: p.Run, ConnectionRevision: p.ConnectionRevision})
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
