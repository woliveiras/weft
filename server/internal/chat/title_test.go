package chat

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/llm"
)

func TestGenerateTitleReturnsOnlyTheSafeTitle(t *testing.T) {
	t.Parallel()
	const first = "Create a reusable reader for the release notes."
	title, err := GenerateTitle(t.Context(), first, func(_ context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
		if len(req.Tools) != 0 || len(req.Messages) != 1 || req.Messages[0].Content != first || !req.JSON || req.MaxTokens > 96 {
			t.Fatalf("unexpected title model request: %+v", req)
		}
		return answer(`{"title":"Release notes reader"}`), nil
	})
	if err != nil || title != "Release notes reader" {
		t.Fatalf("title=%q error=%v", title, err)
	}
	for _, body := range []string{
		`{"title":"One","title":"Two"}`, `{"title":"Release notes","kind":"agent.create"}`,
		`{"title":"See https://user:password@private.example"}`,
		`{"title":"Authorization: Bearer sample-secret-shaped-value"}`,
	} {
		_, err := GenerateTitle(t.Context(), first, func(context.Context, *llm.ChatRequest) (*llm.ChatResponse, error) {
			return answer(body), nil
		})
		if !errors.Is(err, ErrInvalidPlan) {
			t.Errorf("unsafe title response accepted: %v", err)
		}
	}
}

func TestRenameRejectsInvalidTitlesBeforeStorage(t *testing.T) {
	for _, value := range []string{" ", "Untitled chat", "Title\nwith a newline", "https://private.example", strings.Repeat("a", 121)} {
		if _, err := (Sessions{}).Rename(t.Context(), "owner", "id", "before", value); !errors.Is(err, ErrInvalidTitle) {
			t.Errorf("expected invalid title: %v", err)
		}
	}
}
