package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/cost"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/taskconfig"
	"github.com/woliveiras/weft/server/internal/tools"
)

type chatSubmission struct {
	RequestID string `json:"requestId"`
	Text      string `json:"text"`
}

type chatSessionSummary struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	LastActivity time.Time `json:"lastActivity"`
	Running      int       `json:"running"`
	Unread       int       `json:"unread"`
	Success      int       `json:"unreadSuccess"`
	Fail         int       `json:"unreadFailure"`
	Cancel       int       `json:"unreadCancellation"`
	Decisions    int       `json:"decisions"`
	Unknown      int       `json:"unknown"`
}

var errChatIndexUnavailable = errors.New("Chat history is temporarily unavailable")

func (s *Server) chatOwner(r *http.Request) (Identity, error) {
	identity := identityFrom(r.Context())
	if identity.chatOwner == "" {
		return Identity{}, fmt.Errorf("%w: Chat needs an unambiguous installation identity", tools.ErrDenied)
	}
	if s.ChatIndexErr != nil {
		return Identity{}, errChatIndexUnavailable
	}
	return identity, nil
}

func chatError(err error) error {
	switch {
	case errors.Is(err, chat.ErrInvalidID), errors.Is(err, chat.ErrInvalidMessage), errors.Is(err, chat.ErrMessageLimit):
		return badRequest("%s", err)
	default:
		return err
	}
}

func (s *Server) chatStore() chat.Sessions {
	return chat.Sessions{Store: s.Store}
}

func (s *Server) listChats(w http.ResponseWriter, r *http.Request) error {
	actor, err := s.chatOwner(r)
	if err != nil {
		return err
	}
	archived := r.URL.Query().Get("archived") == "true"
	page, err := s.chatStore().List(r.Context(), actor.chatOwner, archived, r.URL.Query().Get("after"))
	if err != nil {
		return chatError(err)
	}
	items := make([]chatSessionSummary, 0, len(page.Items))
	now := time.Now()
	for _, summary := range page.Items {
		decisions := summary.Decisions
		for _, expiry := range summary.ReviewExpiries {
			if now.Before(expiry) {
				decisions++
			}
		}
		items = append(items, chatSessionSummary{
			ID: summary.ID, Title: summary.Title, LastActivity: summary.LastActivity, Running: summary.Running,
			Unread: summary.Unread, Success: summary.UnreadSuccess, Fail: summary.UnreadFailure, Cancel: summary.UnreadCancellation,
			Decisions: decisions, Unknown: summary.Unknown,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items, "shared": actor.chatOwner == sharedChatOwner, "next": page.Next, "archivedUnread": page.ArchivedUnread,
	})
	return nil
}

func (s *Server) getChat(w http.ResponseWriter, r *http.Request) error {
	actor, err := s.chatOwner(r)
	if err != nil {
		return err
	}
	session, err := s.chatStore().Reconcile(r.Context(), actor.chatOwner, r.PathValue("chatID"), s.observeChatTask)
	if err != nil {
		return chatError(err)
	}
	session, err = s.reconcileChatReviews(r.Context(), actor.chatOwner, session)
	if err != nil {
		return chatError(err)
	}
	writeChatSession(w, actor, session)
	return nil
}

func (s *Server) submitChat(w http.ResponseWriter, r *http.Request) error {
	actor, err := s.chatOwner(r)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 12<<10))
	decoder.DisallowUnknownFields()
	var input chatSubmission
	if err = decoder.Decode(&input); err != nil || decoder.Decode(new(any)) != io.EOF {
		return badRequest("expected one chat request")
	}
	store := s.chatStore()
	id := r.PathValue("chatID")
	if id == "" {
		id = input.RequestID
		_, _, err = store.Create(r.Context(), actor.chatOwner, id, input.Text)
	} else {
		_, err = store.Append(r.Context(), actor.chatOwner, id, input.RequestID, input.Text)
	}
	if err != nil {
		return chatError(err)
	}
	return s.interpretChat(w, r, actor, id, input.RequestID, false)
}

func (s *Server) retryChat(w http.ResponseWriter, r *http.Request) error {
	actor, err := s.chatOwner(r)
	if err != nil {
		return err
	}
	return s.interpretChat(w, r, actor, r.PathValue("chatID"), r.PathValue("requestID"), true)
}

