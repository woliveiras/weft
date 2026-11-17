package environment

import (
	"context"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/sandboxcapacity"
)

func TestProgressPersistedDuringCapacityWait(t *testing.T) {
	h := capacityHarness(t)
	ctx := context.Background()
	if err := h.m.opt.Capacity.Reserve(ctx, "other-task", 64<<20, func(context.Context) ([]sandboxcapacity.Container, error) { return nil, nil }); err != nil {
		t.Fatal(err)
	}
	g := graphWith(&domain.EnvironmentSpec{Image: "alpine:3.22"})
	if _, err := h.m.Build(ctx, g, false, "test"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(8 * time.Second)
	seen := false
	for time.Now().Before(deadline) {
		st, err := h.m.load(ctx, g.Metadata.ID)
		if err != nil {
			t.Fatal(err)
		}
		if st.Phase == "waiting_capacity" {
			seen = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !seen {
		t.Fatal("waiting phase not persisted")
	}
	if err := h.m.opt.Capacity.Release(ctx, "other-task"); err != nil {
		t.Fatal(err)
	}
	v := h.wait(t, g)
	if v.State != StateReady || v.Phase != "" || v.ReuseDecision != "private" || len(v.BuildReasons) != 1 || v.BuildReasons[0] != "not_built" {
		t.Fatalf("ready: %+v", v)
	}
	g.Spec.Environment.Image = "alpine:other"
	v, err := h.m.Get(ctx, g)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.ChangeReasons) != 1 || v.ChangeReasons[0] != "definition_changed" {
		t.Fatalf("stale: %+v", v)
	}
	h.docker.set(t, "fail-buildx-build", "")
	if _, err := h.m.Build(ctx, g, true, "test"); err != nil {
		t.Fatal(err)
	}
	v = h.wait(t, g)
	if v.State != StateFailed || v.Phase != "" || v.BuildReasons[0] != "explicit_rebuild" {
		t.Fatalf("failure: %+v", v)
	}
}
