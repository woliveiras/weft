package trigger

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/observability"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/tools"
)

const maxSnapshotEdits = 3

const readinessCapacity = "The trigger has 10 held issues. Resolve a hold before it evaluates new issues."

type Hold struct {
	Generation         string     `json:"generation"`
	CommentID          string     `json:"commentId,omitempty"`
	CommentAuthor      string     `json:"commentAuthor,omitempty"`
	UndoOwner          string     `json:"undoOwner,omitempty"`
	PublicationMessage string     `json:"publicationMessage,omitempty"`
	LabelAttempts      int        `json:"labelAttempts,omitempty"`
	RetryLabelAt       *time.Time `json:"retryLabelAt,omitempty"`
	IdentityFailures   int        `json:"identityFailures,omitempty"`
	CommentAttempts    int        `json:"commentAttempts,omitempty"`
	RetryCommentAt     *time.Time `json:"retryCommentAt,omitempty"`

	RetryFailures   int                  `json:"retryFailures,omitempty"`
	RetryAccessAt   *time.Time           `json:"retryAccessAt,omitempty"`
	RetryAt         *time.Time           `json:"retryAt,omitempty"`
	ScopeAssessment *ReadinessAssessment `json:"scopeAssessment,omitempty"`
	ScopeCheckedAt  time.Time            `json:"scopeCheckedAt,omitzero"`
	ScopeLagPolls   int                  `json:"scopeLagPolls,omitempty"`
	MissingSince    time.Time            `json:"missingSince,omitzero"`
	MissingPoll     uint64               `json:"missingPoll,omitempty"`
	ScopeMiss       string               `json:"scopeMiss,omitempty"`
	SnapshotEdits   int                  `json:"snapshotEdits,omitempty"`
	Eligibility     string               `json:"eligibility,omitempty"`
	Message         string               `json:"message"`
	Key             string               `json:"key"`
	URL             string               `json:"url,omitempty"`
	Reason          string               `json:"reason"`
	State           string               `json:"state"`
	Round           int                  `json:"round"`
	OpenedAt        time.Time            `json:"openedAt"`
	RoundAt         time.Time            `json:"roundAt"`
	Publication     string               `json:"publication,omitempty"`
	Scope           string               `json:"scope"`
	Evaluated       string               `json:"evaluated,omitempty"`
	Signal          string               `json:"signal,omitempty"`
	Questions       []string             `json:"questions,omitempty"`
	Marker          string               `json:"marker,omitempty"`
	Intervention    bool                 `json:"intervention,omitempty"`
	Removals        []string             `json:"removals,omitempty"`
}

// Legacy holds need an identifier even while readiness is disabled and the
// runner cannot update them. Once stored, generations rotate with the round.
func (t *Trigger) fillHoldGenerations() {
	for i := range t.Holds {
		h := &t.Holds[i]
		if h.Generation == "" {
			h.Generation = triggerFingerprint([]any{t.ID, h.Key, h.OpenedAt, h.Round, h.Marker})
		}
	}
}

func nextHoldGeneration(previous, current Hold) string {
	if previous.Generation == "" || previous.Round != current.Round || previous.SnapshotEdits != current.SnapshotEdits || previous.Marker != current.Marker {
		return domain.NewID("holdgen")
	}
	return previous.Generation
}

func (t *Trigger) holdIndex(key string) int {
	return slices.IndexFunc(t.Holds, func(h Hold) bool { return h.Key == key })
}

func holdScope(t *Trigger) string {
	return triggerFingerprint(struct {
		Source   SourceConfig
		Workflow string
	}{t.Source, t.Workflow})
}

const readinessMarkerPrefix = "Weft readiness reference: "

func holdMessage(h Hold) string {
	var lines []string
	switch h.State {
	case "waiting":
		lines = []string{fmt.Sprintf("Weft question (round %d of %d). Please reply on this issue with the information below.", h.Round, maxReadinessRounds)}
		for _, code := range h.Questions {
			lines = append(lines, "- "+readinessQuestions[code])
		}
	case "operator":
		if h.Reason == "snapshot_round_limit" {
			lines = []string{"Weft readiness failure. The issue changed repeatedly during readiness checks and reached the three-edit limit. An operator should review the current issue and resolve the hold before it is selected again."}
			break
		}
		if h.Reason == "eligibility_edit_limit" {
			lines = []string{"Weft readiness failure. An eligibility change reached the three-edit limit for readiness checks. An operator should review eligibility and resolve the hold before the issue is selected again."}
			break
		}
		if h.Reason == "failure_round_limit" {
			lines = []string{"Weft readiness failure. The two readiness rounds are exhausted. An operator should review the evaluation failure and resolve the hold before a maintainer selects the issue again."}
			break
		}
		lines = []string{"Weft readiness failure. Two rounds of questions did not resolve the missing requirements. An operator should review the issue. After clarifying it, a maintainer can add weft:todo to request another assessment."}
	default:
		lines = []string{"Weft readiness failure. " + ReadinessReason(h.Reason) + " Review the trigger and the issue, then have a maintainer add weft:todo to request another assessment."}
	}
	return strings.Join(lines, "\n") + "\n\n" + readinessMarkerPrefix + h.Marker
}

