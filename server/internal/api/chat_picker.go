package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/storage"
)

func (s *Server) chatCandidates(w http.ResponseWriter, r *http.Request) error {
	actor, err := s.chatOwner(r)
	if err != nil {
		return err
	}
	area, query := r.URL.Query().Get("area"), r.URL.Query().Get("q")
	if !chat.PickerArea(area) || len(query) > 320 || (query != "" && chat.ValidateMessage(query) != nil) || strings.ContainsAny(query, "\r\n\t") {
		return badRequest("choose a supported resource and a shorter single-line search")
	}
	result, err := s.searchChatResources(r.Context(), actor, discoverySlot{area: area}, query)
	if err != nil {
		return err
	}
	type item struct {
		ID    string `json:"id"`
		Label string `json:"label"`
	}
	items := make([]item, 0, len(result.Choices))
	for _, c := range result.Choices {
		items = append(items, item{c.ID, c.Label})
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "partial": result.Incomplete()})
	return nil
}

func (s *Server) exactPickerReply(ctx context.Context, actor Identity, plan chat.Plan) chat.Interpretation {
	unavailable := chat.Interpretation{Plan: chat.Plan{Kind: chat.Clarify, Title: plan.Title}, Text: "This selection is unavailable. Choose a resource again."}
	switch plan.Kind {
	case chat.WatchTask:
		task, err := s.observeChatTask(ctx, plan.Task)
		if errors.Is(err, storage.ErrNotFound) {
			return unavailable
		}
		if err != nil {
			return discoveryUnavailable(plan)
		}
		return observedTaskReply(plan, task)
	case chat.FindResource:
		choice, err := s.chatPickerChoice(ctx, actor, plan.Area, plan.Query)
		if errors.Is(err, storage.ErrNotFound) {
			return unavailable
		}
		if err != nil {
			return discoveryUnavailable(plan)
		}
		return foundResource(plan, plan.Area, choice)
	}
	return unavailable
}

func (s *Server) chatPickerChoice(ctx context.Context, actor Identity, area, id string) (chat.Choice, error) {
	matches, err := s.searchChatResources(ctx, actor, discoverySlot{area: area}, id)
	if err != nil {
		return chat.Choice{}, err
	}
	for _, choice := range matches.Choices {
		if choice.ID == id {
			return choice, nil
		}
	}
	return chat.Choice{}, storage.ErrNotFound
}

// Reject a stale draft before its first turn is saved. A retry of an already
// accepted request is reconciled by Sessions.Create against its original text.
func (s *Server) validatePickerDraft(ctx context.Context, actor Identity, input chatSubmission) error {
	plan, ok := chat.PickerCommand(input.Text)
	if !ok {
		return nil
	}
	if plan.Kind == chat.Clarify {
		return badRequest("choose a valid action and resource ID")
	}
	id := plan.Query
	if plan.Kind == chat.WatchTask {
		id = plan.Task
	}
	_, err := s.chatPickerChoice(ctx, actor, plan.Area, id)
	if err == nil {
		return nil
	}
	if _, savedErr := s.chatStore().Get(ctx, actor.chatOwner, input.RequestID); savedErr == nil {
		return nil
	} else if !errors.Is(savedErr, storage.ErrNotFound) {
		return chatError(savedErr)
	}
	if errors.Is(err, storage.ErrNotFound) {
		return badRequest("This selection is unavailable. Change or clear it before sending.")
	}
	return err
}
