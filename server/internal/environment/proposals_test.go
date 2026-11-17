package environment

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/events"
	"github.com/woliveiras/weft/server/internal/hitl"
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/storage/fsstore"
)

func proposalFixture(t *testing.T, bin string) (*Proposals, *domain.Graph, string) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	store, err := fsstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	bus := events.NewBus(store, logger)
	manager, err := New(Options{Store: store, Bus: bus, Log: logger, Workspaces: fakeWorkspaces{dir}, DockerBin: bin})
	if err != nil {
		t.Fatal(err)
	}
	p := &Proposals{Store: store, Approvals: hitl.New(store, bus, logger), Manager: manager, Directory: func(*domain.Graph) (string, error) { return dir, nil }}
	manager.SetProposalValidator(p.ValidateAccepted)
	p.Save = func(ctx context.Context, g *domain.Graph, version int, _ string) error {
		_, err := store.SaveGraph(ctx, g, version)
		return err
	}
	p.Models = analysisModelFunc(func(context.Context, string, *llm.ChatRequest) (*llm.ChatResponse, error) {
		return &llm.ChatResponse{Message: llm.ChatMessage{Content: `{"environment":{"dockerfile":"FROM alpine:3.22\nCOPY package.json /manifest\nRUN echo proposal-ok > /proof\n"},"rationale":"Shell fixture with a copied manifest; setup network permits pulling Alpine.","tools":["sh"]}`}}, nil
	})
	g := &domain.Graph{Metadata: domain.GraphMetadata{ID: fmt.Sprintf("proposal-test-%d", time.Now().UnixNano()), Name: "proposal"}}
	if _, err := store.SaveGraph(ctx, g, 0); err != nil {
		t.Fatal(err)
	}
	if err := store.PutGraphRecord(ctx, &domain.GraphRecord{ID: g.Metadata.ID, State: domain.GraphStopped, DesiredState: domain.GraphStopped}); err != nil {
		t.Fatal(err)
	}
	writeDiscoveryFile(t, dir, "package.json", `{"name":"fixture"}`)
	return p, g, dir
}