func ReadinessReason(reason string) string {
	switch reason {
	case "incomplete":
		return "The issue is waiting for clarification."
	case "failure_round_limit":
		return "Readiness failed and exhausted the two-round allowance. Review the evaluator and resolve the hold before selecting the issue again."
	case "round_limit":
		return "The issue needs operator intervention after two question rounds."
	case "history_limit":
		return "The tracker comment or label history exceeded the readiness scan limit. Review the issue and resolve the hold before selecting it again."
	case "input_limit":
		return "The issue and replies exceed the 16 KiB evaluation limit."
	case "output_limit":
		return "The model reached the 512-token output limit."
	case "monetary_limit":
		return "The readiness monetary limit prevented a verdict."
	case "timeout":
		return "The model could not return a verdict within 10 seconds."
	case "invalid_verdict":
		return "The model returned an invalid readiness verdict."
	case "pricing_unavailable":
		return "Readiness pricing or budget information is unavailable."
	case "accounting_unavailable":
		return "The readiness call could not be accounted for."
	case "model_unavailable":
		return "The reviewed readiness model is unavailable."
	case "tracker_unavailable":
		return "The tracker could not verify the current issue or its responses."
	case "snapshot_round_limit":
		return "The issue changed repeatedly during readiness checks and reached the three-edit limit. Review the current issue and resolve the hold before selecting it again."
	case "eligibility_edit_limit":
		return "An eligibility change reached the three-edit limit for readiness checks. Review eligibility and resolve the hold."
	case "snapshot_changed":
		return "The issue changed during readiness evaluation."
	case "claim_busy":
		return "Issue admission is temporarily unavailable. Weft will retry after backoff."
	case "claim_recheck_unavailable":
		return "The tracker could not recheck the issue after claim. Weft will retry after rollback and backoff."
	case "eligibility_changed":
		return "The tracker state changed during readiness checks. Weft will verify eligibility again."
	case "evaluation_interrupted":
		return "Readiness evaluation was interrupted."
	case "scope_assessment_pending":
		return "The readiness assessment is waiting for GitHub to verify the trigger query before publication."
	case "scope_waiting":
		return "The issue passed readiness and is waiting for GitHub to verify the trigger query."
	case "scope_changed":
		return "This hold belongs to a different tracker or workflow scope."
	default:
		return "Readiness could not be confirmed."
	}
}

func (h Hold) Description() string {
	if h.State == "retry_failed" {
		return "Readiness retries stopped because " + retryFailureCause(h.Reason) + " Review the issue and resolve the hold."
	}
	return ReadinessReason(h.Reason)
}

func (r *Runner) saveHold(ctx context.Context, t *Trigger, h Hold) error {
	return r.saveHoldState(ctx, t, h, false)
}

func (r *Runner) saveHoldState(ctx context.Context, t *Trigger, h Hold, rollback bool) error {
	h.Message = h.Description()
	outcome := holdFailureOutcome(h)
	transition := false
	current, err := r.Triggers.Update(ctx, t.ID, "system:trigger", func(current *Trigger) error {
		if current.State != t.State || current.ReadinessDisabled != t.ReadinessDisabled || current.Consent.Scope != t.Consent.Scope || current.Consent.RecordedAt != t.Consent.RecordedAt {
			return errTriggerChanged
		}
		if rollback {
			if current.Claiming != h.Key {
				return errTriggerChanged
			}
			current.Claiming, current.ClaimAdmission = "", ""
		}
		idx := current.holdIndex(h.Key)
		transition = outcome != "" && (idx < 0 || holdFailureOutcome(current.Holds[idx]) != outcome)
		if idx >= 0 {
			h.Generation = nextHoldGeneration(current.Holds[idx], h)
			current.Holds[idx] = h
		} else {
			if len(current.Holds) >= maxReadinessHolds {
				return storage.ErrConflict
			}
			h.Generation = nextHoldGeneration(Hold{}, h)
			current.Holds = append(current.Holds, h)
			delete(current.RetainedReadiness, readinessProgressKey(current, h.Key))
		}
		if len(current.Holds) >= maxReadinessHolds {
			current.ReadinessBlock = readinessCapacity
		}
		return nil
	})
	if err == nil {
		t.Holds, t.ReadinessBlock, t.RetainedReadiness = current.Holds, current.ReadinessBlock, current.RetainedReadiness
		if transition {
			observability.TriggerReadinessFailures.WithLabelValues(outcome).Inc()
			r.log().WarnContext(ctx, "issue readiness failed", "trigger", t.ID, "outcome", outcome)
		}
	}
	return err
}

func holdFailureOutcome(h Hold) string {
	switch h.Publication {
	case "label_failed":
		return "label_failed"
	case "comment_failed":
		return "comment_failed"
	}
	if h.State == "retry_failed" {
		return "retry_failed"
	}
	return ""
}

func trackerRead[T any](ctx context.Context, call Invoker, name string, args trackerArgs) (T, error) {
	var out T
	res, err := call(ctx, name, args)
	if err == nil && res != nil && res.IsError && (name == trackerComments || name == trackerRequeue) &&
		bytes.Equal(res.Data, trackerHistoryLimitResult) {
		return out, errTrackerHistoryLimit
	}
	if failure := trackerResultFailure(res, err); failure != nil {
		return out, failure
	}
	err = decode(res, err, &out)
	return out, err
}

