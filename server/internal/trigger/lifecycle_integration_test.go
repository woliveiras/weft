package trigger_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/jiradc"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/tools"
	"github.com/woliveiras/weft/server/internal/trigger"
)

func nativeMapping(t *testing.T, m jiradc.Connections, name, from, tr, target string) trigger.MappingPin {
	t.Helper()
	v, err := (jiradc.WorkflowMappings{Connections: m}).Put(t.Context(), jiradc.MappingInput{Name: name, Connection: "jira-native", ConnectionRevision: 1, SampleIssueKey: "APP-7", SampleIssueID: "10007", IssueTypeID: "10002", FromStatusID: from, TransitionID: tr, TargetStatusID: target, IssueUpdated: "2026-11-20T09:00:00.000+0000", Enabled: true}, 0, "admin")
	if err != nil {
		t.Fatal(err)
	}
	return trigger.MappingPin{Name: v.Name, Revision: v.Revision}
}
func TestNativeLifecyclePinsOptionalTransitionsToActualTaskEvidence(t *testing.T) {
	r, c, fake, f, m, in := nativeRunner(t)
	work := nativeMapping(t, m, "working", "10", "32", "15")
	f.transitionStatus = "15"
	review := nativeMapping(t, m, "reviewing", "15", "31", "20")
	f.transitionStatus = "10"
	in.LifecycleMappings = trigger.LifecycleMappings{WorkStarted: work, PRDelivered: review}
	tr, err := r.Triggers.Create(t.Context(), in, "admin")
	if err != nil {
		t.Fatal(err)
	}
	tr = advance(r, c, t, tr.ID)
	if tr.CurrentIssue.ID != "10007" || tr.CurrentIssue.IssueTypeID != "10002" || tr.CurrentMappings != in.LifecycleMappings || f.transitionPosts != 0 {
		t.Fatal("admission treated as runtime start", tr, f.transitionPosts)
	}
	task := tr.CurrentTask
	tr = advance(r, c, t, tr.ID)
	rows, err := r.Triggers.ListLifecycleSyncs(t.Context())
	if err != nil || len(rows) != 0 || f.transitionPosts != 0 {
		t.Fatal("waiting task transitioned", rows, err)
	}
	fake.status[task] = trigger.TaskStatus{State: "running", WorkExecutionID: "exe-runtime-admitted"}
	tr = advance(r, c, t, tr.ID)
	rows, err = r.Triggers.ListLifecycleSyncs(t.Context())
	if err != nil || len(rows) != 1 || rows[0].State != "target_observed" || rows[0].Evidence != "exe-runtime-admitted" || f.transitionPosts != 1 || f.transitionStatus != "15" {
		t.Fatal(rows, err, f.transitionPosts)
	}
	fake.status[task] = trigger.TaskStatus{State: "succeeded", WorkExecutionID: "exe-runtime-admitted", DeliveryEventID: "evt-opened", PullRequestURL: "https://bitbucket.example/projects/TEAM/repos/app/pull-requests/2"}
	tr = advance(r, c, t, tr.ID)
	rows, err = r.Triggers.ListLifecycleSyncs(t.Context())
	if err != nil || len(rows) != 2 || tr.CurrentTask != "" || f.transitionPosts != 2 || f.transitionStatus != "20" || !slices.Contains(f.labels, trigger.LabelReview) {
		t.Fatal(rows, err, tr, f.labels)
	}
	for _, row := range rows {
		if row.State != "target_observed" || row.TaskID != task {
			t.Fatal(row)
		}
	}
	advance(r, c, t, tr.ID)
	if f.transitionPosts != 2 || len(fake.started) != 1 {
		t.Fatal("duplicate work", f.transitionPosts, len(fake.started))
	}
	// A direct agent grant does not provide the private trigger-only context.
	reg := tools.NewRegistry()
	reg.MustRegister(trigger.TrackerTools(nil, m)...)
	tool, _ := reg.Get("tracker.transition")
	res, err := tool.Invoke(t.Context(), &tools.Invocation{Principal: trigger.Principal(tr), Args: []byte(`{"syncId":"` + rows[0].ID + `"}`)})
	if !errors.Is(err, tools.ErrDenied) || res != nil || f.transitionPosts != 2 {
		t.Fatal("agent forged lifecycle scope", res, err)
	}
}
func TestNativeLifecycleMappingFailurePreservesDeliveredTaskAndLabels(t *testing.T) {
	for _, mode := range []string{"disabled", "remote-denied", "lost-response", "both-milestones"} {
		t.Run(mode, func(t *testing.T) {
			r, c, fake, f, m, in := nativeRunner(t)
			in.LifecycleMappings.PRDelivered = nativeMapping(t, m, "reviewing", "10", "31", "20")
			if mode == "both-milestones" {
				in.LifecycleMappings.WorkStarted = nativeMapping(t, m, "working", "10", "32", "15")
				// The delivered mapping must start from the state reached by work_started.
				mapped, _ := (jiradc.WorkflowMappings{Connections: m}).Get(t.Context(), "reviewing")
				input := mapped.MappingInput
				input.FromStatusID = "15"
				f.transitionStatus = "15"
				mapped, err := (jiradc.WorkflowMappings{Connections: m}).Put(t.Context(), input, mapped.Revision, "admin")
				if err != nil {
					t.Fatal(err)
				}
				in.LifecycleMappings.PRDelivered.Revision = mapped.Revision
				f.transitionStatus = "10"
			}
			tr, err := r.Triggers.Create(t.Context(), in, "admin")
			if err != nil {
				t.Fatal(err)
			}
			tr = advance(r, c, t, tr.ID)
			task := tr.CurrentTask
			switch mode {
			case "disabled":
				v, _ := (jiradc.WorkflowMappings{Connections: m}).Get(t.Context(), "reviewing")
				v.Enabled = false
				if _, err = (jiradc.WorkflowMappings{Connections: m}).Put(t.Context(), v.MappingInput, v.Revision, "admin"); err != nil {
					t.Fatal(err)
				}
			case "remote-denied":
				f.transitionFailure = true
			case "lost-response":
				f.transitionLostResponse = true
			}
			fake.status[task] = trigger.TaskStatus{State: "succeeded", WorkExecutionID: "exe-admitted", DeliveryEventID: "evt-pr", PullRequestURL: "https://bitbucket.example/pr/2"}
			tr = advance(r, c, t, tr.ID)
			if tr.CurrentTask != "" || tr.State != trigger.Active || !slices.Contains(f.labels, trigger.LabelReview) || len(fake.started) != 1 {
				t.Fatal("optional sync changed delivery", tr, f.labels)
			}
			rows, err := r.Triggers.ListLifecycleSyncs(t.Context())
			if err != nil || len(rows) == 0 {
				t.Fatal(rows, err)
			}
			if mode == "both-milestones" {
				if f.transitionPosts != 2 || f.transitionStatus != "20" {
					t.Fatal("lifecycle order", rows, f.transitionStatus)
				}
			} else {
				state := "failed"
				if mode == "lost-response" {
					state = "unknown"
				}
				if rows[0].State != state || !strings.Contains(rows[0].Error, "review") {
					t.Fatal(rows)
				}
				before := f.transitionPosts
				advance(r, c, t, tr.ID)
				if f.transitionPosts != before {
					t.Fatal("uncertain/failed stage automatically repeated")
				}
			}
		})
	}
}

