package trigger

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
)

const readinessCapacity = "The trigger has 10 held issues. Resolve a hold before it evaluates new issues."

type Hold struct {
	CommentID          string     `json:"commentId,omitempty"`
	CommentAuthor      string     `json:"commentAuthor,omitempty"`
	UndoOwner          string     `json:"undoOwner,omitempty"`
	PublicationMessage string     `json:"publicationMessage,omitempty"`
	CommentAttempts    int        `json:"commentAttempts,omitempty"`
	RetryCommentAt     *time.Time `json:"retryCommentAt,omitempty"`

	Eligibility  string    `json:"eligibility,omitempty"`
	Message      string    `json:"message"`
	Key          string    `json:"key"`
	URL          string    `json:"url,omitempty"`
	Reason       string    `json:"reason"`
	State        string    `json:"state"`
	Round        int       `json:"round"`
	OpenedAt     time.Time `json:"openedAt"`
	RoundAt      time.Time `json:"roundAt"`
	Publication  string    `json:"publication,omitempty"`
	Scope        string    `json:"scope"`
	Evaluated    string    `json:"evaluated,omitempty"`
	Signal       string    `json:"signal,omitempty"`
	Questions    []string  `json:"questions,omitempty"`
	Marker       string    `json:"marker,omitempty"`
	Intervention bool      `json:"intervention,omitempty"`
	Removals     []string  `json:"removals,omitempty"`
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
			lines = []string{"Weft readiness failure. Repeated issue changes exhausted the readiness allowance. An operator should review the current issue and resolve the hold before it is selected again."}
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
		return "The issue changed repeatedly and exhausted the readiness allowance. Review the current issue and resolve the hold before selecting it again."
	case "snapshot_changed":
		return "The issue changed during readiness evaluation."
	case "evaluation_interrupted":
		return "Readiness evaluation was interrupted."
	case "scope_changed":
		return "This hold belongs to a different tracker or workflow scope."
	default:
		return "Readiness could not be confirmed."
	}
}

func (r *Runner) saveHold(ctx context.Context, t *Trigger, h Hold) error {
	h.Message = ReadinessReason(h.Reason)
	current, err := r.Triggers.Update(ctx, t.ID, "system:trigger", func(current *Trigger) error {
		if current.State != Active || current.Consent.Scope != t.Consent.Scope || current.Consent.RecordedAt != t.Consent.RecordedAt {
			return errTriggerChanged
		}
		idx := current.holdIndex(h.Key)
		if idx >= 0 {
			current.Holds[idx] = h
		} else {
			if len(current.Holds) >= maxReadinessHolds {
				return storage.ErrConflict
			}
			current.Holds = append(current.Holds, h)
		}
		if len(current.Holds) >= maxReadinessHolds {
			current.ReadinessBlock = readinessCapacity
		}
		return nil
	})
	if err == nil {
		t.Holds, t.ReadinessBlock = current.Holds, current.ReadinessBlock
	}
	return err
}

func trackerRead[T any](ctx context.Context, call Invoker, name string, args trackerArgs) (T, error) {
	var out T
	res, err := call(ctx, name, args)
	if err == nil && res != nil && res.IsError && (name == trackerComments || name == trackerRequeue) &&
		bytes.Equal(res.Data, trackerHistoryLimitResult) {
		return out, errTrackerHistoryLimit
	}
	err = decode(res, err, &out)
	return out, err
}

func trackerWrite(ctx context.Context, call Invoker, name string, args trackerArgs) error {
	return result(call(ctx, name, args))
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
	if err == nil && res != nil && res.IsError {
		var failure trackerCommentFailure
		if json.Unmarshal(res.Data, &failure) == nil && failure.Status >= 400 && failure.Status <= 599 {
			return commentReceipt{}, failure
		}
	}
	var receipt commentReceipt
	err = decode(res, err, &receipt)
	return receipt, err
}

const maxCommentAttempts = 3