func trackerWrite(ctx context.Context, call Invoker, name string, args trackerArgs) error {
	res, err := call(ctx, name, args)
	if failure := trackerResultFailure(res, err); failure != nil {
		return failure
	}
	return result(res, err)
}

func relabel(ctx context.Context, call Invoker, key string, remove []string, add, owner string) error {
	return trackerWrite(ctx, call, trackerLabels, trackerArgs{Key: key, Remove: remove, Add: add, Owner: owner})
}

func comment(ctx context.Context, call Invoker, key, body string) error {
	_, err := writeComment(ctx, call, key, body)
	return err
}

func writeComment(ctx context.Context, call Invoker, key, body string) (commentReceipt, error) {
	res, err := call(ctx, trackerComment, trackerArgs{Key: key, Body: body})
	if failure := trackerResultFailure(res, err); failure != nil {
		return commentReceipt{}, failure
	}
	var receipt commentReceipt
	err = decode(res, err, &receipt)
	return receipt, err
}

func trackerResultFailure(res *tools.Result, err error) error {
	if err == nil && res != nil && res.IsError {
		var failure trackerHTTPFailure
		if json.Unmarshal(res.Data, &failure) == nil && failure.Status >= 400 && failure.Status <= 599 {
			return failure
		}
	}
	return nil
}

const maxCommentAttempts = 3

const maxLabelAttempts = 3

func (r *Runner) failHoldLabels(ctx context.Context, t *Trigger, h Hold, message string) error {
	h.Publication, h.PublicationMessage, h.RetryLabelAt = "label_failed", message+" "+abandonTrackerWriteWarning, nil
	return r.saveHold(ctx, t, h)
}

func (r *Runner) publishHoldLabels(ctx context.Context, t *Trigger, call Invoker, h Hold) error {
	now := r.Triggers.now()
	if h.RetryLabelAt != nil && now.Before(*h.RetryLabelAt) {
		return nil
	}
	const exhausted = "The tracker label update could not be confirmed after three attempts."
	if h.LabelAttempts >= maxLabelAttempts {
		return r.failHoldLabels(ctx, t, h, exhausted)
	}
	h.LabelAttempts++
	next := now.Add(time.Minute << (h.LabelAttempts - 1))
	h.RetryLabelAt = &next
	h.PublicationMessage = "The tracker label update is unconfirmed. Weft will retry after backoff."
	if err := r.saveHold(ctx, t, h); err != nil {
		return err
	}
	err := trackerWrite(ctx, call, trackerLabels, trackerArgs{Key: h.Key, Remove: h.Removals, Add: LabelFailed, UndoOwner: h.UndoOwner})
	if errors.Is(err, errTriggerChanged) {
		return err
	}
	if err != nil {
		var failure trackerHTTPFailure
		if errors.As(err, &failure) && failure.definitive() {
			return r.failHoldLabels(ctx, t, h, fmt.Sprintf("The tracker refused the label or assignment update (HTTP %d).", failure.Status))
		}
		if h.LabelAttempts >= maxLabelAttempts {
			return r.failHoldLabels(ctx, t, h, exhausted)
		}
		if deadline := trackerRetryDeadline(r.Triggers.now(), err); deadline.After(next) {
			h.RetryLabelAt = &deadline
			return r.saveHold(ctx, t, h)
		}
		return nil
	}
	h.UndoOwner, h.LabelAttempts, h.RetryLabelAt, h.PublicationMessage = "", 0, nil, ""
	if h.Publication == "label_pending_only" {
		return r.confirmHoldComment(ctx, t, h)
	}
	h.Publication = "label_applied"
	if err := r.saveHold(ctx, t, h); err != nil {
		return err
	}
	if t.State != Active || t.ReadinessDisabled {
		return nil
	}
	return r.postHoldComment(ctx, t, call, h)
}

func (r *Runner) postHoldComment(ctx context.Context, t *Trigger, call Invoker, h Hold) error {
	now := r.Triggers.now()
	if h.RetryCommentAt != nil && now.Before(*h.RetryCommentAt) {
		return nil
	}
	author, err := trackerRead[string](ctx, call, trackerIdentity, trackerArgs{Key: h.Key})
	if errors.Is(err, errTriggerChanged) {
		return err
	}
	if err != nil || author == "" {
		h.IdentityFailures++
		if isDefinitiveTrackerFailure(err) || h.IdentityFailures > maxReadinessRetries {
			return r.failHoldComment(ctx, t, h, "The tracker comment author could not be verified. Check the connection and resolve the hold.")
		}
		h.RetryCommentAt = readinessRetryAt(r.Triggers.now(), h.IdentityFailures, err)
		h.PublicationMessage = "The tracker comment author could not be verified. Weft will retry the lookup after backoff."
		return r.saveHold(ctx, t, h)
	}
	if h.CommentAuthor != "" && h.CommentAuthor != author {
		return r.failHoldComment(ctx, t, h, "The tracker comment author changed. Review the issue and resolve the hold.")
	}
	h.CommentAuthor, h.IdentityFailures, h.PublicationMessage = author, 0, ""
	h.CommentAttempts++
	if h.RoundAt.IsZero() {
		h.RoundAt = now
	}
	next := now.Add(time.Minute << (h.CommentAttempts - 1))
	h.Publication, h.RetryCommentAt = "comment_pending", &next
	if err := r.saveHold(ctx, t, h); err != nil {
		return err
	}
	receipt, err := writeComment(ctx, call, h.Key, holdMessage(h))
	if err == nil && receipt.ID != "" {
		h.CommentID = receipt.ID
		if receipt.AuthorID == h.CommentAuthor {
			h.RoundAt = now
			if !receipt.At.IsZero() {
				h.RoundAt = receipt.At
			}
			return r.confirmHoldComment(ctx, t, h)
		}
		return r.saveHold(ctx, t, h)
	}
	var failure trackerHTTPFailure
	if errors.As(err, &failure) && failure.definitive() {
		return r.failHoldComment(ctx, t, h, failure.guidance())
	}
	if deadline := trackerRetryDeadline(r.Triggers.now(), err); deadline.After(next) {
		h.RetryCommentAt = &deadline
		return r.saveHold(ctx, t, h)
	}
	return errors.New("readiness comment delivery is unconfirmed")
}

