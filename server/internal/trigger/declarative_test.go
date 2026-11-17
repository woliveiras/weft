package trigger_test

import (
	"github.com/woliveiras/weft/server/internal/trigger"
	"testing"
)

func TestDeclarativeConsentIsAtomicAndExplicit(t *testing.T) {
	m := trigger.Manager{Store: triggerStore(t)}
	in := input()
	paused, err := m.ApplyConfiguration(ctx, "", in, "alice", false)
	if err != nil {
		t.Fatal(err)
	}
	if paused.State != trigger.Paused || paused.PausedReason != "consent required" || paused.Consent.Actor != "" || !paused.Consent.RecordedAt.IsZero() {
		t.Fatalf("fabricated consent: %+v", paused)
	}
	saved, err := m.Get(ctx, paused.ID)
	if err != nil || saved.State != trigger.Paused {
		t.Fatalf("stored state: %+v %v", saved, err)
	}
	active, err := m.ApplyConfiguration(ctx, paused.ID, in, "bob", true)
	if err != nil {
		t.Fatal(err)
	}
	if active.State != trigger.Active || active.Consent.Actor != "bob" || active.Consent.Authorization != in.Authorization || active.Consent.RecordedAt.IsZero() {
		t.Fatalf("missing explicit consent: %+v", active)
	}
	in.Authorization = ""
	if _, err = m.ApplyConfiguration(ctx, active.ID, in, "bob", true); err == nil {
		t.Fatal("implicit authorization accepted")
	}
}
