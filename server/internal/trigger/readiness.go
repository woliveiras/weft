package trigger

import (
	"context"
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
	case "round_limit":
		return "The issue needs operator intervention after two question rounds."
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
	return trackerWrite(ctx, call, trackerComment, trackerArgs{Key: key, Body: body})
}

// Publish in two recorded steps. A comment whose response was lost can be
// confirmed by its unique marker. It is never automatically posted twice.
func (r *Runner) publishHold(ctx context.Context, t *Trigger, call Invoker, h Hold) error {
	switch h.Publication {
	case "label_pending", "label_pending_only":
		if err := relabel(ctx, call, h.Key, h.Removals, LabelFailed, ""); err != nil {
			return errors.New("readiness label write did not complete")
		}
		if h.Publication == "label_pending_only" {
			h.Publication = "confirmed"
			return r.saveHold(ctx, t, h)
		}
		h.Publication = "comment_pending"
		h.RoundAt = r.Triggers.now()
		if err := r.saveHold(ctx, t, h); err != nil {
			return err
		}
		if err := comment(ctx, call, h.Key, holdMessage(h)); err != nil {
			return errors.New("readiness comment delivery is unconfirmed")
		}
	case "comment_pending":
		comments, err := trackerRead[[]issueComment](ctx, call, trackerComments, trackerArgs{Key: h.Key, Since: h.RoundAt.Add(-time.Second)})
		if err != nil || !slices.ContainsFunc(comments, func(row issueComment) bool { return canonicalComment(row.Body) == canonicalComment(holdMessage(h)) }) {
			return errors.New("readiness comment delivery is unconfirmed")
		}
	default:
		return nil
	}
	h.Publication = "confirmed"
	return r.saveHold(ctx, t, h)
}

// Jira ADF flattening can omit empty paragraphs. Preserve line boundaries
// and require the complete generated text and the unique marker.
func canonicalComment(body string) string {
	lines := strings.Split(body, "\n")
	return strings.Join(slices.DeleteFunc(lines, func(line string) bool { return strings.TrimSpace(line) == "" }), "\n")
}

func (r *Runner) finishHold(ctx context.Context, t *Trigger, call Invoker, h Hold, a ReadinessAssessment, current Issue) error {
	h.Reason, h.State = a.Failure, "failed"
	h.Questions = a.Missing
	if len(a.Missing) > 0 {
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
	h.Marker, h.Publication = domain.NewID("hold"), "label_pending"
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

func (r *Runner) currentReplies(ctx context.Context, t *Trigger, call Invoker, h Hold, i Issue) ([]issueComment, error) {
	if h.Round == 0 && h.RoundAt.IsZero() {
		return nil, nil
	}
	replies, err := trackerRead[[]issueComment](ctx, call, trackerComments, trackerArgs{Key: i.Key, Since: h.OpenedAt})
	return eligibleReplies(i, t.Owner, replies), err
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
	if err != nil {
		return false, r.finishHold(ctx, t, call, h, ReadinessAssessment{Failure: "tracker_unavailable"}, current)
	}
	if i.Updated != current.Updated || readinessInput(current, latest) != input || issueReason(t, current) != "" ||
		!slices.Equal(i.Labels, current.Labels) {
		h.State, h.Reason = "recheck", "snapshot_changed"
		return false, r.saveHold(ctx, t, h)
	}
	current.EvaluatedInput = input
	outcome, err := r.claimAndStart(ctx, t, call, current)
	if outcome == "claim_busy" {
		h.State, h.Reason = "recheck", "snapshot_changed"
		return false, errors.Join(err, r.saveHold(ctx, t, h))
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
	if reason := holdReleaseReason(t, h, i); reason != "" {
		return false, r.releaseHold(ctx, t, h, reason)
	}
	if t.Source.Query != "" {
		inScope, err := searchHeldScope(ctx, src, call, i, t.Label)
		if err != nil {
			return false, err
		}
		if !inScope && !(h.Publication == "label_pending" && i.HasLabel(LabelInProgress)) {
			return false, r.releaseHold(ctx, t, h, "The issue no longer matches the trigger query.")
		}
	}
	if h.Publication == "label_pending" || h.Publication == "label_pending_only" || h.Publication == "comment_pending" {
		return false, r.publishHold(ctx, t, call, h)
	}
	if h.State == "evaluating" {
		return false, r.finishHold(ctx, t, call, h, ReadinessAssessment{Failure: "evaluation_interrupted"}, i)
	}
	replies, err := r.currentReplies(ctx, t, call, h, i)
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
		issues, err := src.Search(ctx, call, label)
		if err != nil {
			return false, err
		}
		if slices.ContainsFunc(issues, func(found Issue) bool { return found.Key == i.Key }) {
			return true, nil
		}
		uncertain = uncertain || len(issues) >= trackerSearchLimit
	}
	if uncertain {
		return false, errors.New("the bounded tracker search could not verify the held issue scope")
	}
	return false, nil
}