func (r *Runner) confirmHoldComment(ctx context.Context, t *Trigger, h Hold) error {
	h.Publication, h.PublicationMessage, h.RetryCommentAt = "confirmed", "", nil
	return r.saveHold(ctx, t, h)
}

func (r *Runner) failHoldComment(ctx context.Context, t *Trigger, h Hold, message string) error {
	h.Publication, h.PublicationMessage, h.RetryCommentAt = "comment_failed", message, nil
	return r.saveHold(ctx, t, h)
}

// Every retry is preceded by a successful scan for the original marker. The
// trigger lease serializes the scan and POST with all other pollers.
func (r *Runner) publishHold(ctx context.Context, t *Trigger, call Invoker, h Hold) error {
	switch h.Publication {
	case "label_pending", "label_pending_only":
		return r.publishHoldLabels(ctx, t, call, h)
	case "label_applied":
		return r.postHoldComment(ctx, t, call, h)
	case "comment_pending":
		if h.RetryCommentAt != nil && r.Triggers.now().Before(*h.RetryCommentAt) {
			return nil
		}
		if h.CommentAuthor == "" {
			return r.failHoldComment(ctx, t, h, "The original tracker comment author is unknown. Review the issue and resolve the hold.")
		}
		// The tracker clock can lag the runner. Do not exclude a published
		// comment from reconciliation based on its timestamp.
		comments, err := trackerRead[[]issueComment](ctx, call, trackerComments, trackerArgs{Key: h.Key})
		if errors.Is(err, errTrackerHistoryLimit) {
			h.Publication, h.RetryCommentAt = "comment_failed", nil
			h.PublicationMessage = "The tracker history could not confirm comment delivery."
			return r.holdForHistoryLimit(ctx, t, h)
		}
		if err != nil {
			return errors.New("readiness comment delivery is unconfirmed")
		}
		if i := publishedCommentIndex(h, comments); i >= 0 {
			h.CommentID, h.RoundAt = comments[i].ID, comments[i].At
			return r.confirmHoldComment(ctx, t, h)
		}
		if h.CommentID != "" {
			return r.failHoldComment(ctx, t, h, "The posted tracker comment could not be verified by ID and author. Review the issue and resolve the hold.")
		}
		if h.CommentAttempts >= maxCommentAttempts {
			return r.failHoldComment(ctx, t, h, "The tracker comment could not be confirmed after three attempts. Check the issue and connection, then resolve the hold.")
		}
		return r.postHoldComment(ctx, t, call, h)
	default:
		return nil
	}
}

func publishedCommentIndex(h Hold, comments []issueComment) int {
	expected := canonicalComment(holdMessage(h))
	return slices.IndexFunc(comments, func(row issueComment) bool {
		if row.ID == "" || h.CommentAuthor == "" || row.AuthorID != h.CommentAuthor {
			return false
		}
		if h.CommentID != "" {
			return row.ID == h.CommentID
		}
		return canonicalComment(row.Body) == expected
	})
}

// Jira ADF flattening can omit empty paragraphs. Preserve line boundaries
// and require the complete generated text and the unique marker.
func canonicalComment(body string) string {
	lines := strings.Split(body, "\n")
	return strings.Join(slices.DeleteFunc(lines, func(line string) bool { return strings.TrimSpace(line) == "" }), "\n")
}

func (h *Hold) prepareComment() {
	h.Marker, h.Publication = domain.NewID("hold"), "label_pending"
	h.RoundAt, h.CommentAttempts, h.RetryCommentAt, h.PublicationMessage = time.Time{}, 0, nil, ""
	h.CommentID, h.CommentAuthor, h.IdentityFailures = "", "", 0
}

func recordEvaluationFailure(h *Hold, reason string) {
	h.Round = min(h.Round+1, maxReadinessRounds)
	h.State, h.Reason = "failed", reason
	if h.Round == maxReadinessRounds {
		h.State, h.Reason, h.Intervention = "operator", "failure_round_limit", true
	}
}