func (s *Server) interpretChat(w http.ResponseWriter, r *http.Request, actor Identity, id, requestID string, retry bool) error {
	store := s.chatStore()
	session, claim, err := store.ClaimInterpretation(r.Context(), actor.chatOwner, id, requestID, retry)
	if err != nil {
		return chatError(err)
	}
	if !claim.IsZero() {
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		history := session.Context(requestID)
		var answer chat.Interpretation
		directive := ""
		if len(history) > 0 {
			directive = chat.ExplicitReviewDirective(history[len(history)-1].Content)
		}
		if directive != "" {
			answer, err = s.decideChatReviewFromText(ctx, actor, session, directive)
			if err != nil {
				answer = chat.Interpretation{Text: "The review decision could not be read. Inspect the existing review before trying again."}
			}
		} else {
			plan, modelErr := chat.Interpret(ctx, history, func(ctx context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
				return s.callChatModel(ctx, actor, req, "chat_interpretation")
			})
			if modelErr != nil {
				answer = chat.Interpretation{Text: "Chat could not interpret this request. Check the Chat model or budget, then retry the saved request.", Retryable: true}
			} else {
				answer = s.resolveChatPlan(ctx, actor, id, plan)
			}
		}
		saveCtx, finish := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
		defer finish()
		session, err = store.FinishInterpretation(saveCtx, actor.chatOwner, id, requestID, claim, answer)
		if err != nil {
			return chatError(err)
		}
	}
	writeChatSession(w, actor, session)
	return nil
}

func (s *Server) callChatModel(ctx context.Context, actor Identity, req *llm.ChatRequest, operation string) (*llm.ChatResponse, error) {
	settings, err := s.readChatModel(ctx)
	if err != nil || settings.Status != "configured" {
		return nil, chat.ErrModelUnavailable
	}
	select {
	case s.chatInference <- struct{}{}:
		defer func() { <-s.chatInference }()
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		return nil, tools.ErrRateLimited
	}
	req.Model = settings.Model.Name
	// Use a correlation reference distinct from task executions and transcript IDs.
	req.Metadata = map[string]string{"execution": domain.NewID("chat"), "operation": operation}
	provider := settings.Model.Provider
	typ := s.Models.Type(provider)
	prices := cost.Prices{Store: s.Store}
	budgets := cost.Budgets{Store: s.Store, Bus: s.Bus}
	reservation, err := budgets.Reserve(ctx, cost.Target{User: actor.Name, Execution: req.Metadata["execution"]},
		func(ctx context.Context) (cost.Catalogue, error) {
			catalogue, _, err := prices.Get(ctx)
			return catalogue, err
		}, provider, typ, req)
	if err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, errors.Join(ctx.Err(), reservation.Release(context.WithoutCancel(ctx)))
	}
	response, err := s.Models.ChatExplicit(ctx, provider, req)
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	if settleErr := reservation.Settle(saveCtx, response); settleErr != nil {
		return nil, settleErr
	}
	return response, err
}