type lifecycleJournalFault struct{ storage.Store }

func (s lifecycleJournalFault) PutSettingIf(ctx context.Context, key string, value []byte, revision int64, actor string, guards []storage.SettingRevision) (int64, error) {
	if strings.HasPrefix(key, trigger.LifecycleSyncPrefix) {
		return 0, errors.New("synthetic journal unavailable")
	}
	return s.Store.PutSettingIf(ctx, key, value, revision, actor, guards)
}
func TestNativeLifecycleJournalFailureRetainsCurrentTaskForRecovery(t *testing.T) {
	r, c, fake, f, m, in := nativeRunner(t)
	in.LifecycleMappings.PRDelivered = nativeMapping(t, m, "reviewing", "10", "31", "20")
	tr, err := r.Triggers.Create(t.Context(), in, "admin")
	if err != nil {
		t.Fatal(err)
	}
	tr = advance(r, c, t, tr.ID)
	task := tr.CurrentTask
	fake.status[task] = trigger.TaskStatus{State: "succeeded", DeliveryEventID: "evt-pr", PullRequestURL: "https://bitbucket.example/pr/2"}
	original := r.Triggers.Store
	r.Triggers.Store = lifecycleJournalFault{Store: original}
	tr = advance(r, c, t, tr.ID)
	if tr.CurrentTask != task || f.transitionPosts != 0 || slices.Contains(f.labels, trigger.LabelReview) {
		t.Fatal("journal failure dropped evidence", tr)
	}
	r.Triggers.Store = original
	tr = advance(r, c, t, tr.ID)
	if tr.CurrentTask != "" || f.transitionPosts != 1 || len(fake.started) != 1 {
		t.Fatal("recovery duplicated/dropped task", tr, f.transitionPosts)
	}
}
func TestNativeLifecycleConsentRejectsUnreviewedPinsAndCannotEditAdmittedPins(t *testing.T) {
	r, c, _, _, m, in := nativeRunner(t)
	pin := nativeMapping(t, m, "reviewing", "10", "31", "20")
	in.LifecycleMappings.PRDelivered = pin
	bad := in
	bad.LifecycleMappings.PRDelivered.Revision = 2
	if _, err := r.Triggers.Create(t.Context(), bad, "admin"); !errors.Is(err, storage.ErrConflict) {
		t.Fatal("stale mapping consent", err)
	}
	tr, err := r.Triggers.Create(t.Context(), in, "admin")
	if err != nil {
		t.Fatal(err)
	}
	tr = advance(r, c, t, tr.ID)
	if _, err = r.Triggers.Edit(t.Context(), tr.ID, bad, "admin"); !errors.Is(err, storage.ErrConflict) {
		t.Fatal("admitted pins changed", err)
	}
	bad = in
	bad.LifecycleMappings.PRDelivered = nativeMapping(t, m, "other-review", "10", "31", "20")
	if _, err = r.Triggers.ApplyConfigurationAt(t.Context(), tr.ID, bad, "admin", true, ""); !errors.Is(err, storage.ErrConflict) {
		t.Fatal("declarative apply changed admitted pins", err)
	}

}