func (r *Runner) finishHold(ctx context.Context, t *Trigger, call Invoker, h Hold, a ReadinessAssessment, current Issue) error {
	h.ScopeAssessment = nil
	h.Questions = a.Missing
	h.RetryFailures, h.RetryAt = 0, nil
	if a.Failure != "" {
		recordEvaluationFailure(&h, a.Failure)
	} else if len(a.Missing) > 0 {
		if h.Round >= maxReadinessRounds {
			h.State, h.Reason = "operator", "round_limit"
			if h.Intervention {
				h.Publication = "label_pending_only"
				h.Removals = selectionRemovals(t, current, h.Signal != "")
				if err := r.saveHold(ctx, t, h); err != nil {
					return err
				}
				return r.publishHold(ctx, t, call, h)
			}
			h.Intervention = true
		} else {
			h.State, h.Reason = "waiting", "incomplete"
			h.Round++
		}
	}
	h.prepareComment()
	h.Removals = selectionRemovals(t, current, h.Signal != "")
	if err := r.saveHold(ctx, t, h); err != nil {
		return err
	}
	r.log().InfoContext(ctx, "issue readiness held", "trigger", t.ID, "reason", h.Reason, "round", h.Round)
	return r.publishHold(ctx, t, call, h)
}

func claimRemovals(t *Trigger, i Issue, requeued bool) []string {
	remove := selectionRemovals(t, i, requeued)
	// The waiting marker is Weft-owned. Claim cleanup is independent of
	// the current hold scope and bounded resolution history.
	if i.HasLabel(LabelFailed) {
		remove = append(remove, LabelFailed)
	}
	return remove
}

func selectionRemovals(t *Trigger, i Issue, requeued bool) []string {
	remove := []string{}
	for _, label := range []string{t.Label, LabelTodo} {
		if (label == t.Label || requeued) && i.HasLabel(label) && !slices.Contains(remove, label) {
			remove = append(remove, label)
		}
	}
	return remove
}

func (r *Runner) holdForHistoryLimit(ctx context.Context, t *Trigger, h Hold) error {
	h.State, h.Reason = "operator", "history_limit"
	if err := r.saveHold(ctx, t, h); err != nil {
		return err
	}
	r.log().InfoContext(ctx, "issue readiness requires operator review", "trigger", t.ID, "reason", h.Reason, "round", h.Round)
	return nil
}

func (r *Runner) currentReplies(ctx context.Context, t *Trigger, call Invoker, h Hold, i Issue) ([]issueComment, error) {
	if h.RoundAt.IsZero() {
		return nil, nil
	}
	replies, err := trackerRead[[]issueComment](ctx, call, trackerComments, trackerArgs{Key: i.Key, Since: h.OpenedAt})
	replies = slices.DeleteFunc(replies, func(reply issueComment) bool {
		return h.CommentID != "" && reply.ID == h.CommentID && reply.AuthorID == h.CommentAuthor
	})
	return eligibleReplies(i, t.Owner, replies), err
}

func recordSnapshotInvalidation(h *Hold, reason string) {
	h.RetryFailures, h.RetryAt = 0, nil
	h.SnapshotEdits = min(h.SnapshotEdits+1, maxSnapshotEdits)
	h.State, h.Reason = "recheck", reason
	if h.SnapshotEdits >= maxSnapshotEdits {
		h.State, h.Reason, h.Intervention = "operator", "snapshot_round_limit", true
		if reason == "eligibility_changed" {
			h.Reason = "eligibility_edit_limit"
		}
	}
}

func (r *Runner) recheckSnapshot(ctx context.Context, t *Trigger, call Invoker, h Hold, current Issue, reason string) error {
	recordSnapshotInvalidation(&h, reason)
	if h.State != "operator" || holdReleaseReason(t, h, current) != "" {
		return r.saveHold(ctx, t, h)
	}
	h.prepareComment()
	h.Removals = selectionRemovals(t, current, h.Signal != "")
	if err := r.saveHold(ctx, t, h); err != nil {
		return err
	}
	return r.publishHold(ctx, t, call, h)
}

func deferScopeAssessment(h *Hold, a ReadinessAssessment) {
	h.State, h.Reason = "scope_waiting", "scope_waiting"
	h.RetryFailures, h.RetryAt = 0, nil
	h.ScopeAssessment = nil
	if a.Failure != "" || len(a.Missing) > 0 {
		h.ScopeAssessment = &a
		h.Reason = "scope_assessment_pending"
	}
}

