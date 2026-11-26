package api

import (
	"context"
	"maps"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/installation"
)

func (s *Server) reviewChatInstallation(ctx context.Context, actor Identity, id string, proposal chat.Plan) chat.Interpretation {
	out := chat.Interpretation{Plan: proposal}
	if !actor.Role.Allows(config.RoleAdmin) {
		out.Text = "An administrator must review installation configuration."
		return out
	}
	docs, err := chat.ParseChatConfiguration(proposal.Document)
	if err != nil {
		out.Text = "Use the installation CLI or the existing setup form for this document. Enter credentials in the setup form."
		return out
	}
	snapshot, err := s.configSnapshot(ctx)
	if err != nil {
		out.Text, out.Retryable = "Could not read the installation configuration. Retry the saved request.", true
		return out
	}
	docs = hydrateChatProjectSettings(docs, snapshot)
	in := installation.Request{Revisions: map[string]string{}, RecordConsent: proposal.IntakeConsent}
	for _, doc := range docs {
		if doc.Revision() != "" {
			continue
		}
		old, err := configLookup(snapshot, doc)
		if err != nil {
			out.Text = "The installation object is ambiguous. Use its exported ID."
			return out
		}
		if old.Doc.Kind != "" {
			in.Revisions[doc.Key()] = old.Doc.Revision()
		}
	}
	if in.RecordConsent {
		hasIntake := false
		for _, doc := range docs {
			if doc.Kind == "IssueTrigger" || doc.Kind == "PullRequestReviewRoute" {
				hasIntake = true
			}
		}
		if !hasIntake {
			out.Text = "Intake consent applies only to issue triggers and pull request review routes."
			return out
		}
	}
	plan, _, _, err := s.buildConfigPlan(ctx, in, docs)
	if err != nil {
		out.Text, out.Retryable = "The installation plan is temporarily unavailable. Retry the saved request.", true
		return out
	}
	if !plan.OK {
		// Detailed diagnostics can include private installation inventory.
		out.Text = "The installation plan has conflicting revisions, invalid settings or unmet dependencies. Review the configuration or supply corrected settings, then request a fresh plan."
		return out
	}
	review, err := chat.NewConfigReview(actor.chatOwner, id, chat.ConfigReview{Document: proposal.Document, Objects: plan.Objects, Consent: in.RecordConsent}, time.Now())
	if err != nil {
		out.Text = "This configuration exceeds the chat preview or contains sensitive fields. Review it using the installation CLI."
		return out
	}
	out.Text, out.Review = "Review the installation changes before applying them.", review
	return out
}

func (s *Server) applyReviewedChatInstallation(ctx context.Context, actor Identity, id string, review *chat.OperationReview) (*chat.Session, error) {
	decision := review.Configuration
	docs, err := chat.ParseChatConfiguration(decision.Document)
	if err != nil {
		return s.chatStore().FinishReview(ctx, actor.chatOwner, id, review.ID, chat.ReviewConflict, 0)
	}
	for _, doc := range docs {
		if partialChatProject(doc) {
			snapshot, err := s.configSnapshot(ctx)
			if err != nil {
				return s.chatStore().FinishReview(ctx, actor.chatOwner, id, review.ID, chat.ReviewUnknown, 0)
			}
			docs = hydrateChatProjectSettings(docs, snapshot)
			break
		}
	}
	revisions := map[string]string{}
	for _, obj := range decision.Objects {
		revisions[obj.Key] = obj.Revision
	}
	in := installation.Request{Expected: revisions, Revisions: revisions, Confirmed: true, RecordConsent: decision.Consent}
	current, _, _, err := s.buildConfigPlan(ctx, in, docs)
	if err != nil {
		return s.chatStore().FinishReview(ctx, actor.chatOwner, id, review.ID, chat.ReviewUnknown, 0)
	}
	if !current.OK || installation.Digest(current.Objects) != installation.Digest(decision.Objects) {
		return s.chatStore().FinishReview(ctx, actor.chatOwner, id, review.ID, chat.ReviewConflict, 0)
	}
	plan, err := s.applyInstallationPlan(ctx, actor.Name, in, docs)
	if err != nil {
		return s.chatStore().FinishReview(ctx, actor.chatOwner, id, review.ID, chat.ReviewUnknown, 0)
	}
	receipts := make([]chat.ConfigReceipt, 0, len(plan.Objects))
	for _, obj := range plan.Objects {
		outcome := obj.Result
		if outcome == "failed" && strings.HasPrefix(obj.Error, "saved object") {
			outcome = "saved-revision-unknown"
		}
		receipts = append(receipts, chat.ConfigReceipt{Key: obj.Key, Result: outcome, Revision: obj.Revision})
	}
	return s.chatStore().FinishConfigReview(ctx, actor.chatOwner, id, review.ID, receipts, plan.OK)
}

func partialChatProject(doc installation.Document) bool {
	if doc.Kind != "Project" || len(doc.Spec) == 0 {
		return false
	}
	for key := range doc.Spec {
		if key != "executionMode" && key != "checks" {
			return false
		}
	}
	return true
}

// Existing projects can be identified by native ID without repeating source
// details or settings the operator did not ask to change. Keep hydration out
// of the persisted proposal. The plan and revision still gate confirmation.
func hydrateChatProjectSettings(docs []installation.Document, snapshot map[string]configObject) []installation.Document {
	for i, doc := range docs {
		if !partialChatProject(doc) {
			continue
		}
		old, ok := snapshot[doc.Key()]
		if !ok {
			continue
		}
		hydrated := maps.Clone(old.Doc.Spec)
		maps.Copy(hydrated, doc.Spec)
		doc.Spec = hydrated
		docs[i] = doc
	}
	return docs
}

// The owner may have changed role since a proposal was prepared. All routes
// returning a session use this projection, including read and title updates.
func writeChatSession(w http.ResponseWriter, actor Identity, session *chat.Session) {
	if !actor.Role.Allows(config.RoleAdmin) {
		projection := *session
		projection.Messages = slices.Clone(session.Messages)
		session = &projection
		for i := range session.Messages {
			message := &session.Messages[i]
			if message.Review != nil && message.Review.BudgetRaise != nil {
				message.Review = nil
				message.Text = "Administrator access is required to view this budget review."
			}
			if message.Review != nil && message.Review.PR != nil && !actor.Role.Allows(config.RoleOperator) {
				message.Review = nil
				message.Text = "Operator access is required to view this pull request review."
			}
			if message.Review != nil && message.Review.Environment != nil {
				message.Review = nil
				message.Text = "Administrator access is required to view this environment review."
			}
			if message.Review != nil && message.Review.Configuration != nil {
				message.Review = nil
				message.Text = "Administrator access is required to view this installation review."
			}
		}
	}
	writeJSON(w, http.StatusOK, session)
}