func TestNativeLifecycleRecoveryReconcilesUnknownAndRetriesOnlyPreflightFailure(t *testing.T) {
	for _, lost := range []bool{false, true} {
		t.Run(fmt.Sprint(lost), func(t *testing.T) {
			r, c, fake, f, m, in := nativeRunner(t)
			in.LifecycleMappings.PRDelivered = nativeMapping(t, m, "reviewing", "10", "31", "20")
			tr, err := r.Triggers.Create(t.Context(), in, "admin")
			if err != nil {
				t.Fatal(err)
			}
			tr = advance(r, c, t, tr.ID)
			f.transitionLostResponse = lost
			f.transitionFailure = !lost
			fake.status[tr.CurrentTask] = trigger.TaskStatus{State: "succeeded", DeliveryEventID: "evt-pr", PullRequestURL: "https://bitbucket.example/pr/2"}
			tr = advance(r, c, t, tr.ID)
			rows, err := r.Triggers.ListLifecycleSyncs(t.Context())
			if err != nil || len(rows) != 1 {
				t.Fatal(rows, err)
			}
			v := rows[0]
			if _, err = r.Triggers.RetryLifecycleSync(t.Context(), v.ID, v.Revision+1, "admin"); !errors.Is(err, storage.ErrConflict) {
				t.Fatal("stale recovery", err)
			}
			f.transitionFailure = false
			if lost {
				if _, err = r.Triggers.RetryLifecycleSync(t.Context(), v.ID, v.Revision, "admin"); !errors.Is(err, storage.ErrConflict) {
					t.Fatal("unknown POST retry", err)
				}
				mapping, _ := (jiradc.WorkflowMappings{Connections: m}).Get(t.Context(), "reviewing")
				mapping.Enabled = false
				if _, err = (jiradc.WorkflowMappings{Connections: m}).Put(t.Context(), mapping.MappingInput, mapping.Revision, "admin"); err != nil {
					t.Fatal(err)
				}
				v, err = r.Triggers.ReconcileLifecycleSync(t.Context(), v.ID, v.Revision, "operator")
				if err != nil || v.State != "target_observed" || f.transitionPosts != 1 {
					t.Fatal(v, err, f.transitionPosts)
				}
			} else {
				v, err = r.Triggers.RetryLifecycleSync(t.Context(), v.ID, v.Revision, "admin")
				if err != nil || v.State != "queued" || v.RetryRevision < 1 {
					t.Fatal(v, err)
				}
				tr = advance(r, c, t, tr.ID)
				rows, err = r.Triggers.ListLifecycleSyncs(t.Context())
				if err != nil || rows[0].State != "target_observed" || f.transitionPosts != 1 || len(fake.started) != 1 {
					t.Fatal(rows, err)
				}
			}
		})
	}
}

