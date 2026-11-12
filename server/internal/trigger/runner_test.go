package trigger_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/events"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/tools"
	"github.com/woliveiras/weft/server/internal/trigger"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

// runner wires a trigger runner to a real gateway whose GitHub tools are
// served by tr (a fresh empty tracker when nil).
func runner(st storage.Store, c *clock, instance string, tr ...*tracker) *trigger.Runner {
	bus := events.NewBus(st, slog.New(slog.NewTextHandler(io.Discard, nil)))
	gh := newTracker("gh")
	if len(tr) > 0 {
		gh = tr[0]
	}
	reg := tools.NewRegistry()
	reg.MustRegister(gh.tools("search_issues")...)
	reg.MustRegister(trigger.TrackerTools(gh.request)...)
	return &trigger.Runner{Triggers: trigger.Manager{Store: st, Bus: bus, Now: c.now}, Store: st, Instance: instance,
		Gateway: tools.NewGateway(reg, nil, bus), Assessor: assessFunc(ready), Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

func TestDue(t *testing.T) {
	now := time.Now()
	past, future := now.Add(-time.Minute), now.Add(time.Minute)
	for _, tc := range []struct {
		tr   trigger.Trigger
		want bool
	}{
		{trigger.Trigger{State: trigger.Active}, true},
		{trigger.Trigger{State: trigger.Active, NextRunAt: &past}, true},
		{trigger.Trigger{State: trigger.Active, NextRunAt: &now}, true},
		{trigger.Trigger{State: trigger.Active, NextRunAt: &future}, false},
		{trigger.Trigger{State: trigger.Paused}, false},
		{trigger.Trigger{State: trigger.Finished}, false},
	} {
		if got := trigger.Due(&tc.tr, now); got != tc.want {
			t.Errorf("%+v: %v", tc.tr, got)
		}
	}
}

func TestRunnerSchedulesOnceAndCollapsesMissedTimes(t *testing.T) {
	st := triggerStore(t)
	c := &clock{t: time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)}
	r := runner(st, c, "a")
	tr, err := r.Triggers.Create(t.Context(), input(), "alice")
	if err != nil {
		t.Fatal(err)
	}
	r.Tick(t.Context())
	got, _ := r.Triggers.Get(t.Context(), tr.ID)
	if got.LastRunAt == nil || !got.LastRunAt.Equal(c.t) || !got.NextRunAt.Equal(c.t.Add(15*time.Minute)) {
		t.Fatalf("first run: %+v", got)
	}
	// Not due yet: nothing happens.
	c.t = c.t.Add(5 * time.Minute)
	r.Tick(t.Context())
	if again, _ := r.Triggers.Get(t.Context(), tr.ID); !again.LastRunAt.Equal(*got.LastRunAt) {
		t.Fatal("ran before it was due")
	}
	// Ten intervals later it runs once, and is next due one interval later.
	c.t = c.t.Add(150 * time.Minute)
	r.Tick(t.Context())
	got, _ = r.Triggers.Get(t.Context(), tr.ID)
	if !got.LastRunAt.Equal(c.t) || !got.NextRunAt.Equal(c.t.Add(15*time.Minute)) {
		t.Fatalf("missed times: %+v", got)
	}
	evs, _ := st.ListEvents(t.Context(), storage.EventFilter{Types: []domain.EventType{domain.EvTriggerRun}})
	if len(evs) != 2 {
		t.Fatalf("runs = %d", len(evs))
	}
}

func TestRunnerLeaseKeepsOneInstancePerTrigger(t *testing.T) {
	st := triggerStore(t)
	c := &clock{t: time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)}
	a, b := runner(st, c, "a"), runner(st, c, "b")
	tr, _ := a.Triggers.Create(t.Context(), input(), "alice")
	// Instance b holds the lease (for example, mid-run): a skips it.
	if ok, err := st.AcquireLease(t.Context(), "trigger/"+tr.ID, "b", time.Minute); !ok || err != nil {
		t.Fatal(ok, err)
	}
	a.Tick(t.Context())
	if got, _ := a.Triggers.Get(t.Context(), tr.ID); got.LastRunAt != nil {
		t.Fatal("a ran a trigger b held")
	}
	_ = st.ReleaseLease(t.Context(), "trigger/"+tr.ID, "b")
	b.Tick(t.Context())
	a.Tick(t.Context()) // already ran: not due
	evs, _ := st.ListEvents(t.Context(), storage.EventFilter{Types: []domain.EventType{domain.EvTriggerRun}})
	if len(evs) != 1 {
		t.Fatalf("runs = %d", len(evs))
	}
}

func TestRunnerPausesOnStaleConsent(t *testing.T) {
	st := triggerStore(t)
	c := &clock{t: time.Now().UTC()}
	r := runner(st, c, "a")
	tr, _ := r.Triggers.Create(t.Context(), input(), "alice")
	g, _ := st.GetGraph(t.Context(), "factory", 0)
	if _, err := st.SaveGraph(t.Context(), g, 1); err != nil {
		t.Fatal(err)
	}
	r.Tick(t.Context())
	got, _ := r.Triggers.Get(t.Context(), tr.ID)
	if got.State != trigger.Paused || got.PausedReason == "" {
		t.Fatalf("stale consent must pause: %+v", got)
	}
	// Paused triggers are not run.
	c.t = c.t.Add(time.Hour)
	r.Tick(t.Context())
	evs, _ := st.ListEvents(t.Context(), storage.EventFilter{Types: []domain.EventType{domain.EvTriggerRun}})
	if len(evs) != 1 {
		t.Fatalf("runs = %d", len(evs))
	}
}

type assessFunc func(context.Context, *trigger.Trigger, string) trigger.ReadinessAssessment

func (f assessFunc) Assess(ctx context.Context, t *trigger.Trigger, input string) trigger.ReadinessAssessment {
	return f(ctx, t, input)
}
func ready(context.Context, *trigger.Trigger, string) trigger.ReadinessAssessment {
	return trigger.ReadinessAssessment{}
}
