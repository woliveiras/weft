package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/scm"
	"github.com/woliveiras/weft/server/internal/storage"
)

func (s *Server) pinnedChatPRTarget(ctx context.Context, p chat.PRAnalysis) (manualPRTarget, error) {
	target, err := s.selectPRTarget(ctx, chat.PRSelector{Workflow: p.Graph, Connection: p.Connection, Number: strconv.Itoa(p.Number),
		Node: p.Node, WorkflowVersion: p.Version, Run: p.Run, ConnectionRevision: p.ConnectionRevision})
	if err != nil {
		return manualPRTarget{}, err
	}
	scope := p
	scope.Head, scope.Base, scope.HeadRef, scope.BaseRef, scope.Draft = "", "", "", "", false
	if target.proposal != scope {
		return manualPRTarget{}, errPRScope
	}
	return target, nil
}

func sameChatPRRevision(p chat.PRAnalysis, snap scm.ReviewSnapshot) bool {
	return p.Number == snap.Number && p.Head == snap.HeadSHA && p.Base == snap.BaseSHA &&
		p.HeadRef == snap.Head && p.BaseRef == snap.Base && p.Draft == snap.Draft && snap.State == "open"
}

func (s *Server) pinnedChatPRDiff(ctx context.Context, actor Identity, p chat.PRAnalysis) (string, error) {
	target, err := s.pinnedChatPRTarget(ctx, p)
	if err != nil {
		return "", err
	}
	snapshot, diff, err := s.readChatPR(ctx, actor, target, true)
	if err != nil || !sameChatPRRevision(p, snapshot) || strings.TrimSpace(diff) == "" || !chat.SafeReviewContent(diff) {
		return "", errPRRead
	}
	return diff, nil
}

func (s *Server) inspectChatPRDiff(w http.ResponseWriter, r *http.Request) error {
	actor, err := s.chatOwner(r)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	session, err := s.chatStore().Get(ctx, actor.chatOwner, r.PathValue("chatID"))
	if err != nil {
		return chatError(err)
	}
	for _, message := range session.Messages {
		review := message.Review
		if review == nil || review.PR == nil || review.ID != r.PathValue("operationID") {
			continue
		}
		p := *review.PR
		diff, err := s.pinnedChatPRDiff(ctx, actor, p)
		if errors.Is(err, errPRScope) || errors.Is(err, errPRRead) {
			return fmt.Errorf("%w: the pinned diff is unavailable under the current scope or revision; request a fresh review", storage.ErrConflict)
		}
		if err != nil {
			return err
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, map[string]string{"head": p.Head, "base": p.Base, "diff": diff})
		return nil
	}
	return storage.ErrNotFound
}