func TestNativeLifecycleTwoWorkersSerializeTaskStages(t *testing.T) {
	r, c, fake, f, m, in := nativeRunner(t)
	in.LifecycleMappings.WorkStarted = nativeMapping(t, m, "working", "10", "32", "15")
	f.transitionStatus = "15"
	in.LifecycleMappings.PRDelivered = nativeMapping(t, m, "reviewing", "15", "31", "20")
	f.transitionStatus = "10"
	tr, err := r.Triggers.Create(t.Context(), in, "admin")
	if err != nil {
		t.Fatal(err)
	}
	tr = advance(r, c, t, tr.ID)
	fake.status[tr.CurrentTask] = trigger.TaskStatus{State: "succeeded", WorkExecutionID: "exe-work", DeliveryEventID: "evt-pr", PullRequestURL: "https://bitbucket.example/pr/2"}
	f.transitionEntered = make(chan struct{})
	f.transitionRelease = make(chan struct{})
	done := make(chan struct{})
	go func() { defer close(done); r.Tick(t.Context()) }()
	select {
	case <-f.transitionEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("work stage did not enter dispatch")
	}
	// A second instance, and even a concurrent Tick with the same instance name,
	// cannot acquire another stage under the first operation's unique lease holder.
	other := *r
	other.Instance = "second-controller"
	other.Tick(t.Context())
	r.Tick(t.Context())
	if f.transitionPosts != 1 {
		t.Errorf("review overtook work dispatch: %d", f.transitionPosts)
	}
	close(f.transitionRelease)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("first worker did not finish")
	}
	rows, err := r.Triggers.ListLifecycleSyncs(t.Context())
	if err != nil || len(rows) != 2 || f.transitionPosts != 2 || f.transitionStatus != "20" {
		t.Fatal(rows, err, f.transitionPosts, f.transitionStatus)
	}
	for _, v := range rows {
		if v.State != "target_observed" {
			t.Fatal(v)
		}
	}
}

