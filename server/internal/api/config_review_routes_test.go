package api_test

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/app"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/installation"
	"github.com/woliveiras/weft/server/internal/reviewroute"
	"github.com/woliveiras/weft/server/internal/scm"
	"github.com/woliveiras/weft/server/internal/storage"
)

func reviewRouteYAML(t *testing.T, in reviewroute.Input) string {
	t.Helper()
	spec := installation.Map(in)
	delete(spec, "name")
	raw, err := installation.Export([]installation.Document{installation.New("PullRequestReviewRoute", in.Name, spec)})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func configReviewRouteFixture(t *testing.T) (*httptest.Server, *app.App, reviewroute.Input) {
	t.Helper()
	srv, a := setupApp(t, nil)
	token := "declarative-review-route-fixture-token"
	c, err := a.SCM.Save(t.Context(), scm.Input{Name: "review-scm", Kind: "github", Repository: "acme/app", Token: &token}, "fixture", false)
	if err != nil {
		t.Fatal(err)
	}
	g := &domain.Graph{Metadata: domain.GraphMetadata{ID: "declarative-review"}, Spec: domain.GraphSpec{
		Nodes:    []domain.Node{{ID: "reviewer", Runtime: domain.RuntimeRef{Kind: domain.RuntimeEcho}, Loop: domain.LoopSpec{Mode: domain.LoopEvent}, Tools: []domain.ToolGrant{{Name: scm.CapPullRequestRead, Repositories: []string{c.Name}}, {Name: scm.CapReviewSummary, Repositories: []string{c.Name}}}}},
		Channels: []domain.Channel{{ID: "requests", To: []string{"reviewer"}}},
	}}
	if _, _, err := a.Engine.SaveGraph(t.Context(), g, 0, "fixture"); err != nil {
		t.Fatal(err)
	}
	return srv, a, reviewroute.Input{Name: "portable-review", Connection: c.Name, ConnectionRevision: c.UpdatedAt.UTC().Format(time.RFC3339Nano), Provider: c.Kind, Instance: "https://api.github.com", Repository: c.Repository, GraphID: g.Metadata.ID, GraphVersion: 1, ChannelID: "requests", NodeID: "reviewer", Enabled: true}
}

func reviewRouteActivationCount(t *testing.T, store storage.Store) int {
	t.Helper()
	events, err := store.ListEvents(t.Context(), storage.EventFilter{Types: []domain.EventType{reviewroute.ChangeRequested}, Limit: 1000})
	if err != nil {
		t.Fatal(err)
	}
	return len(events)
}

func TestDeclarativeReviewRouteRequiresConsentAndPlanNeverActivates(t *testing.T) {
	srv, a, in := configReviewRouteFixture(t)
	raw := reviewRouteYAML(t, in)
	req := installation.Request{Files: []string{raw}}
	plan := configRequest(t, srv, "plan", req)
	if plan.OK || plan.Objects[0].Action != "error" || !strings.Contains(plan.Objects[0].Error, "recordConsent") {
		t.Fatal("enabled route did not require recorded consent", plan)
	}
	if reviewRouteActivationCount(t, a.Store) != 0 {
		t.Fatal("plan created activation boundary")
	}
	req.RecordConsent = true
	plan = configRequest(t, srv, "plan", req)
	if !plan.OK || plan.Objects[0].Action != "create" || plan.Objects[0].Consent == "" || reviewRouteActivationCount(t, a.Store) != 0 {
		t.Fatal(plan)
	}
	if _, err := a.ReviewRoutes.Get(t.Context(), in.Name); !errors.Is(err, storage.ErrNotFound) {
		t.Fatal("plan persisted a route", err)
	}
	earlier := domain.NewEvent(domain.EvPullRequestOpened, "source-task", map[string]any{
		"connection": in.Connection, "connectionRevision": in.ConnectionRevision, "provider": in.Provider,
		"instance": in.Instance, "repository": in.Repository, "number": 7, "url": "https://github.com/acme/app/pull/7",
		"headSha": strings.Repeat("a", 40), "baseSha": strings.Repeat("b", 40), "reviewRevisionAvailable": true,
	})
	if err := a.Store.AppendEvent(t.Context(), earlier); err != nil {
		t.Fatal(err)
	}
	report := configApplyRequest(t, srv, req)
	saved, err := a.ReviewRoutes.Get(t.Context(), in.Name)
	if !report.OK || report.Objects[0].Result != "applied" || err != nil || saved.Revision != 1 || saved.ActivationSeq <= earlier.Seq || reviewRouteActivationCount(t, a.Store) != 1 {
		t.Fatal("effective apply did not persist its reviewed barrier", report, saved, err)
	}
	if err := a.ReviewRoutes.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	rows, _, err := a.ReviewRoutes.ReceiptsPage(t.Context(), "", 100)
	if err != nil || len(rows) != 0 {
		t.Fatal("activation replayed an earlier PR", rows, err)
	}
	// Exported YAML has its own opaque baseline, not the manager's numeric CAS
	// revision, and contains neither activation internals nor the credential.
	exported := exportConfig(t, srv)
	docs, err := installation.Parse([]string{exported})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range docs {
		if d.Kind == "PullRequestReviewRoute" {
			found = true
			if d.Revision() == "" || d.Revision() == "1" || d.Spec["activationSeq"] != nil || d.Spec["revision"] != nil || fmt.Sprint(d.Spec["graphVersion"]) != "1" || d.Spec["connectionRevision"] != in.ConnectionRevision {
				t.Fatal("export changed explicit pins or exposed native state", d)
			}
		}
	}
	if !found || strings.Contains(exported, "declarative-review-route-fixture-token") {
		t.Fatal("missing or unsafe route export")
	}
}

func TestDeclarativeReviewRouteNoopAndExactDisableRemainOffline(t *testing.T) {
	srv, a, in := configReviewRouteFixture(t)
	report := configApplyRequest(t, srv, installation.Request{Files: []string{reviewRouteYAML(t, in)}, RecordConsent: true})
	if !report.OK {
		t.Fatal(report)
	}
	// Source authority is no longer current; validation would now fail. Retained
	// unchanged configuration and exact disabling must still be possible locally.
	if _, err := a.SCM.Save(t.Context(), scm.Input{Name: in.Connection, Kind: in.Provider, Repository: "acme/changed"}, "fixture", true); err != nil {
		t.Fatal(err)
	}
	original := a.ReviewRoutes.Validate
	a.ReviewRoutes.Validate = func(context.Context, reviewroute.Route) error { return errors.New("fixture destination unavailable") }
	t.Cleanup(func() { a.ReviewRoutes.Validate = original })
	exported := exportConfig(t, srv)
	before := reviewRouteActivationCount(t, a.Store)
	noop := configApplyRequest(t, srv, installation.Request{Files: []string{exported}})
	if !noop.OK || reviewRouteActivationCount(t, a.Store) != before {
		t.Fatal("unchanged apply activated/revalidated route", noop)
	}
	for _, item := range noop.Objects {
		if item.Result != "unchanged" {
			t.Fatal(item)
		}
	}
	in.Enabled = false
	disabled := configApplyRequest(t, srv, installation.Request{Files: []string{reviewRouteYAML(t, in)}, Force: []string{"PullRequestReviewRoute/" + in.Name}})
	saved, err := a.ReviewRoutes.Get(t.Context(), in.Name)
	if !disabled.OK || disabled.Objects[0].Result != "applied" || err != nil || saved.Enabled || saved.Revision != 2 || saved.GraphVersion != 1 || saved.ConnectionRevision != in.ConnectionRevision {
		t.Fatal("offline disable failed or changed pins", disabled, saved, err)
	}
}

func TestDeclarativeReviewRouteEnabledVersionChangeRequiresFreshConsent(t *testing.T) {
	srv, a, in := configReviewRouteFixture(t)
	first := configApplyRequest(t, srv, installation.Request{Files: []string{reviewRouteYAML(t, in)}, RecordConsent: true})
	if !first.OK {
		t.Fatal(first)
	}
	old, err := a.ReviewRoutes.Get(t.Context(), in.Name)
	if err != nil {
		t.Fatal(err)
	}
	g, err := a.Store.GetGraph(t.Context(), in.GraphID, in.GraphVersion)
	if err != nil {
		t.Fatal(err)
	}
	g.Metadata.Description = "Separately saved review policy"
	if _, _, err := a.Engine.SaveGraph(t.Context(), g, in.GraphVersion, "fixture"); err != nil {
		t.Fatal(err)
	}
	in.GraphVersion = 2
	req := installation.Request{Files: []string{reviewRouteYAML(t, in)}, Force: []string{"PullRequestReviewRoute/" + in.Name}}
	plan := configRequest(t, srv, "plan", req)
	if plan.OK || !strings.Contains(plan.Objects[0].Error, "recordConsent") || reviewRouteActivationCount(t, a.Store) != 1 {
		t.Fatal("version change reused prior consent", plan)
	}
	req.RecordConsent = true
	report := configApplyRequest(t, srv, req)
	changed, err := a.ReviewRoutes.Get(t.Context(), in.Name)
	if !report.OK || err != nil || changed.GraphVersion != 2 || changed.Revision != 2 || changed.ActivationSeq <= old.ActivationSeq || reviewRouteActivationCount(t, a.Store) != 2 {
		t.Fatal("explicit saved version was not freshly activated", report, changed, err)
	}
}

func TestDeclarativeReviewRouteRejectsImplicitPinsAndMixedAdoption(t *testing.T) {
	srv, _, in := configReviewRouteFixture(t)
	for _, edit := range []func(*reviewroute.Input){
		func(v *reviewroute.Input) { v.GraphVersion = 0 },
		func(v *reviewroute.Input) { v.GraphVersion = 2 },
		func(v *reviewroute.Input) { v.ConnectionRevision = "" },
		func(v *reviewroute.Input) { v.ConnectionRevision = "2026-01-01T00:00:00Z" },
		func(v *reviewroute.Input) { v.GraphID = "unsaved" },
		func(v *reviewroute.Input) { v.Connection = "unsaved" },
	} {
		bad := in
		edit(&bad)
		if plan := configRequest(t, srv, "plan", installation.Request{Files: []string{reviewRouteYAML(t, bad)}, RecordConsent: true}); plan.OK {
			t.Fatal("implicit/invalid pin accepted", plan)
		}
	}
	for _, dependency := range []struct{ key, document string }{
		{"Graph/" + in.GraphID, yamlObject("Graph", in.GraphID, "  defaults: {runtime: {kind: echo}}\n  nodes: [{id: replacement}]")},
		{"SCMConnection/" + in.Connection, yamlObject("SCMConnection", in.Connection, "  kind: github\n  repository: acme/replacement\n  secretRef: {preserve: true}")},
	} {
		plan := configRequest(t, srv, "plan", installation.Request{Files: []string{reviewRouteYAML(t, in), dependency.document}, RecordConsent: true, Force: []string{dependency.key}})
		if plan.OK {
			t.Fatal("mixed source/destination adoption accepted", plan)
		}
	}
}

type reviewRouteWriteFailure struct{ storage.Store }

func (s reviewRouteWriteFailure) PutSettingIf(ctx context.Context, key string, value []byte, expected int64, actor string, guards []storage.SettingRevision) (int64, error) {
	if strings.HasPrefix(key, reviewroute.RoutePrefix) {
		return 0, errors.New("fixture route write unavailable")
	}
	return s.Store.PutSettingIf(ctx, key, value, expected, actor, guards)
}

func TestDeclarativeReviewRouteWriteFailureAndStaleBaselineDoNotActivate(t *testing.T) {
	srv, a, in := configReviewRouteFixture(t)
	req := installation.Request{Files: []string{reviewRouteYAML(t, in)}, RecordConsent: true}
	original := a.ReviewRoutes.Store
	a.ReviewRoutes.Store = reviewRouteWriteFailure{Store: original}
	report := configApplyRequest(t, srv, req)
	a.ReviewRoutes.Store = original
	if report.OK || report.Objects[0].Result != "failed" {
		t.Fatal("route write failure was hidden", report)
	}
	if _, err := a.ReviewRoutes.Get(t.Context(), in.Name); !errors.Is(err, storage.ErrNotFound) {
		t.Fatal("failed write activated route", err)
	}
	report = configApplyRequest(t, srv, req)
	if !report.OK {
		t.Fatal(report)
	}
	in.Enabled = false
	req = installation.Request{Files: []string{reviewRouteYAML(t, in)}, Force: []string{"PullRequestReviewRoute/" + in.Name}}
	plan := configRequest(t, srv, "plan", req)
	if !plan.OK {
		t.Fatal(plan)
	}
	saved, err := a.ReviewRoutes.Get(t.Context(), in.Name)
	if err != nil {
		t.Fatal(err)
	}
	saved.Enabled = false
	if _, err := a.ReviewRoutes.Put(t.Context(), saved.Input, saved.Revision, "another-operator"); err != nil {
		t.Fatal(err)
	}
	before := reviewRouteActivationCount(t, a.Store)
	req.Confirmed = true
	req.Expected = map[string]string{plan.Objects[0].Key: plan.Objects[0].Revision}
	report = configRequest(t, srv, "apply", req)
	if report.OK || report.Objects[0].Result != "not-applied" || reviewRouteActivationCount(t, a.Store) != before {
		t.Fatal("force bypassed stale reviewed route baseline", report)
	}
}
