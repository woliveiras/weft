package trigger

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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
		if !Due(t, now) {
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
	if err != nil || !Due(t, r.Triggers.now()) {
		return // changed or ran elsewhere meanwhile
	}
	started := time.Now()
	outcome, err := r.step(ctx, t)
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
		_, err := r.Triggers.Pause(ctx, t.ID, problem, "system:trigger")
		return "consent_stale", err
	}
	if t.CurrentTask != "" {
		return r.followTask(ctx, t)
	}
	m, src, call, err := r.Match(ctx, t)
	if err != nil {
		return "", err
	}
	if _, err := r.Triggers.Update(ctx, t.ID, "system:trigger", func(t *Trigger) error {
		t.Skipped = m.Skipped
		return nil
	}); err != nil {
		return "", err
	}
	if m.Next == nil {
		return "idle", nil
	}
	if r.Tasks == nil {
		return "", errors.New("no task starter is configured")
	}
	return r.claimAndStart(ctx, t, src, call, *m.Next)
}

// TaskURL links a task in the console.
func (r *Runner) TaskURL(taskID string) string {
	return strings.TrimRight(r.BaseURL, "/") + "/#/tasks/" + taskID
}

// claimAndStart takes the issue (assign it to the owner and swap the
// trigger label for weft:in-progress), starts its task, says so on the
// issue with a link, and moves a Jira issue to In Progress when it can. A
// missing transition is noted, not a failure. If the task cannot start, the
// issue is marked failed and the trigger pauses.
func (r *Runner) claimAndStart(ctx context.Context, t *Trigger, src Source, call Invoker, i Issue) (string, error) {
	if err := src.Relabel(ctx, call, i, t.Label, LabelInProgress, t.Owner); err != nil {
		return "", fmt.Errorf("claim %s: %w", i.Key, err)
	}
	i.Labels = append(i.Labels, LabelInProgress)
	key := DedupKey(t, i)
	taskID, startErr := r.Tasks.Start(ctx, t, i)
	if startErr != nil {
		reason := "Weft could not start a task for this issue: " + startErr.Error()
		err := errors.Join(src.Relabel(ctx, call, i, LabelInProgress, LabelFailed, ""), src.Comment(ctx, call, i, reason))
		_, perr := r.Triggers.Update(ctx, t.ID, "system:trigger", func(t *Trigger) error {
			if t.Processed == nil {
				t.Processed = map[string]string{}
			}
			t.Processed[key] = "failed"
			t.State, t.PausedReason = Paused, fmt.Sprintf("The task for %s could not start: %s", i.Key, startErr)
			return nil
		})
		return "start_failed", errors.Join(err, perr)
	}
	if _, err := r.Triggers.Update(ctx, t.ID, "system:trigger", func(t *Trigger) error {
		if t.Processed == nil {
			t.Processed = map[string]string{}
		}
		t.Processed[key], t.CurrentTask, t.CurrentIssue = taskID, taskID, &i
		return nil
	}); err != nil {
		return "", err
	}
	comment := fmt.Sprintf("Weft started working on this issue: %s", r.TaskURL(taskID))
	if err := src.Comment(ctx, call, i, comment); err != nil {
		r.note(ctx, t.ID, taskID, fmt.Sprintf("The start comment could not be posted on %s: %s", i.Key, err))
	}
	moved, err := src.Transition(ctx, call, i, "In Progress")
	switch {
	case err != nil:
		r.note(ctx, t.ID, taskID, fmt.Sprintf("%s could not be moved to In Progress: %s", i.Key, err))
	case !moved && i.Source == "jira":
		r.note(ctx, t.ID, taskID, fmt.Sprintf("%s has no In Progress transition; its status was left unchanged.", i.Key))
	}
	return "started", nil
}

// note records something a person should know on the task (as an event on
// its run) and on the trigger, without failing the run.
func (r *Runner) note(ctx context.Context, triggerID, taskID, text string) {
	if r.Triggers.Bus != nil {
		r.Triggers.Bus.Publish(context.WithoutCancel(ctx), domain.NewEvent(domain.EvTriggerNote, domain.TaskRunGraphPrefix+strings.TrimPrefix(taskID, "tsk_"),
			map[string]any{"trigger": triggerID, "task": taskID, "message": text}).WithActor("system:trigger"))
	}
	_, _ = r.Triggers.Update(ctx, triggerID, "system:trigger", func(t *Trigger) error {
		t.Notes = append(t.Notes, text)
		if len(t.Notes) > 20 {
			t.Notes = t.Notes[len(t.Notes)-20:]
		}
		return nil
	})
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
	if r.Tasks == nil {
		return "", errors.New("no task starter is configured")
	}
	st, err := r.Tasks.Status(ctx, t.CurrentTask)
	if err != nil {
		return "", fmt.Errorf("task %s: %w", t.CurrentTask, err)
	}
	if st.State == "running" || t.CurrentIssue == nil {
		return "waiting", nil
	}
	src, err := t.Source.Open()
	if err != nil {
		return "", err
	}
	call := GatewayInvoker(r.Gateway, Principal(t, src))
	i := *t.CurrentIssue
	link := r.TaskURL(t.CurrentTask)
	var outcome string
	var writeErr error
	if st.State == "succeeded" {
		outcome = "completed"
		msg := "Weft finished this issue: " + link
		if st.PullRequestURL != "" {
			msg = fmt.Sprintf("Weft opened a pull request for this issue: %s\n\nTask: %s", st.PullRequestURL, link)
		}
		writeErr = errors.Join(src.Comment(ctx, call, i, msg), src.Relabel(ctx, call, i, LabelInProgress, LabelReview, ""))
		if moved, terr := src.Transition(ctx, call, i, "In Review"); terr != nil {
			r.note(ctx, t.ID, t.CurrentTask, fmt.Sprintf("%s could not be moved to In Review: %s", i.Key, terr))
		} else if !moved && i.Source == "jira" {
			r.note(ctx, t.ID, t.CurrentTask, fmt.Sprintf("%s has no In Review transition; its status was left unchanged.", i.Key))
		}
	} else {
		outcome = "task_failed"
		reason := st.Reason
		if reason == "" {
			reason = "the task " + st.State
		}
		writeErr = errors.Join(src.Comment(ctx, call, i, fmt.Sprintf("Weft stopped working on this issue: %s\n\nTask: %s", reason, link)),
			src.Relabel(ctx, call, i, LabelInProgress, LabelFailed, ""))
	}
	finished, ended := t.CurrentTask, st.State
	_, err = r.Triggers.Update(ctx, t.ID, "system:trigger", func(t *Trigger) error {
		t.CurrentTask, t.CurrentIssue = "", nil
		if ended != "succeeded" {
			t.State, t.PausedReason = Paused, fmt.Sprintf("The task %s for %s ended %s. Review it and resume the trigger to take more issues.", finished, i.Key, ended)
		}
		return nil
	})
	return outcome, errors.Join(writeErr, err)
}