func (s *Server) resolveChatPlan(ctx context.Context, actor Identity, sessionID string, plan chat.Plan) chat.Interpretation {
	reply := chat.Interpretation{Plan: plan}
	switch plan.Kind {
	case chat.AnalyzePR:
		return s.reviewChatPR(ctx, actor, sessionID, plan)
	case chat.EditInstructions:
		return s.reviewChatInstructions(ctx, actor, sessionID, plan)
	case chat.GuideNode:
		return s.reviewChatGuidance(ctx, actor, sessionID, plan)
	case chat.PlanInstallation:
		return s.reviewChatInstallation(ctx, actor, sessionID, plan)
	case chat.OpenManual:
		return chat.ManualHandoff(plan, actor.Role)
	case chat.Clarify:
		reply.Text = plan.Question
	case chat.Unsupported:
		reply.Text = "Chat can inspect or follow a task or propose a catalog agent or workflow. Use the console navigation for other operations."
	case chat.CreateAgent:
		return s.reviewChatAgent(ctx, actor, sessionID, plan)
	case chat.PublishAgent:
		return s.reviewChatCatalogPublication(ctx, actor, sessionID, plan)
	case chat.CreateWorkflow:
		return s.reviewChatWorkflow(ctx, actor, sessionID, plan)
	case chat.CreateTask:
		return s.reviewChatTaskDraft(ctx, actor, sessionID, plan)
	case chat.StartTask:
		return s.reviewChatTaskStart(ctx, actor, sessionID, plan)
	case chat.ControlWorkflow:
		return s.reviewChatWorkflowControl(ctx, actor, sessionID, plan)
	case chat.RetryTask:
		return s.reviewChatTaskRetry(ctx, actor, sessionID, plan)
	case chat.InspectTask, chat.WatchTask:
		id, err := s.resolveChatTask(ctx, plan.Task)
		if errors.Is(err, storage.ErrNotFound) {
			reply.Text = "Which task should I use? Enter its exact task ID or a unique task title."
			reply.Plan.Kind = chat.Clarify
			return reply
		}
		if err != nil {
			reply.Text, reply.Retryable = "Task discovery is unavailable. Retry the saved request to check again.", true
			return reply
		}
		snapshot, err := s.observeChatTask(ctx, id)
		if err != nil {
			reply.Text, reply.Retryable = "Task status is temporarily unknown. Retry the saved request to check again.", true
			return reply
		}
		reply.Task = &snapshot
		if plan.Kind == chat.WatchTask && !snapshot.Terminal() {
			reply.Text = fmt.Sprintf("Following task %s. Last observed state: %s.", snapshot.ID, snapshot.State)
		} else {
			reply.Text = fmt.Sprintf("Task %s: %s.", snapshot.ID, snapshot.State)
		}
	}
	return reply
}

func (s *Server) resolveChatTask(ctx context.Context, selector string) (string, error) {
	manager := taskconfig.Manager{Store: s.Store}
	if task, err := manager.Get(ctx, selector); err == nil {
		return task.ID, nil
	} else if !errors.Is(err, storage.ErrNotFound) {
		return "", err
	}
	page, err := manager.ListRecentlyUpdated(ctx, "", 100)
	if err != nil {
		return "", err
	}
	id := ""
	for _, task := range page.Items {
		if strings.EqualFold(task.Title, selector) {
			if id != "" || page.Next != "" {
				return "", storage.ErrNotFound
			}
			id = task.ID
		}
	}
	if id == "" {
		return "", storage.ErrNotFound
	}
	return id, nil
}

func (s *Server) observeChatTask(ctx context.Context, id string) (chat.TaskSummary, error) {
	view, err := s.taskRunner().View(ctx, id)
	if err != nil {
		return chat.TaskSummary{}, err
	}
	result := chat.TaskSummary{
		ID: id, State: view.State, GraphID: view.Task.ExecutionGraphID,
		DecisionNeeded: len(view.PendingApprovals) > 0,
		SettledBlock:   view.State == "blocked" && view.Task.Status == "blocked",
	}
	if view.Result != nil && view.Result.GraphID == result.GraphID {
		result.ExecutionID = view.Result.ID
	}
	return result, nil
}

func (s *Server) updateChat(w http.ResponseWriter, r *http.Request) error {
	actor, err := s.chatOwner(r)
	if err != nil {
		return err
	}
	id := r.PathValue("chatID")
	store := s.chatStore()
	var session *chat.Session
	if message := r.PathValue("messageID"); message != "" {
		session, err = store.MarkRead(r.Context(), actor.chatOwner, id, message)
	} else {
		switch r.PathValue("action") {
		case "archive", "restore":
			session, err = store.Archive(r.Context(), actor.chatOwner, id, r.PathValue("action") == "archive")
		default:
			return storage.ErrNotFound
		}
	}
	if err != nil {
		return chatError(err)
	}
	writeChatSession(w, actor, session)
	return nil
}

// RunChatWatches resumes persisted task observation without calling a model.
func (s *Server) RunChatWatches(ctx context.Context) {
	if s.ChatIndexErr != nil {
		return
	}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	cursor := ""
	for ctx.Err() == nil {
		next, err := s.chatStore().ReconcilePage(ctx, cursor, s.Auth.mayObserveChatOwner, s.observeChatTask, s.reconcileChatReviews)
		if err != nil && ctx.Err() == nil {
			s.Log.Warn("chat watch reconciliation failed", "error", err)
		}
		cursor = next
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
