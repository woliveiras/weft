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
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/storage"
)

func (s *Server) retryChatTitle(w http.ResponseWriter, r *http.Request) error {
	actor, err := s.chatOwner(r)
	if err != nil {
		return err
	}
	ctx := r.Context()
	id := r.PathValue("chatID")
	store := s.chatStore()
	current, err := store.Get(ctx, actor.chatOwner, id)
	if err != nil {
		return chatError(err)
	}
	if current.TitleRetryState != "" {
		writeChatSession(w, actor, current)
		return nil
	}
	settings, err := s.readChatModel(ctx)
	if err != nil {
		return err
	}
	if settings.Status != "configured" {
		return fmt.Errorf("%w: select an available Chat model before retrying the title", storage.ErrConflict)
	}
	session, claimed, err := store.ClaimTitleRetry(ctx, actor.chatOwner, id)
	if errors.Is(err, chat.ErrTitleRetryUnavailable) {
		return fmt.Errorf("%w: %s", storage.ErrConflict, err)
	}
	if err != nil {
		return chatError(err)
	}
	if claimed {
		modelCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		title, err := chat.GenerateTitle(modelCtx, session.Messages[0].Text, func(ctx context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
			return s.callChatModel(ctx, actor, req, "chat_title")
		})
		if err != nil {
			title = ""
		}
		saveCtx, finish := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer finish()
		session, err = store.FinishTitleRetry(saveCtx, actor.chatOwner, id, title)
		if err != nil {
			return chatError(err)
		}
	}
	writeChatSession(w, actor, session)
	return nil
}

func (s *Server) renameChat(w http.ResponseWriter, r *http.Request) error {
	actor, err := s.chatOwner(r)
	if err != nil {
		return err
	}
	var input struct {
		Title    string  `json:"title"`
		Previous *string `json:"previousTitle"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF || input.Previous == nil {
		return badRequest("provide the new and previous conversation titles")
	}
	session, err := s.chatStore().Rename(r.Context(), actor.chatOwner, r.PathValue("chatID"), *input.Previous, input.Title)
	if errors.Is(err, chat.ErrInvalidTitle) {
		return badRequest("%s", err)
	}
	if err != nil {
		return chatError(err)
	}
	writeChatSession(w, actor, session)
	return nil
}
