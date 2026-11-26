package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"regexp"
	"time"

	"github.com/woliveiras/weft/server/internal/agentcatalog"
	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
)

var reviewDigest = regexp.MustCompile(`^[0-9a-f]{64}$`)

func (s *Server) reviewChatAgent(ctx context.Context, actor Identity, sessionID string, plan chat.Plan) chat.Interpretation {
	out := chat.Interpretation{Plan: plan}
	if !actor.Role.Allows(config.RoleAdmin) {
		out.Text = "An administrator must create catalog agents."
		return out
	}
	settings, err := s.readChatModel(ctx)
	if err != nil || settings.Status != "configured" {
		out.Text, out.Retryable = "Select an available Chat model before preparing an agent definition.", true
		return out
	}
	m := agentcatalog.Manager{Store: s.Store}
	if _, err := m.Get(ctx, plan.Agent.ID); !errors.Is(err, storage.ErrNotFound) {
		if err == nil {
			out.Text = "That catalog agent ID is already in use. Provide another name for the new agent."
			out.Plan.Kind = chat.Clarify
		} else {
			out.Text, out.Retryable = "Could not inspect the catalog. Retry the saved request.", true
		}
		return out
	}
	definition, err := plan.Agent.Definition(domain.ModelRef{Provider: settings.Model.Provider, Name: settings.Model.Name})
	if err != nil || s.validateAgentDefinition(ctx, &definition) != nil {
		out.Text = "The proposed catalog definition is invalid. Describe the agent again or open Agents to prepare it manually."
		return out
	}
	review, err := chat.NewAgentReview(actor.chatOwner, sessionID, plan.Agent.ID, definition, settings.Revision, time.Now())
	if err != nil {
		out.Text, out.Retryable = "Could not prepare a reviewed agent definition. Retry the saved request.", true
		return out
	}
	out.Text, out.Review = "Review the catalog definition before creating the agent.", review
	return out
}

func (s *Server) decideChatAgent(w http.ResponseWriter, r *http.Request) error {
	actor, err := chatOwner(r)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256))
	decoder.DisallowUnknownFields()
	var input struct {
		Digest string `json:"digest"`
	}
	if err := decoder.Decode(&input); err != nil || decoder.Decode(new(any)) != io.EOF || !reviewDigest.MatchString(input.Digest) {
		return badRequest("provide the digest of the reviewed action")
	}
	ctx := r.Context()
	sessionID := r.PathValue("chatID")
	action := r.PathValue("decision")
	if action != "confirm" && action != "decline" {
		return storage.ErrNotFound
	}
	session, err := s.decideStoredChatAgent(ctx, actor, sessionID, r.PathValue("operationID"), input.Digest, action)
	if errors.Is(err, chat.ErrReviewMismatch) || errors.Is(err, chat.ErrExpiredReview) {
		return fmt.Errorf("%w: %s", storage.ErrConflict, err)
	}
	if err != nil {
		return chatError(err)
	}
	writeJSON(w, http.StatusOK, session)
	return nil
}

func (s *Server) decideStoredChatAgent(ctx context.Context, actor Identity, sessionID, reviewID, digest, action string) (*chat.Session, error) {
	store := s.chatStore()
	if action == "decline" {
		return store.DeclineAgentReview(ctx, actor.chatOwner, sessionID, reviewID, digest)
	}
	session, review, claimed, err := store.ClaimAgentReview(ctx, actor.chatOwner, sessionID, reviewID, digest)
	if err != nil {
		return nil, err
	}
	if claimed {
		// The claim has already been saved. Limit the local catalog operation
		// separately from the browser connection and reconcile a lost reply.
		settleCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		return s.applyReviewedChatAgent(settleCtx, actor, sessionID, review)
	}
	return s.reconcileAgentReviews(ctx, actor.chatOwner, session)
}