func (r *Runner) assessIssue(ctx context.Context, t *Trigger, call Invoker, i Issue, h Hold, replies []issueComment, scopeVerified bool) (bool, error) {
	input := readinessInput(i, replies)
	h.Evaluated = triggerFingerprint(input)
	h.ScopeAssessment = nil
	h.State, h.Reason = "evaluating", "evaluation_interrupted"
	if err := r.saveHold(ctx, t, h); err != nil {
		return false, err
	}
	if r.Assessor == nil {
		a := ReadinessAssessment{Failure: "model_unavailable"}
		if scopeVerified {
			return false, r.finishHold(ctx, t, call, h, a, i)
		}
		deferScopeAssessment(&h, a)
		return false, r.saveHold(ctx, t, h)
	}
	if err := r.ensureCurrent(ctx, t); err != nil {
		return false, err
	}
	a := r.Assessor.Assess(ctx, t, input)
	current, err := trackerRead[Issue](ctx, call, trackerGet, trackerArgs{Key: i.Key})
	if err != nil {
		return false, r.retryReadiness(ctx, t, h, "tracker_unavailable", err)
	}
	latest, err := r.currentReplies(ctx, t, call, h, current)
	if errors.Is(err, errTrackerHistoryLimit) {
		return false, r.holdForHistoryLimit(ctx, t, h)
	}
	if err != nil {
		return false, r.retryReadiness(ctx, t, h, "tracker_unavailable", err)
	}
	if readinessInput(current, latest) != input {
		return false, r.recheckSnapshot(ctx, t, call, h, current, "snapshot_changed")
	}
	if i.Updated != current.Updated || issueReason(t, current) != "" || !slices.Equal(i.Labels, current.Labels) {
		return false, r.recheckSnapshot(ctx, t, call, h, current, "eligibility_changed")
	}
	if !scopeVerified {
		deferScopeAssessment(&h, a)
		return false, r.saveHold(ctx, t, h)
	}
	if a.Failure != "" || len(a.Missing) > 0 {
		return false, r.finishHold(ctx, t, call, h, a, current)
	}
	current.EvaluatedInput = input
	outcome, err := r.claimAndStart(ctx, t, call, current)
	if outcome == "claim_busy" {
		return false, r.retryReadiness(ctx, t, h, "claim_busy", err)
	}
	return outcome != "readiness_changed", err
}

func (r *Runner) checkHeld(ctx context.Context, t *Trigger, src Source, call Invoker, h Hold) (bool, error) {
	if h.Scope != holdScope(t) {
		if holdHasPendingWrite(h) {
			return false, r.failHoldLabels(ctx, t, h, "The pending update belongs to a different trigger scope.")
		}
		if h.Publication == "label_failed" {
			return false, nil
		}
		return false, r.releaseHold(ctx, t, h, ReadinessReason("scope_changed"), false)
	}
	if holdHasPendingWrite(h) {
		return false, r.publishHold(ctx, t, call, h)
	}
	now := r.Triggers.now()
	if (h.RetryAt != nil && now.Before(*h.RetryAt)) || (h.RetryAccessAt != nil && now.Before(*h.RetryAccessAt)) {
		return false, nil
	}
	i, err := trackerRead[Issue](ctx, call, trackerGet, trackerArgs{Key: h.Key, VerifyMissing: true})
	if err != nil {
		if trackerIssueMissing(err) {
			return false, r.checkMissingHold(ctx, t, h, err)
		}
		if h.State == "recheck" && h.RetryFailures > 0 {
			return false, r.retryReadiness(ctx, t, h, "tracker_unavailable", err)
		}
		if h.State == "retry_failed" || h.Publication == "label_failed" {
			return false, nil
		}
		return false, err
	}
	if h.MissingPoll != 0 || h.RetryAccessAt != nil || h.Eligibility == issueMissingForOperator || h.Eligibility == issueMissingAwaitingPoll {
		h.MissingPoll, h.MissingSince, h.RetryAccessAt, h.Eligibility = 0, time.Time{}, nil, ""
		if err := r.saveHold(ctx, t, h); err != nil {
			return false, err
		}
	}
	if h.State == "retry_failed" || h.Publication == "label_failed" {
		return false, nil
	}
	if reason := holdReleaseReason(t, h, i); reason != "" {
		return false, r.releaseHold(ctx, t, h, reason, h.SnapshotEdits > 0)
	}
	if h.Publication == "label_applied" || h.Publication == "comment_pending" {
		return false, r.publishHold(ctx, t, call, h)
	}
	scopeVerified := true
	if t.Source.Query != "" {
		h, scopeVerified, err = r.verifyHeldScope(ctx, t, src, call, h, i)
		if err != nil || (!scopeVerified && h.Eligibility != githubScopeRESTFallback) {
			return false, err
		}
	}
	if h.Publication == "comment_failed" || requiresOperator(h) {
		return false, nil
	}
	if h.State == "evaluating" {
		return false, r.retryReadiness(ctx, t, h, "evaluation_interrupted", nil)
	}
	replies, err := r.currentReplies(ctx, t, call, h, i)
	if errors.Is(err, errTrackerHistoryLimit) {
		return false, r.holdForHistoryLimit(ctx, t, h)
	}
	if err != nil {
		if h.State == "recheck" && h.RetryFailures > 0 {
			return false, r.retryReadiness(ctx, t, h, "tracker_unavailable", err)
		}
		return false, err
	}
	if h.State == "recheck" {
		if i.HasLabel(t.Label) || (h.Publication == "confirmed" && i.HasLabel(LabelFailed)) || (h.Signal != "" && i.HasLabel(LabelTodo)) {
			return r.assessIssue(ctx, t, call, i, h, replies, scopeVerified)
		}
		return false, nil
	}
	signal := ""
	if i.HasLabel(LabelTodo) {
		signal, err = trackerRead[string](ctx, call, trackerRequeue, trackerArgs{Key: h.Key, Since: h.RoundAt})
		if errors.Is(err, errTrackerHistoryLimit) {
			return false, r.holdForHistoryLimit(ctx, t, h)
		}
		if err != nil {
			return false, err
		}
	}
	changed := triggerFingerprint(readinessInput(i, replies)) != h.Evaluated
	newSignal := signal != "" && signal != h.Signal
	if scopeVerified && h.State == "scope_waiting" && h.ScopeAssessment != nil && !changed && !newSignal {
		return false, r.finishHold(ctx, t, call, h, *h.ScopeAssessment, i)
	}
	answered := changed && (h.State == "waiting" || (h.State == "failed" && !h.Intervention)) &&
		slices.ContainsFunc(replies, func(reply issueComment) bool { return reply.At.After(h.RoundAt) })
	needsScopeRecheck := h.State == "scope_waiting" && (scopeVerified || changed)
	if !answered && !needsScopeRecheck && !newSignal {
		return false, nil
	}
	if signal != "" {
		h.Signal = signal
	}
	return r.assessIssue(ctx, t, call, i, h, replies, scopeVerified)
}

