package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/engine"
	"github.com/woliveiras/weft/server/internal/scm"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/tools"
)

const chatPRPublicationTimeout = 30 * time.Second

func (s *Server) chatPRCommentGrant(ctx context.Context, p chat.PRAnalysis) (domain.ToolGrant, error) {
	denied := fmt.Errorf("%w: the reviewed workflow version needs saved SCM read and line comment grants for this connection", tools.ErrDenied)
	if p.Provider != "github" || p.Draft || s.Gateway == nil {
		return domain.ToolGrant{}, fmt.Errorf("%w: this provider or draft PR does not support reviewed line comment publication", storage.ErrConflict)
	}
	g, record, err := s.Engine.GetGraph(ctx, p.Graph, 0)
	if errors.Is(err, storage.ErrNotFound) {
		return domain.ToolGrant{}, denied
	}
	if err != nil {
		return domain.ToolGrant{}, err
	}
	node := g.Node(p.Node)
	if g.Metadata.Version != p.Version || record.Version != p.Version || node == nil || node.Disabled {
		return domain.ToolGrant{}, denied
	}
	if _, ok := savedPRReadGrant(node.Tools, p.Connection); !ok {
		return domain.ToolGrant{}, denied
	}
	for _, grant := range node.Tools {
		if grant.Name == scm.CapReviewComment && tools.CheckRepository(grant, p.Connection) == nil && !grant.RequiresApproval {
			return grant, nil
		}
	}
	return domain.ToolGrant{}, denied
}

func chatReviewByID(session *chat.Session, id string) *chat.OperationReview {
	for _, m := range session.Messages {
		if m.Review != nil && m.Review.ID == id {
			return m.Review
		}
	}
	return nil
}

func (s *Server) prepareChatPRComments(w http.ResponseWriter, r *http.Request) error {
	actor, err := s.chatOwner(r)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 512))
	decoder.DisallowUnknownFields()
	var body struct {
		CommentIDs []int `json:"commentIds"`
	}
	if decoder.Decode(&body) != nil || decoder.Decode(new(any)) != io.EOF || len(body.CommentIDs) == 0 || len(body.CommentIDs) > scm.MaxReviewComments {
		return badRequest("select between one and six draft comments")
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	session, err := s.chatStore().Get(ctx, actor.chatOwner, r.PathValue("chatID"))
	if err != nil {
		return chatError(err)
	}
	rv := chatReviewByID(session, r.PathValue("operationID"))
	if rv == nil || rv.PR == nil {
		return storage.ErrNotFound
	}
	p, err := rv.SelectPRComments(body.CommentIDs)
	if err != nil {
		return badRequest("select only distinct comments from the completed draft")
	}
	if _, err = s.chatPRCommentGrant(ctx, p.Analysis); err != nil {
		return err
	}
	diff, err := s.pinnedChatPRDiff(ctx, actor, p.Analysis)
	if err != nil || !chat.AnchorsPRComments(diff, p.Comments) {
		return fmt.Errorf("%w: the PR changed or a selected comment is not on a pinned changed line", storage.ErrConflict)
	}
	session, err = s.chatStore().AddPRCommentReview(ctx, actor.chatOwner, session.ID, p, time.Now())
	if err != nil {
		return chatError(err)
	}
	writeChatSession(w, actor, session)
	return nil
}

func (s *Server) applyChatPRComments(ctx context.Context, actor Identity, sessionID string, r *chat.OperationReview) (*chat.Session, error) {
	p := r.PRPublication
	var result *chat.Session
	err := s.Engine.WithGraphTransition(ctx, p.Analysis.Graph, func(ctx context.Context) error {
		grant, err := s.chatPRCommentGrant(ctx, p.Analysis)
		if err == nil {
			var diff string
			diff, err = s.pinnedChatPRDiff(ctx, actor, p.Analysis)
			if err == nil && !chat.AnchorsPRComments(diff, p.Comments) {
				err = errPRRead
			}
		}
		if err != nil {
			result, err = s.finishChatPRComments(ctx, actor, sessionID, r, chat.ReviewConflict, chatPRCommentResults(len(p.Comments), "failed", "source_or_grant_changed"))
		} else {
			result, err = s.runChatPRComments(ctx, actor, sessionID, r, grant, false)
		}
		return err
	})
	if errors.Is(err, engine.ErrGraphTransitionBusy) {
		return s.finishChatPRComments(ctx, actor, sessionID, r, chat.ReviewConflict, chatPRCommentResults(len(p.Comments), "failed", "source_or_grant_changed"))
	}
	return result, err
}

func (s *Server) reconcileChatPRComments(w http.ResponseWriter, r *http.Request) error {
	actor, err := s.chatOwner(r)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(r.Context(), 16*time.Second)
	defer cancel()
	session, err := s.chatStore().Get(ctx, actor.chatOwner, r.PathValue("chatID"))
	if err != nil {
		return chatError(err)
	}
	rv := chatReviewByID(session, r.PathValue("operationID"))
	if rv == nil || rv.PRPublication == nil {
		return storage.ErrNotFound
	}
	if rv.State == chat.ReviewApplying && time.Since(rv.ClaimedAt) <= chatPRPublicationTimeout+3*time.Second {
		return fmt.Errorf("%w: publication is still settling", storage.ErrConflict)
	}
	if rv.State == chat.ReviewUnknown || rv.State == chat.ReviewIncomplete || rv.State == chat.ReviewApplying {
		grant, err := s.chatPRCommentGrant(ctx, rv.PRPublication.Analysis)
		if err != nil {
			return err
		}
		session, err = s.runChatPRComments(ctx, actor, session.ID, rv, grant, true)
		if err != nil {
			return chatError(err)
		}
	}
	writeChatSession(w, actor, session)
	return nil
}

func chatPRCommentResults(n int, state, code string) []scm.ReviewCommentResult {
	results := make([]scm.ReviewCommentResult, n)
	for i := range results {
		results[i] = scm.ReviewCommentResult{State: state, Code: code}
	}
	return results
}

func (s *Server) runChatPRComments(ctx context.Context, actor Identity, sessionID string, review *chat.OperationReview, grant domain.ToolGrant, reconcile bool) (*chat.Session, error) {
	p := review.PRPublication
	a := p.Analysis
	principal := &tools.Principal{Actor: actor.Name, GraphID: a.Graph, GraphVersion: a.Version, NodeID: a.Node, WorkspaceReadOnly: true, Grants: []domain.ToolGrant{grant}}
	response, err := scm.InvokeReviewedComments(ctx, s.Gateway, principal, p.Request(reconcile))
	var results []scm.ReviewCommentResult
	if errors.Is(err, tools.ErrDenied) || errors.Is(err, tools.ErrRateLimited) {
		if reconcile {
			return nil, err
		}
		results = chatPRCommentResults(len(p.Comments), "failed", "tool_denied")
	} else if err != nil || response == nil || response.IsError || response.Truncated || json.Unmarshal(response.Data, &results) != nil || len(results) != len(p.Comments) {
		results = chatPRCommentResults(len(p.Comments), "unknown", "receipt_unconfirmed")
	}
	return s.finishChatPRComments(ctx, actor, sessionID, review, "", results)
}

func (s *Server) finishChatPRComments(ctx context.Context, actor Identity, sessionID string, review *chat.OperationReview, state string, results []scm.ReviewCommentResult) (*chat.Session, error) {
	save, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	return s.chatStore().ObservePRCommentReview(save, actor.chatOwner, sessionID, review.ID, state, results)
}