func TestNativeLifecycleRetryCannotOvertakeDeliveryDispatch(t *testing.T) {
	r, c, fake, f, m, in := nativeRunner(t)
	in.LifecycleMappings.WorkStarted = nativeMapping(t, m, "working", "10", "32", "15")
	f.transitionStatus = "11"
	in.LifecycleMappings.PRDelivered = nativeMapping(t, m, "reviewing", "11", "31", "20")
	f.transitionStatus = "10"
	tr, err := r.Triggers.Create(t.Context(), in, "admin")
	if err != nil {
		t.Fatal(err)
	}
	tr = advance(r, c, t, tr.ID)
	// A human moves the issue before dispatch. Start fails preflight; delivery is
	// applicable to the new status and is held at its remote POST boundary.
	f.transitionStatus = "11"
	fake.status[tr.CurrentTask] = trigger.TaskStatus{State: "succeeded", WorkExecutionID: "exe-work", DeliveryEventID: "evt-pr", PullRequestURL: "https://bitbucket.example/pr/2"}
	f.transitionBlockID = "31"
	f.transitionEntered = make(chan struct{})
	f.transitionRelease = make(chan struct{})
	done := make(chan struct{})
	go func() { defer close(done); r.Tick(t.Context()) }()
	select {
	case <-f.transitionEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("delivery did not enter dispatch")
	}
	rows, err := r.Triggers.ListLifecycleSyncs(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var work trigger.LifecycleSync
	for _, row := range rows {
		if row.Stage == "work_started" {
			work = row
		}
	}
	if work.State != "failed" {
		t.Fatal(work)
	}
	if _, err = r.Triggers.RetryLifecycleSync(t.Context(), work.ID, work.Revision, "admin"); !errors.Is(err, storage.ErrConflict) {
		t.Error("retry overtook delivery", err)
	}
	close(f.transitionRelease)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("delivery did not finish")
	}
	if _, err = r.Triggers.RetryLifecycleSync(t.Context(), work.ID, work.Revision, "admin"); !errors.Is(err, storage.ErrConflict) {
		t.Fatal("late start retry accepted", err)
	}
	r.Tick(t.Context())
	if f.transitionPosts != 1 || f.transitionStatus != "20" {
		t.Fatal("start ran after delivery", f.transitionPosts, f.transitionStatus)
	}
}

func TestNativeLifecycleDisabledMappingCannotStrandPausedAdmittedTask(t *testing.T) {
	r, c, fake, f, m, in := nativeRunner(t)
	in.LifecycleMappings.PRDelivered = nativeMapping(t, m, "reviewing", "10", "31", "20")
	tr, err := r.Triggers.Create(t.Context(), in, "admin")
	if err != nil {
		t.Fatal(err)
	}
	tr = advance(r, c, t, tr.ID)
	if _, err = r.Triggers.Pause(t.Context(), tr.ID, "operator review", "admin"); err != nil {
		t.Fatal(err)
	}
	mapping, _ := (jiradc.WorkflowMappings{Connections: m}).Get(t.Context(), "reviewing")
	mapping.Enabled = false
	if _, err = (jiradc.WorkflowMappings{Connections: m}).Put(t.Context(), mapping.MappingInput, 1, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err = r.Triggers.Resume(t.Context(), tr.ID, "admin"); err != nil {
		t.Fatal("optional mapping stranded task delivery", err)
	}
	fake.status[tr.CurrentTask] = trigger.TaskStatus{State: "succeeded", DeliveryEventID: "evt-pr", PullRequestURL: "https://bitbucket.example/pr/2"}
	tr = advance(r, c, t, tr.ID)
	rows, err := r.Triggers.ListLifecycleSyncs(t.Context())
	if err != nil || tr.CurrentTask != "" || !slices.Contains(f.labels, trigger.LabelReview) || f.transitionPosts != 0 || len(rows) != 1 || rows[0].State != "failed" {
		t.Fatal(tr, rows, err)
	}
	// New consent without an admitted task must still explicitly review mappings.
	if _, err = r.Triggers.Resume(t.Context(), tr.ID, "admin"); !errors.Is(err, storage.ErrConflict) {
		t.Fatal("fresh scope silently adopted disabled mapping", err)
	}
}