func (s *Server) decideChatAgentFromText(ctx context.Context, actor Identity, session *chat.Session, action string) (chat.Interpretation, error) {
	if !actor.Role.Allows(config.RoleAdmin) {
		return chat.Interpretation{Text: "An administrator must decide catalog agent creation."}, nil
	}
	review := session.SinglePendingAgentReview(time.Now())
	if review == nil {
		return chat.Interpretation{Text: "This request does not identify one active proposal. Use the action on the specific review or ask for a new review."}, nil
	}
	// The message route has viewer privileges for ordinary conversations.
	// Use the same administrative serialization as the button route.
	s.configurationMu.Lock()
	defer s.configurationMu.Unlock()
	_, err := s.decideStoredChatAgent(ctx, actor, session.ID, review.ID, review.Digest, action)
	if errors.Is(err, chat.ErrExpiredReview) {
		return chat.Interpretation{Text: "Confirmation expired. Ask for a new review."}, nil
	}
	if err != nil {
		return chat.Interpretation{}, err
	}
	return chat.Interpretation{Text: "The review card shows the recorded decision and catalog state."}, nil
}

func (s *Server) applyReviewedChatAgent(ctx context.Context, actor Identity, sessionID string, review *chat.AgentReview) (*chat.Session, error) {
	store := s.chatStore()
	settings, err := s.readChatModel(ctx)
	if err != nil {
		return store.FinishAgentReview(ctx, actor.chatOwner, sessionID, review.ID, chat.ReviewUnknown, 0)
	}
	if settings.Status != "configured" || settings.Revision != review.ModelRevision ||
		settings.Model.Provider != review.Definition.Model.Provider || settings.Model.Name != review.Definition.Model.Name {
		return store.FinishAgentReview(ctx, actor.chatOwner, sessionID, review.ID, chat.ReviewConflict, 0)
	}
	definition := review.Definition
	if err := s.validateAgentDefinition(ctx, &definition); err != nil || !reflect.DeepEqual(definition, review.Definition) {
		return store.FinishAgentReview(ctx, actor.chatOwner, sessionID, review.ID, chat.ReviewConflict, 0)
	}
	m := agentcatalog.Manager{Store: s.Store}
	item, err := m.CreateForOperation(ctx, review.AgentID, definition, actor.Name, review.ID)
	if err == nil {
		s.auditAgentPublication(ctx, item, actor.Name)
		return store.FinishAgentReview(ctx, actor.chatOwner, sessionID, review.ID, chat.ReviewCreated, item.Versions[0].Version)
	}
	if v, found, readErr := s.chatAgentReceipt(ctx, review); readErr == nil && found {
		return store.FinishAgentReview(ctx, actor.chatOwner, sessionID, review.ID, chat.ReviewCreated, v)
	}
	state := chat.ReviewUnknown
	if errors.Is(err, storage.ErrConflict) || errors.Is(err, agentcatalog.ErrInvalid) {
		state = chat.ReviewConflict
	}
	return store.FinishAgentReview(ctx, actor.chatOwner, sessionID, review.ID, state, 0)
}

func (s *Server) chatAgentReceipt(ctx context.Context, review *chat.AgentReview) (int, bool, error) {
	item, err := (agentcatalog.Manager{Store: s.Store}).Get(ctx, review.AgentID)
	if errors.Is(err, storage.ErrNotFound) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	version := item.Versions[0]
	return version.Version, version.Operation == review.ID, nil
}

func (s *Server) reconcileAgentReviews(ctx context.Context, owner string, session *chat.Session) (*chat.Session, error) {
	for _, message := range session.Messages {
		review := message.Review
		if review == nil || (review.State != chat.ReviewApplying && review.State != chat.ReviewUnknown) {
			continue
		}
		version, found, err := s.chatAgentReceipt(ctx, review)
		if err != nil {
			return nil, err
		}
		if found {
			session, err = s.chatStore().FinishAgentReview(ctx, owner, session.ID, review.ID, chat.ReviewCreated, version)
			if err != nil {
				return nil, err
			}
		} else if review.State == chat.ReviewApplying && time.Since(review.ClaimedAt) > 20*time.Second {
			session, err = s.chatStore().FinishAgentReview(ctx, owner, session.ID, review.ID, chat.ReviewUnknown, 0)
			if err != nil {
				return nil, err
			}
		}
	}
	return session, nil
}