func (r *Runner) postHoldComment(ctx context.Context, t *Trigger, call Invoker, h Hold) error {
	author, err := trackerRead[string](ctx, call, trackerIdentity, trackerArgs{Key: h.Key})
	if err != nil || author == "" {
		h.Publication, h.UndoOwner = "comment_pending", ""
		return r.failHoldComment(ctx, t, h, "The tracker comment author could not be verified. Check the connection and resolve the hold.")
	}
	if h.CommentAuthor != "" && h.CommentAuthor != author {
		return r.failHoldComment(ctx, t, h, "The tracker comment author changed. Review the issue and resolve the hold.")
	}
	h.CommentAuthor = author
	h.CommentAttempts++
	now := r.Triggers.now()
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
	var failure trackerCommentFailure
	if errors.As(err, &failure) && failure.definitive() {
		return r.failHoldComment(ctx, t, h, failure.guidance())
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
		if err := trackerWrite(ctx, call, trackerLabels, trackerArgs{Key: h.Key, Remove: h.Removals, Add: LabelFailed, UndoOwner: h.UndoOwner}); err != nil {
			return errors.New("readiness label write did not complete")
		}
		h.UndoOwner = ""
		if h.Publication == "label_pending_only" {
			return r.confirmHoldComment(ctx, t, h)
		}
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
	h.CommentID, h.CommentAuthor = "", ""
}

func recordEvaluationFailure(h *Hold, reason string) {
	h.Round = min(h.Round+1, maxReadinessRounds)
	h.State, h.Reason = "failed", reason
	if h.Round == maxReadinessRounds {
		h.State, h.Reason, h.Intervention = "operator", "failure_round_limit", true
	}
}

func (r *Runner) finishHold(ctx context.Context, t *Trigger, call Invoker, h Hold, a ReadinessAssessment, current Issue) error {
	h.Questions = a.Missing
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

func recordSnapshotInvalidation(h *Hold) {
	h.Round = min(h.Round+1, maxReadinessRounds)
	h.State, h.Reason = "recheck", "snapshot_changed"
	if h.Round >= maxReadinessRounds {
		h.State, h.Reason, h.Intervention = "operator", "snapshot_round_limit", true
	}
}

func (r *Runner) recheckSnapshot(ctx context.Context, t *Trigger, call Invoker, h Hold, current Issue) error {
	recordSnapshotInvalidation(&h)
	if h.State != "operator" {
		return r.saveHold(ctx, t, h)
	}
	h.prepareComment()
	h.Removals = selectionRemovals(t, current, h.Signal != "")
	if err := r.saveHold(ctx, t, h); err != nil {
		return err
	}
	return r.publishHold(ctx, t, call, h)
}

func (r *Runner) assessIssue(ctx context.Context, t *Trigger, call Invoker, i Issue, h Hold, replies []issueComment) (bool, error) {
	input := readinessInput(i, replies)
	h.Evaluated = triggerFingerprint(input)
	h.State, h.Reason = "evaluating", "evaluation_interrupted"
	if err := r.saveHold(ctx, t, h); err != nil {
		return false, err
	}
	if r.Assessor == nil {
		return false, r.finishHold(ctx, t, call, h, ReadinessAssessment{Failure: "model_unavailable"}, i)
	}
	if err := r.ensureCurrent(ctx, t); err != nil {
		return false, err
	}
	a := r.Assessor.Assess(ctx, t, input)
	if a.Failure != "" || len(a.Missing) > 0 {
		return false, r.finishHold(ctx, t, call, h, a, i)
	}
	current, err := trackerRead[Issue](ctx, call, trackerGet, trackerArgs{Key: i.Key})
	if err != nil {
		return false, r.finishHold(ctx, t, call, h, ReadinessAssessment{Failure: "tracker_unavailable"}, i)
	}
	latest, err := r.currentReplies(ctx, t, call, h, current)
	if errors.Is(err, errTrackerHistoryLimit) {
		return false, r.holdForHistoryLimit(ctx, t, h)
	}
	if err != nil {
		return false, r.finishHold(ctx, t, call, h, ReadinessAssessment{Failure: "tracker_unavailable"}, current)
	}
	if i.Updated != current.Updated || readinessInput(current, latest) != input || issueReason(t, current) != "" ||
		!slices.Equal(i.Labels, current.Labels) {
		return false, r.recheckSnapshot(ctx, t, call, h, current)
	}
	current.EvaluatedInput = input
	outcome, err := r.claimAndStart(ctx, t, call, current)
	if outcome == "claim_busy" {
		return false, errors.Join(err, r.recheckSnapshot(ctx, t, call, h, current))
	}
	return true, err
}

func (r *Runner) checkHeld(ctx context.Context, t *Trigger, src Source, call Invoker, h Hold) (bool, error) {
	if h.Scope != holdScope(t) {
		return false, r.releaseHold(ctx, t, h, ReadinessReason("scope_changed"))
	}
	i, err := trackerRead[Issue](ctx, call, trackerGet, trackerArgs{Key: h.Key})
	if err != nil {
		return false, err
	}
	if (h.Publication == "label_pending" || h.Publication == "label_pending_only") && h.UndoOwner != "" {
		return false, r.publishHold(ctx, t, call, h)
	}
	if reason := holdReleaseReason(t, h, i); reason != "" {
		return false, r.releaseHold(ctx, t, h, reason)
	}
	if t.Source.Query != "" {
		inScope, err := searchHeldScope(ctx, src, call, i, t.Label)
		if errors.Is(err, errHeldScopeUnverified) {
			const reason = "could not be verified against the trigger query within the tracker search limit"
			if h.Eligibility == reason {
				return false, nil
			}
			h.Eligibility = reason
			return false, r.saveHold(ctx, t, h)
		}
		if err != nil {
			return false, err
		}
		if h.Eligibility != "" {
			h.Eligibility = ""
			if err := r.saveHold(ctx, t, h); err != nil {
				return false, err
			}
		}
		if !inScope && !(h.Publication == "label_pending" && i.HasLabel(LabelInProgress)) {
			return false, r.releaseHold(ctx, t, h, "The issue no longer matches the trigger query.")
		}
	}
	if h.Publication == "label_pending" || h.Publication == "label_pending_only" || h.Publication == "comment_pending" {
		return false, r.publishHold(ctx, t, call, h)
	}
	if h.Publication == "comment_failed" || (h.Reason == "failure_round_limit" || h.Reason == "history_limit" || h.Reason == "snapshot_round_limit") {
		return false, nil
	}
	if h.State == "evaluating" {
		return false, r.finishHold(ctx, t, call, h, ReadinessAssessment{Failure: "evaluation_interrupted"}, i)
	}
	replies, err := r.currentReplies(ctx, t, call, h, i)
	if errors.Is(err, errTrackerHistoryLimit) {
		return false, r.holdForHistoryLimit(ctx, t, h)
	}
	if err != nil {
		return false, err
	}
	if h.State == "recheck" {
		if i.HasLabel(t.Label) || (h.Publication == "confirmed" && i.HasLabel(LabelFailed)) || (h.Signal != "" && i.HasLabel(LabelTodo)) {
			return r.assessIssue(ctx, t, call, i, h, replies)
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
	answered := (h.State == "waiting" || (h.State == "failed" && !h.Intervention)) && slices.ContainsFunc(replies, func(reply issueComment) bool { return reply.At.After(h.RoundAt) }) &&
		triggerFingerprint(readinessInput(i, replies)) != h.Evaluated
	if !answered && (signal == "" || signal == h.Signal) {
		return false, nil
	}
	if signal != "" {
		h.Signal = signal
	}
	return r.assessIssue(ctx, t, call, i, h, replies)
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