var errHeldRepositoryAccess = errors.New("The tracker connection could not access the configured GitHub repository. Review its permissions.")
var errHeldIssuesDisabled = errors.New("Issues are disabled in the configured GitHub repository. Held issues remain pending.")
var errHeldProjectAccess = errors.New("The tracker connection could not access the held issue's Jira project. Review its permissions.")
var errHeldIssueAccessUnverified = errors.New("The tracker could not verify access to the repository or project. Held issues remain pending.")

const issueMissingForOperator = "could not be found. Review the tracker and resolve the hold"
const issueMissingAwaitingPoll = "could not be found and is awaiting confirmation after the minimum polling interval"

func hasConsecutiveMissingObservation(t *Trigger, h Hold) bool {
	return h.MissingPoll != 0 && h.MissingPoll == t.PollSequence && !h.MissingSince.IsZero()
}

func missingHoldDisposition(t *Trigger, h Hold, failure trackerHTTPFailure, now time.Time) (release bool, reason string, err error) {
	if t.Source.Kind == "github" || t.Source.Kind == "jira" {
		if failure.IssuesDisabled {
			return false, "", errHeldIssuesDisabled
		}
		switch failure.AccessStatus {
		case http.StatusNotFound, http.StatusForbidden:
			if t.Source.Kind == "jira" {
				return false, "", errHeldProjectAccess
			}
			return false, "", errHeldRepositoryAccess
		default:
			if failure.AccessStatus < 200 || failure.AccessStatus >= 300 {
				return false, "", errHeldIssueAccessUnverified
			}
		}
	}
	if requiresOperator(h) || h.State == "operator" || h.Publication == "comment_failed" || h.Publication == "label_failed" ||
		h.Publication == "comment_pending" || h.RetryCommentAt != nil {
		return false, issueMissingForOperator, nil
	}
	if t.Source.Kind == "github" && failure.Status == http.StatusGone {
		return true, "The tracker reported that the issue was deleted (HTTP 410).", nil
	}
	minimum := time.Duration(max(1, t.IntervalMinutes)) * time.Minute
	if hasConsecutiveMissingObservation(t, h) && !now.Before(h.MissingSince.Add(minimum)) {
		return true, fmt.Sprintf("The tracker could not find the issue in consecutive polls at least one polling interval apart (HTTP %d).", failure.Status), nil
	}
	return false, issueMissingAwaitingPoll, nil
}

func (r *Runner) checkMissingHold(ctx context.Context, t *Trigger, h Hold, err error) error {
	var failure trackerHTTPFailure
	if !errors.As(err, &failure) {
		return err
	}
	now := r.Triggers.now()
	release, reason, err := missingHoldDisposition(t, h, failure, now)
	if errors.Is(err, errHeldIssueAccessUnverified) {
		next := issueAccessRetryAt(now, failure)
		h.MissingPoll, h.MissingSince, h.RetryAccessAt = 0, time.Time{}, &next
		h.Eligibility = "could not be checked because repository or project access verification is temporarily unavailable"
		return r.saveHold(ctx, t, h)
	}
	if err != nil {
		return err
	}
	if release {
		return r.releaseHold(ctx, t, h, reason, true)
	}
	h.RetryAccessAt = nil
	if reason == issueMissingAwaitingPoll {
		if !hasConsecutiveMissingObservation(t, h) {
			h.MissingSince = now
		}
		h.MissingPoll = t.PollSequence + 1
	} else {
		h.MissingPoll, h.MissingSince = 0, time.Time{}
		if h.Eligibility == reason {
			return nil
		}
	}
	h.Eligibility = reason
	return r.saveHold(ctx, t, h)
}

func issueAccessRetryAt(now time.Time, failure trackerHTTPFailure) time.Time {
	if retry := trackerRetryDeadline(now, failure); !retry.IsZero() {
		return retry
	}
	return now.Add(time.Minute)
}

func requiresOperator(h Hold) bool {
	return h.State == "retry_failed" || h.Reason == "failure_round_limit" || h.Reason == "history_limit" || h.Reason == "snapshot_round_limit" || h.Reason == "eligibility_edit_limit"
}

const maxGitHubIndexLagPolls = 3
const githubScopeRESTFallback = "is using direct GitHub reads to process responses while search catches up. Claim waits for query verification"

