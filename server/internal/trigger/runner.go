package trigger

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/observability"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/tools"
)

// Runner polls active triggers on the server's scheduled loop. A lease per
// trigger makes sure only one instance works on it at a time; a trigger
// handles one issue at a time; times missed while the server was down or
// busy collapse into a single run.
type Runner struct {
	Triggers Manager
	Store    storage.Store
	// Instance holds the leases.
	Instance string
	Gateway  *tools.Gateway
	Tasks    Tasks
	Assessor Assessor
	// BaseURL links issues back to their task ("<base>/#/tasks/<id>").
	BaseURL string
	Log     *slog.Logger
	// Every is how often triggers are checked (default 30s).
	Every time.Duration
}

func (r *Runner) log() *slog.Logger {
	if r.Log != nil {
		return r.Log
	}
	return slog.Default()
}

// Run checks triggers until ctx ends.
func (r *Runner) Run(ctx context.Context) {
	every := r.Every
	if every <= 0 {
		every = 30 * time.Second
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		r.Tick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Due reports whether an active trigger should run now.
func Due(t *Trigger, now time.Time) bool {
	return t.State == Active && (t.NextRunAt == nil || !now.Before(*t.NextRunAt))
}

// Tick runs every due trigger once.
func (r *Runner) Tick(ctx context.Context) {
	list, err := r.Triggers.List(ctx, "")
	if err != nil {
		r.log().Error("triggers could not be listed", "err", err)
		return
	}
	now := r.Triggers.now()
	for _, t := range list {
		if ctx.Err() != nil {
			return
		}
		if !Due(t, now) && !pausedRollbackDue(t, now) {
			continue
		}
		r.runOne(ctx, t.ID)
	}
}

func (r *Runner) runOne(ctx context.Context, id string) {
	key := "trigger/" + id
	ok, err := r.Store.AcquireLease(ctx, key, r.Instance, 10*time.Minute)
	if err != nil || !ok {
		return // another instance has it
	}
	defer func() { _ = r.Store.ReleaseLease(context.WithoutCancel(ctx), key, r.Instance) }()
	t, err := r.Triggers.Get(ctx, id)
	if err != nil || (!Due(t, r.Triggers.now()) && !pausedRollbackDue(t, r.Triggers.now())) {
		return // changed or ran elsewhere meanwhile
	}
	started := time.Now()
	pollCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	outcome, err := r.step(pollCtx, t)
	cancel()
	if err != nil {
		outcome = "error"
		r.log().Warn("trigger run failed", "trigger", id, "err", err)
	}
	observability.TriggerRuns.WithLabelValues(outcome).Inc()
	observability.TriggerSeconds.Observe(time.Since(started).Seconds())
	now := r.Triggers.now()
	next := now.Add(time.Duration(t.IntervalMinutes) * time.Minute)
	if _, uerr := r.Triggers.Update(ctx, id, "system:trigger", func(t *Trigger) error {
		t.LastRunAt, t.NextRunAt = &now, &next
		t.LastError = ""
		if err != nil {
			t.LastError = err.Error()
		}
		return nil
	}); uerr != nil {
		r.log().Error("trigger state could not be saved", "trigger", id, "err", uerr)
	}
	if r.Triggers.Bus != nil {
		data := map[string]any{"trigger": id, "outcome": outcome}
		if err != nil {
			data["error"] = err.Error()
		}
		r.Triggers.Bus.Publish(context.WithoutCancel(ctx), domain.NewEvent(domain.EvTriggerRun, "", data).WithActor("system:trigger"))
	}
}

// step does one run of a trigger and returns a bounded outcome label.
func (r *Runner) step(ctx context.Context, t *Trigger) (string, error) {
	problem, err := r.Triggers.ConsentProblem(ctx, t)
	if err != nil {
		return "", err
	}
	if problem != "" {
		if t.State == Paused {
			return "consent_stale", r.recoverPausedRollbacks(ctx, t, problem)
		}
		_, err := r.Triggers.Pause(ctx, t.ID, problem, "system:trigger")
		return "consent_stale", err
	}
	if t.State == Paused || t.ReadinessDisabled {
		if err := r.recoverPausedRollbacks(ctx, t, ""); err != nil || t.State == Paused {
			return "readiness_rollback", err
		}
	}
	if t.Claiming != "" {
		_, err := r.Triggers.Pause(ctx, t.ID, "The claim for "+t.Claiming+" was interrupted. Review the tracker and any task before resuming.", "system:trigger")
		return "claim_interrupted", err
	}
	if t.CurrentTask != "" {
		return r.followTask(ctx, t)
	}
	src, err := t.Source.Open()
	if err != nil {
		return "", err
	}
	call := r.checkedInvoker(t)
	heldCall := call
	holds := slices.Clone(t.Holds)
	if t.Source.Kind == "github" {
		call = r.boundGitHubSearch(t, call)
		heldCall = boundHeldGitHubSearch(t, call)
		slices.SortStableFunc(holds, func(a, b Hold) int { return a.ScopeCheckedAt.Compare(b.ScopeCheckedAt) })
	}
	var heldErr error
	if !t.ReadinessDisabled {
		for _, hold := range holds {
			started, err := r.checkHeld(ctx, t, src, heldCall, hold)
			if started || errors.Is(err, errTriggerChanged) {
				return "readiness_return", err
			}
			if err != nil {
				heldErr = errors.New("one or more held issues could not be checked or updated")
			}
		}
		if len(t.Holds) >= maxReadinessHolds {
			return "readiness_capacity", heldErr
		}
	}
	m, err := match(ctx, t, src, call)
	if err != nil {
		return "", err
	}
	if _, err := r.Triggers.Update(ctx, t.ID, "system:trigger", func(current *Trigger) error {
		current.Skipped = m.Skipped
		return nil
	}); err != nil {
		return "", err
	}
	if m.Next == nil {
		return "idle", heldErr
	}
	if r.Tasks == nil {
		return "", errors.New("no task starter is configured")
	}
	i, err := trackerRead[Issue](ctx, call, trackerGet, trackerArgs{Key: m.Next.Key})
	if err != nil {
		return "", err
	}
	if Reason(t, i) != "" {
		return "eligibility_changed", heldErr
	}
	if t.ReadinessDisabled {
		return r.claimAndStart(ctx, t, call, i)
	}
	h := Hold{Key: i.Key, URL: i.URL, Scope: holdScope(t), OpenedAt: r.Triggers.now()}
	if progress, ok := t.RetainedReadiness[readinessProgressKey(t, i.Key)]; ok {
		h.Round, h.SnapshotEdits, h.Intervention = progress.Round, progress.SnapshotEdits, progress.Intervention
		if progress.State != "" {
			h.State, h.Reason, h.Publication = progress.State, progress.Reason, "label_pending_only"
			h.Removals = selectionRemovals(t, i, false)
			if err := r.saveHold(ctx, t, h); err != nil {
				return "readiness_held", err
			}
			return "readiness_held", r.publishHold(ctx, t, call, h)
		}
	}
	started, err := r.assessIssue(ctx, t, call, i, h, nil, true)
	if started {
		return "readiness_admitted", err
	}
	return "readiness_held", errors.Join(err, heldErr)
}

var errTriggerChanged = errors.New("trigger changed during polling")

func (r *Runner) ensureCurrent(ctx context.Context, t *Trigger) error {
	return r.ensureTriggerState(ctx, t, Active)
}

func (r *Runner) ensureTriggerState(ctx context.Context, t *Trigger, state string) error {
	current, err := r.Triggers.Get(ctx, t.ID)
	if err != nil {
		return err
	}
	if current.State != state || current.Consent.Scope != t.Consent.Scope || current.Consent.RecordedAt != t.Consent.RecordedAt {
		return errTriggerChanged
	}
	problem, err := r.Triggers.ConsentProblem(ctx, current)
	if err != nil {
		return err
	}
	if problem != "" {
		return errors.New(problem)
	}
	return nil
}

func (r *Runner) checkedInvoker(t *Trigger) Invoker {
	return r.invokerForState(t, Active)
}

func (r *Runner) invokerForState(t *Trigger, state string) Invoker {
	call := GatewayInvoker(r.Gateway, Principal(t))
	return func(ctx context.Context, name string, args any) (*tools.Result, error) {
		if err := r.ensureTriggerState(ctx, t, state); err != nil {
			return nil, err
		}
		ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		return call(context.WithValue(ctx, trackerScopeKey{}, t), name, args)
	}
}

// TaskURL links a task in the console.
func (r *Runner) TaskURL(taskID string) string {
	return strings.TrimRight(r.BaseURL, "/") + "/#/tasks/" + taskID
}

// claimAndStart takes one eligible issue and starts its task under current
// trigger consent. Tracker writes use only the related selection and state labels.
func (r *Runner) claimAndStart(ctx context.Context, t *Trigger, call Invoker, i Issue) (string, error) {
	if r.Tasks == nil {
		return "", errors.New("no task starter is configured")
	}
	admission := "trigger-admissions/" + triggerFingerprint(struct {
		Source, Cloud, Issue, Workflow string
	}{i.Source, t.Source.CloudID, strings.ToLower(i.Key), t.Workflow})
	owner := r.Instance + "/" + t.ID
	ok, err := r.Store.AcquireLease(ctx, admission, owner, 3*time.Minute)
	if err != nil || !ok {
		return "claim_busy", err
	}
	defer func() {
		if err := r.Store.ReleaseLease(context.WithoutCancel(ctx), admission, owner); err != nil {
			r.log().WarnContext(ctx, "trigger admission lease release failed", "trigger", t.ID)
		}
	}()
	if _, err := r.Store.GetSetting(ctx, admission); err == nil {
		_, err := r.Triggers.Update(ctx, t.ID, "system:trigger", func(current *Trigger) error {
			finishClaim(current, DedupKey(t, i), i.Key, "prior admission")
			return nil
		})
		return "already_admitted", err
	} else if !errors.Is(err, storage.ErrNotFound) {
		return "claim_busy", err
	}
	// Write the admission intent before modifying tracker state. A crash
	// or ambiguous write requires reconciliation, never a second start.
	if _, err := r.Triggers.Update(ctx, t.ID, "system:trigger", func(current *Trigger) error {
		if current.State != Active || current.Consent.Scope != t.Consent.Scope || current.Consent.RecordedAt != t.Consent.RecordedAt || current.Claiming != "" {
			return errTriggerChanged
		}
		current.Claiming, current.ClaimAdmission = i.Key, admission
		return nil
	}); err != nil {
		return "claim_busy", err
	}
	holdIndex := t.holdIndex(i.Key)
	remove := claimRemovals(t, i, holdIndex >= 0 && t.Holds[holdIndex].Signal != "")
	if err := relabel(ctx, call, i.Key, remove, LabelInProgress, t.Owner); err != nil {
		return "", errors.New("issue claim could not be confirmed")
	}
	if err := r.ensureCurrent(ctx, t); err != nil {
		return "", err
	}
	if i.EvaluatedInput != "" {
		h := t.Holds[holdIndex]
		current, err := trackerRead[Issue](ctx, call, trackerGet, trackerArgs{Key: i.Key})
		var replies []issueComment
		if err == nil {
			replies, err = r.currentReplies(ctx, t, call, h, current)
		}
		edited := err == nil && readinessInput(current, replies) != i.EvaluatedInput
		if err != nil || issueReason(t, current) != "" || edited {
			h.State, h.Reason = "recheck", "eligibility_changed"
			if edited {
				recordSnapshotInvalidation(&h)
			}
			h.Publication = "label_pending_only"
			if errors.Is(err, errTrackerHistoryLimit) {
				h.State, h.Reason = "operator", "history_limit"
			} else if err != nil {
				now := r.Triggers.now()
				recordReadinessRetry(&h, now, "claim_recheck_unavailable", err)
				if deadline := trackerRetryDeadline(now, err); !deadline.IsZero() {
					h.RetryLabelAt = &deadline
				}
			} else if edited && h.State == "operator" {
				h.prepareComment()
			}
			h.Removals = []string{LabelInProgress}
			if i.Assignee == "" {
				h.UndoOwner = t.Owner
			}
			if err := r.saveHoldState(ctx, t, h, true); err != nil {
				return "readiness_changed", err
			}
			return "readiness_changed", r.publishHold(ctx, t, call, h)
		}
	}
	if err := r.ensureCurrent(ctx, t); err != nil {
		return "", err
	}
	if _, err := r.Store.PutSetting(ctx, admission, []byte(t.ID), 0, "system:trigger"); err != nil {
		return "", err
	}
	key := DedupKey(t, i)
	taskID, startErr := r.Tasks.Start(ctx, t, i)
	if startErr != nil {
		reason := "Weft failure. Weft could not start a task for this issue. Review the task and trigger, then resume the trigger."
		writeErr := errors.Join(relabel(ctx, call, i.Key, []string{LabelInProgress}, LabelFailed, ""), comment(ctx, call, i.Key, reason))
		_, err := r.Triggers.Update(ctx, t.ID, "system:trigger", func(t *Trigger) error {
			finishClaim(t, key, i.Key, "failed")
			t.State, t.PausedReason = Paused, "Weft could not start a task for "+i.Key+". Review the task and trigger before resuming."
			return nil
		})
		return "start_failed", errors.Join(writeErr, err)
	}
	if _, err := r.Triggers.Update(ctx, t.ID, "system:trigger", func(current *Trigger) error {
		finishClaim(current, key, i.Key, taskID)
		current.CurrentTask = taskID
		current.CurrentIssue = &Issue{Source: i.Source, Key: i.Key, URL: i.URL}
		current.CurrentSource = t.Source
		return nil
	}); err != nil {
		return "", err
	}
	if err := comment(ctx, call, i.Key, fmt.Sprintf("Weft started working on this issue: %s", r.TaskURL(taskID))); err != nil {
		if _, saveErr := r.Triggers.Update(ctx, t.ID, "system:trigger", func(t *Trigger) error {
			t.Notes = append(t.Notes, "The start comment could not be confirmed for "+i.Key+".")
			if len(t.Notes) > 20 {
				t.Notes = t.Notes[len(t.Notes)-20:]
			}
			return nil
		}); saveErr != nil {
			return "started", saveErr
		}
	}
	return "started", nil
}

func finishClaim(t *Trigger, key, issue, result string) {
	t.Claiming, t.ClaimAdmission = "", ""
	if t.Processed == nil {
		t.Processed = map[string]string{}
	}
	t.Processed[key] = result
	delete(t.RetainedReadiness, key)
	if idx := t.holdIndex(issue); idx >= 0 {
		t.Holds = slices.Delete(t.Holds, idx, idx+1)
	}
	if len(t.Holds) < maxReadinessHolds {
		t.ReadinessBlock = ""
	}
}

// Tasks creates and observes the tasks a trigger starts.
type Tasks interface {
	// Start creates a task for the issue under the trigger's consent and
	// starts it, returning the task id.
	Start(ctx context.Context, t *Trigger, i Issue) (string, error)
	// Status reports a task's outcome: State is running, succeeded, failed
	// or cancelled; PullRequestURL is set when its hand-off opened one.
	Status(ctx context.Context, taskID string) (TaskStatus, error)
}

// TaskStatus is what a trigger needs to know about its task.
type TaskStatus struct {
	State          string
	Reason         string
	PullRequestURL string
}

// followTask writes the outcome of the trigger's running task back to its
// issue once the task ends: on success a comment with the pull request,
// the weft:review label and Jira In Review when available; on failure or
// stop a comment with the reason, the weft:failed label, and the trigger
// pauses so a person can look before more issues are taken.
func (r *Runner) followTask(ctx context.Context, t *Trigger) (string, error) {
	if r.Tasks == nil || t.CurrentIssue == nil {
		return "", errors.New("active trigger task state is unavailable")
	}
	if t.CurrentSource != t.Source {
		_, err := r.Triggers.Pause(ctx, t.ID, "Restore the original tracker scope to write back the current task.", "system:trigger")
		return "writeback_scope_changed", err
	}
	st, err := r.Tasks.Status(ctx, t.CurrentTask)
	if err != nil {
		return "", errors.New("trigger task status is unavailable")
	}
	if st.State == "running" {
		return "busy", nil
	}
	call := r.checkedInvoker(t)
	i := *t.CurrentIssue
	link := r.TaskURL(t.CurrentTask)
	add, outcome := LabelFailed, "task_failed"
	message := "Weft failure. The task ended " + st.State + ". Review the task and resume the trigger when it is ready for more issues.\n\nTask: " + link
	if st.State == "succeeded" {
		if st.PullRequestURL != "" {
			add, outcome = LabelReview, "completed"
			message = fmt.Sprintf("Weft opened a pull request for this issue: %s\n\nTask: %s", st.PullRequestURL, link)
		} else {
			message = "Weft delivery failure. The task finished without an open pull request. Review delivery and resume the trigger.\n\nTask: " + link
		}
	}
	writeErr := errors.Join(relabel(ctx, call, i.Key, []string{LabelInProgress}, add, ""), comment(ctx, call, i.Key, message))
	_, err = r.Triggers.Update(ctx, t.ID, "system:trigger", func(current *Trigger) error {
		current.CurrentTask, current.CurrentIssue, current.CurrentSource = "", nil, SourceConfig{}
		if add == LabelFailed || writeErr != nil {
			current.State = Paused
			current.PausedReason = "Review delivery for " + i.Key + " and resume the trigger."
			if writeErr != nil {
				current.PausedReason = "Tracker write-back for " + i.Key + " could not be confirmed. Review the tracker before resuming."
			}
		}
		return nil
	})
	return outcome, errors.Join(writeErr, err)
}

// A paused trigger can finish an already recorded rollback under current
// consent. It performs no discovery, assessment, comment or admission.
func pausedRollbackDue(t *Trigger, now time.Time) bool {
	return t.State == Paused && slices.ContainsFunc(t.Holds, func(h Hold) bool {
		return holdHasPendingWrite(h) && slices.Contains(h.Removals, LabelInProgress) &&
			(h.RetryLabelAt == nil || !now.Before(*h.RetryLabelAt))
	})
}

func (r *Runner) recoverPausedRollbacks(ctx context.Context, t *Trigger, consentProblem string) error {
	for _, h := range slices.Clone(t.Holds) {
		if !holdHasPendingWrite(h) || !slices.Contains(h.Removals, LabelInProgress) {
			continue
		}
		if consentProblem != "" || h.Scope != holdScope(t) || (h.UndoOwner != "" && h.UndoOwner != t.Owner) {
			if err := r.failHoldLabels(ctx, t, h, "Current trigger consent does not cover the pending rollback."); err != nil {
				return err
			}
		} else if err := r.publishHoldLabels(ctx, t, r.invokerForState(t, t.State), h); err != nil {
			return err
		}
	}
	return nil
}