func TestProposalRestartFailureRetryAndTampering(t *testing.T) {
	docker := newFakeDocker(t)
	p, g, _ := proposalFixture(t, docker.bin)
	ctx := context.Background()
	v, err := p.Request(ctx, g, domain.ModelRef{}, false, "developer")
	if err != nil {
		t.Fatal(err)
	}
	// Persisted pending definition cannot be built by forging graph JSON.
	weftd := *g
	weftd.Spec.Environment = v.Analysis.Environment
	if _, err := p.Manager.Build(ctx, &weftd, false, "agent"); err == nil {
		t.Fatal("built unapproved environment")
	}
	if len(docker.calls(t)) != 0 {
		t.Fatal("Docker ran before approval")
	}
	a, err := p.Store.GetApproval(ctx, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.ValidateDecision(ctx, a); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Approvals.Decide(ctx, v.ID, true, "developer", "", ""); err != nil {
		t.Fatal(err)
	}
	// Recreate service with only persisted records, as after a restart.
	restarted := *p
	p = &restarted
	p.Manager.SetProposalValidator(p.ValidateAccepted)
	docker.set(t, "fail-build", "yes")
	p.Reconcile(ctx)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		p.Reconcile(ctx)
		v, _ = p.Get(ctx, g.Metadata.ID)
		if v.State == "failed" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if v.State != "failed" || v.Error == "" {
		t.Fatalf("failure not visible %+v", v)
	}
	if err := os.Remove(docker.dir + "/fail-build"); err != nil {
		t.Fatal(err)
	}
	if err := p.Apply(ctx, g.Metadata.ID); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		p.Reconcile(ctx)
		v, _ = p.Get(ctx, g.Metadata.ID)
		if v.State == "ready" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if v.State != "ready" {
		t.Fatalf("retry %+v", v)
	}
	saved, err := p.Store.GetGraph(ctx, g.Metadata.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	saved.Spec.Environment.Setup = []string{"echo changed"}
	if err := p.ValidateAccepted(ctx, saved); err == nil {
		t.Fatal("approved definition tampered")
	}
}

func TestProposalRealSandbox(t *testing.T) {
	bin := os.Getenv("WEFT_TEST_ENV_DOCKER_BIN")
	if bin == "" {
		t.Skip("run make test-environment-context")
	}
	p, g, _ := proposalFixture(t, bin)
	ctx := context.Background()
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if err := p.Manager.Reset(cleanup, g.Metadata.ID, "test"); err != nil {
			t.Error(err)
		}
	})
	v, err := p.Request(ctx, g, domain.ModelRef{}, false, "developer")
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Check(ctx, g); err == nil {
		t.Fatal("pending proposal did not gate tasks")
	}
	a, err := p.Store.GetApproval(ctx, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	var detail Proposal
	if err := json.Unmarshal(a.Detail, &detail); err != nil || detail.Analysis.Rationale == "" {
		t.Fatal("missing review detail")
	}
	if err := p.ValidateDecision(ctx, a); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Approvals.Decide(ctx, v.ID, true, "developer", "", ""); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		p.Reconcile(ctx)
		v, err = p.Get(ctx, g.Metadata.ID)
		if err != nil {
			t.Fatal(err)
		}
		if v.State == "failed" {
			t.Fatal(v.Error)
		}
		if v.State == "ready" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if v.State != "ready" {
		t.Fatalf("build timed out %+v", v)
	}
	saved, err := p.Store.GetGraph(ctx, g.Metadata.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Check(ctx, saved); err != nil {
		t.Fatal(err)
	}
	view, err := p.Manager.Get(ctx, saved)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := p.Manager.docker(ctx, nil, &out, "run", "--rm", "--network", "none", "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--entrypoint", "sh", view.Image, "-ec", `test "$(id -u)" = 10001; test "$(cat /proof)" = proposal-ok; test -s /manifest; test "$(wc -l < /proc/net/route)" -eq 1`); err != nil {
		t.Fatalf("approved sandbox %v %s", err, out.String())
	}
	t.Log("model fixture -> persisted proposal -> developer approval -> validated COPY context -> real isolated sandbox verified")
}

func TestProposalCancellationStopsAnalysisAndLeavesNoApproval(t *testing.T) {
	docker := newFakeDocker(t)
	p, g, _ := proposalFixture(t, docker.bin)
	entered := make(chan struct{})
	done := make(chan error, 1)
	p.Models = analysisModelFunc(func(ctx context.Context, _ string, _ *llm.ChatRequest) (*llm.ChatResponse, error) {
		close(entered)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	go func() {
		_, err := p.Request(context.Background(), g, domain.ModelRef{}, false, "developer")
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("analysis not started")
	}
	if err := p.Cancel(context.Background(), g.Metadata.ID, "developer"); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled analysis succeeded")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("analysis did not stop")
	}
	v, err := p.Get(context.Background(), g.Metadata.ID)
	if err != nil || v.State != "cancelled" {
		t.Fatalf("lost cancellation %+v %v", v, err)
	}
	if _, err := p.Store.GetApproval(context.Background(), v.ID); err == nil {
		t.Fatal("created approval after cancellation")
	}
	if len(docker.calls(t)) != 0 {
		t.Fatal("Docker after cancellation")
	}
}

func TestProposalStopRevokesResume(t *testing.T) {
	docker := newFakeDocker(t)
	p, g, _ := proposalFixture(t, docker.bin)
	if _, err := p.Request(context.Background(), g, domain.ModelRef{}, true, "developer"); err != nil {
		t.Fatal(err)
	}
	if err := p.DisableResume(context.Background(), g.Metadata.ID); err != nil {
		t.Fatal(err)
	}
	v, err := p.Get(context.Background(), g.Metadata.ID)
	if err != nil || v.Resume {
		t.Fatalf("stop did not revoke resume: %+v %v", v, err)
	}
	if v.State != "awaiting_approval" {
		t.Fatal("stop changed approval state")
	}
}