func (r *Runner) verifyHeldScope(ctx context.Context, t *Trigger, src Source, call Invoker, h Hold, i Issue) (Hold, bool, error) {
	var matches bool
	var err error
	isGitHub := t.Source.Kind == "github"
	if isGitHub {
		matches, err = src.MatchesHeld(ctx, call, i, "")
	} else {
		matches, err = searchHeldScope(ctx, src, call, i, t.Label)
	}
	previous := h
	switch {
	case errors.Is(err, errSearchBudgetDeferred):
		h.Eligibility = "could not be verified because the GitHub polling search budget was reached"
	case errors.Is(err, errHeldScopeIndexLag):
		h.ScopeLagPolls = min(h.ScopeLagPolls+1, maxGitHubIndexLagPolls)
		h.Eligibility = "could not be verified because GitHub search has not indexed the current issue revision"
		if h.ScopeLagPolls >= maxGitHubIndexLagPolls {
			h.Eligibility = githubScopeRESTFallback
		}
		h.ScopeMiss = ""
	case errors.Is(err, errHeldScopeUnverified):
		h.Eligibility = "could not be verified against the trigger query within the tracker search limit"
		h.ScopeMiss = ""
	case err != nil && (!isGitHub || errors.Is(err, errTriggerChanged)):
		return h, false, err
	case err != nil:
		h.Eligibility = "could not be verified because the GitHub search is unavailable"
		h.ScopeMiss = ""
	case matches:
		h.Eligibility, h.ScopeMiss = "", ""
	case !isGitHub:
		return h, false, r.releaseHold(ctx, t, h, "The issue no longer matches the trigger query.", false)
	default:
		revision := triggerFingerprint(i)
		if h.ScopeMiss == revision {
			return h, false, r.releaseHold(ctx, t, h, "The issue no longer matches the trigger query.", true)
		}
		h.ScopeMiss = revision
		h.Eligibility = "was absent from the trigger query and is awaiting confirmation in another poll"
	}
	if isGitHub && !errors.Is(err, errSearchBudgetDeferred) {
		if !errors.Is(err, errHeldScopeIndexLag) {
			h.ScopeLagPolls = 0
		}
		h.ScopeCheckedAt = r.Triggers.now()
	}
	if h.Eligibility != previous.Eligibility || h.ScopeMiss != previous.ScopeMiss || h.ScopeLagPolls != previous.ScopeLagPolls || h.ScopeCheckedAt != previous.ScopeCheckedAt {
		if err := r.saveHold(ctx, t, h); err != nil {
			return h, false, err
		}
	}
	return h, matches, nil
}

func searchHeldScope(ctx context.Context, src Source, call Invoker, i Issue, selection string) (bool, error) {
	uncertain := false
	for _, label := range []string{LabelFailed, LabelTodo, selection} {
		if !i.HasLabel(label) {
			continue
		}
		matches, err := src.MatchesHeld(ctx, call, i, label)
		if errors.Is(err, errHeldScopeUnverified) {
			uncertain = true
			continue
		}
		if err != nil || matches {
			return matches, err
		}
	}
	if uncertain {
		return false, errHeldScopeUnverified
	}
	return false, nil
}

const maxReadinessRetries = 3

func trackerRetryDeadline(now time.Time, err error) time.Time {
	var failure trackerHTTPFailure
	if !errors.As(err, &failure) || !failure.RateLimited {
		return time.Time{}
	}
	latest := now.Add(time.Hour)
	next := now.Add(min(time.Hour, max(time.Minute, failure.RetryDelay)))
	if failure.RetryDeadline != nil && failure.RetryDeadline.After(next) {
		if failure.RetryDeadline.After(latest) {
			return latest
		}
		return *failure.RetryDeadline
	}
	return next
}

func readinessRetryAt(now time.Time, failures int, err error) *time.Time {
	next := now.Add(time.Minute << (failures - 1))
	if deadline := trackerRetryDeadline(now, err); deadline.After(next) {
		next = deadline
	}
	return &next
}

func isDefinitiveTrackerFailure(err error) bool {
	var failure trackerHTTPFailure
	return errors.As(err, &failure) && failure.definitive()
}

func retryFailureCause(reason string) string {
	switch reason {
	case "claim_busy":
		return "issue admission could not be coordinated."
	case "claim_recheck_unavailable":
		return "the tracker could not recheck the issue after claim."
	case "evaluation_interrupted":
		return "evaluation was repeatedly interrupted."
	default:
		return "the tracker could not verify the issue or its responses."
	}
}

func recordReadinessRetry(h *Hold, now time.Time, reason string, err error) {
	h.RetryFailures++
	h.State, h.Reason = "recheck", reason
	if isDefinitiveTrackerFailure(err) || h.RetryFailures > maxReadinessRetries {
		h.State, h.RetryAt = "retry_failed", nil
		return
	}
	h.RetryAt = readinessRetryAt(now, h.RetryFailures, err)
}

func (r *Runner) retryReadiness(ctx context.Context, t *Trigger, h Hold, reason string, err error) error {
	if errors.Is(err, errTriggerChanged) {
		return err
	}
	recordReadinessRetry(&h, r.Triggers.now(), reason, err)
	return r.saveHold(ctx, t, h)
}
