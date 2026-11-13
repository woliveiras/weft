package api_test

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/cost"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/eventtest"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/taskconfig"
	"github.com/woliveiras/weft/server/internal/taskrun"
)

func TestPriceCatalogueThroughAPI(t *testing.T) {
	srv, a := setupApp(t, nil)
	var cat struct {
		Version  int    `json:"version"`
		Currency string `json:"currency"`
		Unpriced string `json:"unpriced"`
		Prices   []struct {
			Type                string `json:"type"`
			Model               string `json:"model"`
			OutputMicrosPerMTok int64  `json:"outputMicrosPerMTok"`
		} `json:"prices"`
	}
	if c := call(t, srv, "GET", "/api/v1/cost/prices", viewerTok, "", "", &cat); c != 200 || cat.Version != 0 || cat.Currency != "USD" {
		t.Fatalf("default: %d %+v", c, cat)
	}
	body := `{"currency":"USD","unpriced":"allow","prices":[{"type":"openai","model":"*","inputMicrosPerMTok":2500000,"outputMicrosPerMTok":10000000}]}`
	for _, tok := range []string{viewerTok, operatorTok} {
		if c := call(t, srv, "PUT", "/api/v1/cost/prices", tok, "application/json", body, nil); c != 403 {
			t.Fatalf("non-admin put: %d", c)
		}
	}
	var bad apiError
	if c := call(t, srv, "PUT", "/api/v1/cost/prices", adminTok, "application/json", `{"currency":"US","prices":[]}`, &bad); c != 422 || len(bad.Details.Errors) != 1 || bad.Details.Errors[0].Path != "currency" {
		t.Fatalf("invalid: %d %+v", c, bad)
	}
	if c := call(t, srv, "PUT", "/api/v1/cost/prices", adminTok, "application/json", `{"currency":"USD","extra":1}`, nil); c != 400 {
		t.Fatalf("unknown field: %d", c)
	}
	code, hdr, out := rawCall(t, "PUT", srv.URL+"/api/v1/cost/prices", adminTok, body, nil)
	if code != 200 || !strings.Contains(out, `"version":1`) || hdr.Get("ETag") == "" {
		t.Fatalf("put: %d %s", code, out)
	}
	if c, _, _ := rawCall(t, "PUT", srv.URL+"/api/v1/cost/prices", adminTok, body, map[string]string{"If-Match": `"999"`}); c != 409 {
		t.Fatalf("stale: %d", c)
	}
	if c, _, _ := rawCall(t, "PUT", srv.URL+"/api/v1/cost/prices", adminTok, body, map[string]string{"If-Match": hdr.Get("ETag")}); c != 200 {
		t.Fatalf("matching revision: %d", c)
	}
	if c := call(t, srv, "GET", "/api/v1/cost/prices", viewerTok, "", "", &cat); c != 200 || cat.Version != 2 || len(cat.Prices) != 1 || cat.Prices[0].OutputMicrosPerMTok != 10_000_000 {
		t.Fatalf("after put: %+v", cat)
	}
	evs, _ := a.Store.ListEvents(t.Context(), storage.EventFilter{Types: []domain.EventType{domain.EvPricesChanged}})
	if len(evs) != 2 || evs[0].Actor != "admin" {
		t.Fatalf("audit = %+v", evs)
	}
	eventtest.Capture(t, a.Store, domain.EvPricesChanged)
}

// A task run that reaches its monetary budget is paused before the model is
// called; only a person can raise the budget, which resumes the run.
func TestTaskRunBudgetPausesAndAHumanRaiseResumes(t *testing.T) {
	testBudgetRaise(t, "running")
}

func TestBudgetRaiseReportsGraphStillPausedAfterLeaseWait(t *testing.T) {
	testBudgetRaise(t, "blocked")
}

func TestBudgetRaiseDropsStoppedOrDeletedGraphs(t *testing.T) {
	for _, state := range []string{"stopped", "deleted"} {
		t.Run(state, func(t *testing.T) { testBudgetRaise(t, state) })
	}
}

func testBudgetRaise(t *testing.T, state string) {
	t.Helper()
	srv, a := setupApp(t, nil)
	nativeGraph := `{"metadata":{"id":"priced-fixture","name":"Priced fixture"},"spec":{"nodes":[{"id":"reader","prompt":"Summarise the request in one line.","runtime":{"kind":"native"},"model":{"provider":"mock"},"loop":{"mode":"once"},"tools":[]}]}}`
	if c, _, b := rawCall(t, "POST", srv.URL+"/api/v1/graphs", adminTok, nativeGraph, nil); c != 201 {
		t.Fatal(c, b)
	}
	prices := `{"currency":"USD","prices":[{"type":"mock","model":"*","inputMicrosPerMTok":1000000,"outputMicrosPerMTok":1000000}]}`
	if c := call(t, srv, "PUT", "/api/v1/cost/prices", adminTok, "application/json", prices, nil); c != 200 {
		t.Fatalf("prices: %d", c)
	}
	if c := call(t, srv, "PUT", "/api/v1/cost/policy", operatorTok, "application/json", `{"taskRunBudgetMicros":1}`, nil); c != 403 {
		t.Fatalf("operator policy: %d", c)
	}
	if c := call(t, srv, "PUT", "/api/v1/cost/policy", adminTok, "application/json", `{"taskRunBudgetMicros":-1}`, nil); c != 422 {
		t.Fatalf("invalid policy: %d", c)
	}
	if c := call(t, srv, "PUT", "/api/v1/cost/policy", adminTok, "application/json", `{"taskRunBudgetMicros":1}`, nil); c != 200 {
		t.Fatalf("policy: %d", c)
	}
	body := `{"title":"Read code","request":"Explain architecture","outcome":"understand","graphId":"priced-fixture","graphVersion":1,"nodeId":"reader","resultNodeId":"reader"}`
	var d taskconfig.Draft
	if c := call(t, srv, "POST", "/api/v1/tasks", operatorTok, "application/json", body, &d); c != 201 {
		t.Fatal(c)
	}
	if c := taskStart(t, srv.URL, d.ID, adminTok, d.Revision); c != 202 {
		t.Fatal(c)
	}
	gid := taskrun.ExecutionID(d.ID)
	type view struct {
		LimitMicros, ChargedMicros, ReservedMicros int64
		Reached, Paused                            bool
		RaisedBy                                   string
		NotResumed                                 []string
	}
	var v view
	path := "/api/v1/cost/budgets/run/" + gid
	waitFor(t, 10*time.Second, "budget reached and run paused", func() bool {
		_, rec, err := a.Engine.GetGraph(t.Context(), gid, 0)
		return err == nil && rec.DesiredState == domain.GraphPaused &&
			call(t, srv, "GET", path, viewerTok, "", "", &v) == 200 && v.Reached && v.Paused
	})
	waitFor(t, 5*time.Second, "refused turn ends without ending the paused task", func() bool {
		var progress taskrun.View
		return call(t, srv, "GET", "/api/v1/tasks/"+d.ID+"/progress", viewerTok, "", "", &progress) == 200 &&
			len(progress.Executions) > 0 && progress.Executions[0].Status.Terminal() && progress.State == "running"
	})
	if state == "running" {
		if err := a.Store.KVPut(t.Context(), "_control/"+gid, "node/reader", []byte(`{"paused":"invalid"}`)); err != nil {
			t.Fatal(err)
		}
		var progress taskrun.View
		if c := call(t, srv, "GET", "/api/v1/tasks/"+d.ID+"/progress", viewerTok, "", "", &progress); c != 200 || progress.State != "running" {
			t.Fatalf("invalid node control broke paused task progress: %d %+v", c, progress)
		}
		if err := a.Store.KVDelete(t.Context(), "_control/"+gid, "node/reader"); err != nil {
			t.Fatal(err)
		}
	}

	if v.LimitMicros != 1 || v.ChargedMicros != 0 || v.ReservedMicros != 0 {
		t.Fatalf("refused call must cost nothing: %+v", v)
	}
	if _, rec, err := a.Engine.GetGraph(t.Context(), gid, 0); err != nil || rec.DesiredState != domain.GraphPaused {
		t.Fatalf("run must be paused: %+v %v", rec, err)
	}
	if evs, _ := a.Store.ListEvents(t.Context(), storage.EventFilter{GraphID: gid, Types: []domain.EventType{domain.EvBudgetReached}}); len(evs) != 1 {
		t.Fatalf("budget.reached = %d", len(evs))
	}
	// The model was never called for this run.
	if evs, _ := a.Store.ListEvents(t.Context(), storage.EventFilter{GraphID: gid, Types: []domain.EventType{domain.EvCostRecorded}}); len(evs) != 0 {
		t.Fatalf("a refused call reached the provider: %+v", evs)
	}

	if c := call(t, srv, "POST", "/api/v1/cost/budgets/run/"+gid+"/raise", operatorTok, "application/json", `{"limitMicros":10000000}`, nil); c != 403 {
		t.Fatalf("operator raise: %d", c)
	}
	if c := call(t, srv, "POST", "/api/v1/cost/budgets/run/"+gid+"/raise", adminTok, "application/json", `{"limitMicros":1}`, nil); c != 422 {
		t.Fatalf("raise must increase the limit: %d", c)
	}
	if c := call(t, srv, "POST", "/api/v1/cost/budgets/run/nope/raise", adminTok, "application/json", `{"limitMicros":5}`, nil); c != 404 {
		t.Fatalf("missing budget: %d", c)
	}
	key := "graph-transition/" + gid
	want := domain.GraphRunning
	var resumed, pending []string
	switch state {
	case "blocked":
		want = domain.GraphPaused
		pending = []string{gid}
		if ok, err := a.Store.AcquireLease(t.Context(), key, "test-transition", time.Minute); err != nil || !ok {
			t.Fatalf("hold graph transition: %t %v", ok, err)
		}
	case "stopped", "deleted":
		want = domain.GraphStopped
		if c := call(t, srv, "POST", "/api/v1/tasks/"+d.ID+"/cancel", adminTok, "application/json", `{"confirm":true}`, nil); c != 202 {
			t.Fatalf("cancel: %d", c)
		}
		if state == "deleted" {
			waitFor(t, 5*time.Second, "graph stopped before delete", func() bool {
				_, rec, err := a.Engine.GetGraph(t.Context(), gid, 0)
				return err == nil && rec.State == domain.GraphStopped
			})
			if err := a.Engine.WithGraphTransition(t.Context(), gid, func(ctx context.Context) error {
				return a.Engine.DeleteGraph(ctx, gid, "admin")
			}); err != nil {
				t.Fatalf("delete: %v", err)
			}
		}
	default:
		resumed = []string{gid}
	}
	if c := call(t, srv, "POST", path+"/raise", adminTok, "application/json", `{"limitMicros":10000000}`, &v); c != 200 || v.Reached ||
		v.Paused != (state == "blocked") || v.LimitMicros != 10_000_000 || v.RaisedBy != "admin" || !slices.Equal(v.NotResumed, pending) {
		t.Fatalf("raise: %d %+v", c, v)
	}
	if _, rec, err := a.Engine.GetGraph(t.Context(), gid, 0); state != "deleted" && (err != nil || rec.DesiredState != want) {
		t.Fatalf("state after raise: %+v %v", rec, err)
	}
	var audit struct {
		Resumed, NotResumed []string
		Skipped             map[string]string
	}
	events, err := a.Store.ListEvents(t.Context(), storage.EventFilter{GraphID: gid, Types: []domain.EventType{domain.EvBudgetRaised}})
	if err != nil || len(events) != 1 || events[0].Actor != "admin" ||
		json.Unmarshal(events[0].Data, &audit) != nil || !slices.Equal(audit.Resumed, resumed) || !slices.Equal(audit.NotResumed, pending) {
		t.Fatalf("budget.raised audit: %+v (%+v) %v", audit, events, err)
	}
	if (state == "stopped" || state == "deleted") && audit.Skipped[gid] != state {
		t.Fatalf("missing skip reason %q: %+v", state, audit)
	}
	if state == "blocked" {
		var ledgers []view
		if c := call(t, srv, "GET", "/api/v1/cost/ledgers", viewerTok, "", "", &ledgers); c != 200 || len(ledgers) != 1 ||
			!ledgers[0].Paused || !slices.Equal(ledgers[0].NotResumed, pending) {
			t.Fatalf("persisted pending resume: %d %+v", c, ledgers)
		}
		if err := a.Store.ReleaseLease(t.Context(), key, "test-transition"); err != nil {
			t.Fatal(err)
		}
		waitFor(t, 12*time.Second, "pending resume applied and audited", func() bool {
			_, rec, err := a.Engine.GetGraph(t.Context(), gid, 0)
			if err != nil || rec.DesiredState != domain.GraphRunning {
				return false
			}
			var fresh view
			if c := call(t, srv, "GET", path, viewerTok, "", "", &fresh); c != 200 || fresh.Paused || len(fresh.NotResumed) != 0 {
				return false
			}
			evs, err := a.Store.ListEvents(t.Context(), storage.EventFilter{GraphID: gid, Types: []domain.EventType{domain.EvBudgetGraphTransition}})
			return err == nil && slices.ContainsFunc(evs, func(ev *domain.Event) bool {
				return ev.Actor == "system:budget" && strings.Contains(string(ev.Data), `"outcome":"resumed"`)
			})
		})
	}
	eventtest.Capture(t, a.Store, domain.EvBudgetChanged, domain.EvBudgetReached, domain.EvBudgetRaised, domain.EvBudgetGraphTransition)
}

// Monthly budgets: reaching the installation budget pauses the run, refuses
// new task starts, and a raise of that month's ledger resumes the run.
func TestMonthlyBudgetPausesRunsAndRefusesNewTasks(t *testing.T) {
	srv, a := setupApp(t, nil)
	nativeGraph := `{"metadata":{"id":"priced-fixture","name":"Priced fixture"},"spec":{"nodes":[{"id":"reader","prompt":"Summarise the request in one line.","runtime":{"kind":"native"},"model":{"provider":"mock"},"loop":{"mode":"once"},"tools":[]}]}}`
	if c := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/json", nativeGraph, nil); c != 201 {
		t.Fatal(c)
	}
	if c := call(t, srv, "PUT", "/api/v1/cost/prices", adminTok, "application/json", `{"currency":"USD","prices":[{"type":"mock","model":"*","inputMicrosPerMTok":1000000,"outputMicrosPerMTok":1000000}]}`, nil); c != 200 {
		t.Fatal(c)
	}
	if c := call(t, srv, "PUT", "/api/v1/cost/budgets", operatorTok, "application/json", `{"budgets":[]}`, nil); c != 403 {
		t.Fatalf("operator budgets: %d", c)
	}
	var bad apiError
	if c := call(t, srv, "PUT", "/api/v1/cost/budgets", adminTok, "application/json", `{"budgets":[{"scope":"team","id":"x","limitMicros":1}]}`, &bad); c != 422 || bad.Details.Errors[0].Path != "budgets[0].scope" {
		t.Fatalf("invalid budgets: %d %+v", c, bad)
	}
	if c := call(t, srv, "PUT", "/api/v1/cost/budgets", adminTok, "application/json", `{"budgets":[{"scope":"installation","limitMicros":1}]}`, nil); c != 200 {
		t.Fatalf("budgets: %d", c)
	}
	var defs struct {
		Budgets []struct{ Scope, ID string } `json:"budgets"`
		Period  string                       `json:"period"`
	}
	if c := call(t, srv, "GET", "/api/v1/cost/budgets", viewerTok, "", "", &defs); c != 200 || len(defs.Budgets) != 1 || defs.Budgets[0].ID != "*" || defs.Period == "" {
		t.Fatalf("definitions: %d %+v", c, defs)
	}
	body := `{"title":"Read code","request":"Explain architecture","outcome":"understand","graphId":"priced-fixture","graphVersion":1,"nodeId":"reader","resultNodeId":"reader"}`
	start := func() (taskconfig.Draft, int) {
		var d taskconfig.Draft
		if c := call(t, srv, "POST", "/api/v1/tasks", operatorTok, "application/json", body, &d); c != 201 {
			t.Fatal(c)
		}
		return d, taskStart(t, srv.URL, d.ID, adminTok, d.Revision)
	}
	d, c := start()
	if c != 202 {
		t.Fatal(c)
	}
	gid := taskrun.ExecutionID(d.ID)
	ledger := "/api/v1/cost/budgets/installation/" + "*@" + defs.Period
	var v struct {
		Reached, Paused bool
		LimitMicros     int64
	}
	waitFor(t, 10*time.Second, "installation budget reached", func() bool {
		_, rec, err := a.Engine.GetGraph(t.Context(), gid, 0)
		return err == nil && rec.DesiredState == domain.GraphPaused &&
			call(t, srv, "GET", ledger, viewerTok, "", "", &v) == 200 && v.Reached && v.Paused
	})
	if _, rec, err := a.Engine.GetGraph(t.Context(), gid, 0); err != nil || rec.DesiredState != domain.GraphPaused {
		t.Fatalf("run must be paused: %+v %v", rec, err)
	}
	if _, c := start(); c != 409 {
		t.Fatalf("a reached monthly budget must refuse new task starts: %d", c)
	}
	if c := call(t, srv, "POST", ledger+"/raise", adminTok, "application/json", `{"limitMicros":100000000}`, &v); c != 200 || v.Reached || v.Paused {
		t.Fatalf("raise: %d %+v", c, v)
	}
	if _, rec, err := a.Engine.GetGraph(t.Context(), gid, 0); err != nil || rec.DesiredState != domain.GraphRunning {
		t.Fatalf("raise must resume the run: %+v %v", rec, err)
	}
	if _, c := start(); c != 202 {
		t.Fatalf("new tasks start after the raise: %d", c)
	}
}

func TestCostReportingEndpoints(t *testing.T) {
	srv, _ := setupApp(t, nil)
	var rep struct {
		Period string              `json:"period"`
		Total  struct{ Calls int } `json:"total"`
	}
	if c := call(t, srv, "GET", "/api/v1/cost/spend", viewerTok, "", "", &rep); c != 200 || len(rep.Period) != 7 {
		t.Fatalf("spend: %d %+v", c, rep)
	}
	if c := call(t, srv, "GET", "/api/v1/cost/spend?period=bad", viewerTok, "", "", nil); c != 422 {
		t.Fatalf("bad period: %d", c)
	}
	var ledgers []any
	if c := call(t, srv, "GET", "/api/v1/cost/ledgers", viewerTok, "", "", &ledgers); c != 200 || len(ledgers) != 0 {
		t.Fatalf("ledgers: %d %+v", c, ledgers)
	}
	if c := call(t, srv, "PUT", "/api/v1/cost/budgets", adminTok, "application/json", `{"budgets":[{"scope":"user","id":"operator","limitMicros":700}]}`, nil); c != 200 {
		t.Fatal(c)
	}
	var preview struct {
		Budgets []struct {
			Scope           string
			RemainingMicros int64
		}
	}
	// The preview is for the caller: the operator's own budget applies.
	if c := call(t, srv, "GET", "/api/v1/cost/budgets/preview?workflow=factory", operatorTok, "", "", &preview); c != 200 || len(preview.Budgets) != 1 || preview.Budgets[0].RemainingMicros != 700 {
		t.Fatalf("preview: %d %+v", c, preview)
	}
	if c := call(t, srv, "GET", "/api/v1/cost/budgets/preview", viewerTok, "", "", &preview); c != 200 || len(preview.Budgets) != 0 {
		t.Fatalf("viewer preview: %d %+v", c, preview)
	}
}

func TestInvalidBudgetLedgerDoesNotHideValidLedgersOrBlockUnrelatedResume(t *testing.T) {
	srv, a := setupApp(t, nil)
	ctx := t.Context()
	const invalidID = "task-run-invalid"
	const validID = "task-run-valid"
	if _, err := a.Store.PutSetting(ctx, cost.BudgetPrefix+"run/"+invalidID, []byte("{"), 0, "fixture"); err != nil {
		t.Fatal(err)
	}
	g := &domain.Graph{Metadata: domain.GraphMetadata{ID: validID}}
	if _, err := a.Store.SaveGraph(ctx, g, 0); err != nil {
		t.Fatal(err)
	}
	if err := a.Store.PutGraphRecord(ctx, &domain.GraphRecord{ID: validID, Version: 1, RunID: "current",
		DesiredState: domain.GraphPaused, State: domain.GraphPaused}); err != nil {
		t.Fatal(err)
	}
	const valid = `{"scope":"run","id":"task-run-valid","currency":"USD","limit":10,"reached":true,"entries":{},"pausedGraphs":["task-run-valid"],"graphActions":{"task-run-valid":{"action":"hold","runId":"current"}}}`
	if _, err := a.Store.PutSetting(ctx, cost.BudgetPrefix+"run/"+validID, []byte(valid), 0, "fixture"); err != nil {
		t.Fatal(err)
	}
	var all []cost.View
	if c := call(t, srv, "GET", "/api/v1/cost/ledgers", viewerTok, "", "", &all); c != 200 || len(all) != 2 ||
		all[0].ID != invalidID || !all[0].Invalid || all[0].Revision != 1 || all[1].ID != validID || all[1].Invalid {
		t.Fatalf("individual ledger status: %d %+v", c, all)
	}
	path := "/api/v1/cost/budgets/run/"
	var invalid, raised cost.View
	if c := call(t, srv, "GET", path+invalidID, viewerTok, "", "", &invalid); c != 200 || !invalid.Invalid || invalid.Revision != 1 {
		t.Fatalf("invalid ledger: %d %+v", c, invalid)
	}
	if c := call(t, srv, "POST", path+invalidID+"/raise", adminTok, "application/json", `{"limitMicros":20}`, nil); c != 409 {
		t.Fatalf("invalid ledger accepted a raise: %d", c)
	}
	if c := call(t, srv, "POST", path+validID+"/raise", adminTok, "application/json", `{"limitMicros":20}`, &raised); c != 200 ||
		raised.Paused || len(raised.NotResumed) != 0 {
		t.Fatalf("unrelated raise: %d %+v", c, raised)
	}
	rec, err := a.Store.GetGraphRecord(ctx, validID)
	if err != nil || rec.DesiredState != domain.GraphRunning {
		t.Fatalf("unrelated graph could not resume: %+v %v", rec, err)
	}
	evs, err := a.Store.ListEvents(ctx, storage.EventFilter{Types: []domain.EventType{domain.EvBudgetLedgerInvalid}})
	if err != nil || len(evs) != 1 || evs[0].GraphID != invalidID {
		t.Fatalf("expected one invalid revision audit: %+v %v", evs, err)
	}
}

func TestClosedBudgetPeriodIsReadOnlyAndDoesNotShowPausedRuns(t *testing.T) {
	srv, a := setupApp(t, nil)
	const ledger = `{"scope":"installation","id":"*@2000-01","limit":10,"reached":true,"entries":{},"pausedGraphs":["old-run"]}`
	if _, err := a.Store.PutSetting(t.Context(), cost.BudgetPrefix+"installation/*@2000-01", []byte(ledger), 0, "fixture"); err != nil {
		t.Fatal(err)
	}
	var all []cost.View
	if c := call(t, srv, "GET", "/api/v1/cost/ledgers", viewerTok, "", "", &all); c != 200 || len(all) != 0 {
		t.Fatalf("historical ledger in current list: %d %+v", c, all)
	}
	if c := call(t, srv, "GET", "/api/v1/cost/ledgers?period=2000-01", viewerTok, "", "", &all); c != 200 ||
		len(all) != 1 || all[0].Paused || len(all[0].NotResumed) != 0 {
		t.Fatalf("historical ledger: %d %+v", c, all)
	}
	if c := call(t, srv, "POST", "/api/v1/cost/budgets/installation/*@2000-01/raise", adminTok, "application/json", `{"limitMicros":20}`, nil); c != 422 {
		t.Fatalf("closed period accepted a raise: %d", c)
	}
}

func TestBudgetHoldClearsAfterGraphStop(t *testing.T) {
	srv, a := setupApp(t, nil)
	const id = "task-run-stop-fixture"
	ctx := t.Context()
	g := &domain.Graph{Metadata: domain.GraphMetadata{ID: id}}
	if _, err := a.Store.SaveGraph(ctx, g, 0); err != nil {
		t.Fatal(err)
	}
	if err := a.Store.PutGraphRecord(ctx, &domain.GraphRecord{ID: id, Version: 1, RunID: "run-a",
		DesiredState: domain.GraphStopped, State: domain.GraphStopped}); err != nil {
		t.Fatal(err)
	}
	const raw = `{"scope":"run","id":"task-run-stop-fixture","limit":10,"entries":{},"reached":true,"pausedGraphs":["task-run-stop-fixture"],"graphActions":{"task-run-stop-fixture":{"action":"hold","runId":"run-a"}}}`
	if _, err := a.Store.PutSetting(ctx, cost.BudgetPrefix+"run/"+id, []byte(raw), 0, "fixture"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 10*time.Second, "automatic hold cleanup", func() bool {
		var view cost.View
		return call(t, srv, "GET", "/api/v1/cost/budgets/run/"+id, viewerTok, "", "", &view) == 200 &&
			!view.Paused && view.Reached
	})
}

func TestManualBudgetResumeThroughAPI(t *testing.T) {
	srv, app := setupApp(t, nil)
	ctx := t.Context()
	const graph = "budget-manual-fixture"
	const path = "/api/v1/graphs/" + graph
	if c, _, body := rawCall(t, "POST", srv.URL+"/api/v1/graphs", adminTok,
		`{"metadata":{"id":"budget-manual-fixture"},"spec":{"nodes":[{"id":"reader","runtime":{"kind":"echo"}}]}}`, nil); c != 201 {
		t.Fatal(c, body)
	}
	rec, err := app.Store.GetGraphRecord(ctx, graph)
	if err != nil {
		t.Fatal(err)
	}
	rec.DesiredState, rec.State, rec.RunID = domain.GraphPaused, domain.GraphPaused, "run-fixture"
	if err := app.Store.PutGraphRecord(ctx, rec); err != nil {
		t.Fatal(err)
	}
	if c := call(t, srv, "POST", path+"/nodes/reader/pause", operatorTok, "", "", nil); c != 200 {
		t.Fatalf("pause node: %d", c)
	}
	raw, err := json.Marshal(cost.Ledger{Scope: cost.TaskRun, ID: graph, Currency: "USD", Limit: 10, Compacted: 30,
		Reached: true, Entries: map[string]cost.Entry{}, PausedGraphs: []string{graph},
		GraphActions: map[string]cost.GraphAction{graph: {Action: cost.HoldGraph, RunID: rec.RunID}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.Store.PutSetting(ctx, cost.BudgetPrefix+"run/"+graph, raw, 0, "fixture"); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"resume", "start", "nodes/reader/resume"} {
		code, _, body := rawCall(t, "POST", srv.URL+path+"/"+action, operatorTok, "", nil)
		if code != 409 || !strings.Contains(body, `"code":"budget_resume_blocked"`) ||
			!strings.Contains(body, graph) || !strings.Contains(body, "raise") {
			t.Fatalf("%s: expected actionable budget conflict, got %d %s", action, code, body)
		}
	}
	var status struct {
		DesiredState string
		Nodes        []struct{ Paused bool }
	}
	if c := call(t, srv, "GET", path+"/status", viewerTok, "", "", &status); c != 200 ||
		status.DesiredState != "paused" || len(status.Nodes) != 1 || !status.Nodes[0].Paused {
		t.Fatalf("blocked resume changed graph or node state: %d %+v", c, status)
	}
	audits, err := app.Store.ListEvents(ctx, storage.EventFilter{GraphID: graph, Types: []domain.EventType{domain.EvBudgetGraphTransition}})
	if err != nil || len(audits) != 3 {
		t.Fatalf("expected one audit per refusal: %v %v", audits, err)
	}
	for _, audit := range audits {
		if audit.Actor != "operator" || !strings.Contains(string(audit.Data), `"outcome":"budget_reached"`) {
			t.Fatalf("wrong refusal audit: %+v %s", audit, audit.Data)
		}
	}
	if ok, err := app.Store.AcquireLease(ctx, "graph-transition/"+graph, "fixture", time.Minute); !ok || err != nil {
		t.Fatalf("hold graph transition: %t %v", ok, err)
	}
	budget := "/api/v1/cost/budgets/run/" + graph
	var view cost.View
	if c := call(t, srv, "POST", budget+"/raise", adminTok, "application/json", `{"limitMicros":20}`, &view); c != 200 ||
		!view.Reached || !view.Paused || len(view.NotResumed) != 0 || view.Charged != 30 {
		t.Fatalf("insufficient raise: %d %+v", c, view)
	}
	if err := app.Store.ReleaseLease(ctx, "graph-transition/"+graph, "fixture"); err != nil {
		t.Fatal(err)
	}
	if c := call(t, srv, "POST", budget+"/raise", adminTok, "application/json", `{"limitMicros":40}`, &view); c != 200 || view.Paused {
		t.Fatalf("sufficient raise: %d %+v", c, view)
	}
	if saved, err := app.Store.GetGraphRecord(ctx, graph); err != nil || saved.BudgetResumeID == "" {
		t.Fatalf("missing durable resume receipt: %+v %v", saved, err)
	}
	eventtest.Capture(t, app.Store, domain.EvBudgetGraphTransition, domain.EvBudgetRaised)
	for _, path := range []string{"/api/v1/graphs", path} {
		if c, _, body := rawCall(t, "GET", srv.URL+path, viewerTok, "", nil); c != 200 ||
			!strings.Contains(body, graph) || strings.Contains(body, "budgetResumeId") {
			t.Fatalf("graph API exposed an internal receipt at %s: %d %s", path, c, body)
		}
	}
}

func TestMonthlyBudgetEditsAndGraphLifecycle(t *testing.T) {
	for _, scenario := range []string{"remove then resume", "increase then automatic resume", "increase with a second blocker", "stop then start"} {
		t.Run(scenario, func(t *testing.T) {
			srv, app := setupApp(t, nil)
			ctx := t.Context()
			const graph = "monthly-budget-fixture"
			const path = "/api/v1/graphs/" + graph
			const definitions = "/api/v1/cost/budgets"
			period := cost.Period(time.Now())
			scope, id := cost.User, cost.MonthlyID("alice", period)
			if c := call(t, srv, "PUT", definitions, adminTok, "application/json",
				`{"budgets":[{"scope":"user","id":"alice","limitMicros":10}]}`, nil); c != 200 {
				t.Fatalf("set definition: %d", c)
			}
			g := &domain.Graph{Metadata: domain.GraphMetadata{ID: graph, Labels: map[string]string{domain.LabelTaskUser: "alice"}},
				Spec: domain.GraphSpec{Nodes: []domain.Node{{ID: "reader", Runtime: domain.RuntimeRef{Kind: "echo"}}}}}
			if _, _, err := app.Engine.SaveGraph(ctx, g, 0, "admin"); err != nil {
				t.Fatal(err)
			}
			rec, err := app.Store.GetGraphRecord(ctx, graph)
			if err != nil {
				t.Fatal(err)
			}
			rec.DesiredState, rec.State, rec.RunID = domain.GraphPaused, domain.GraphPaused, "run-fixture"
			if err := app.Store.PutGraphRecord(ctx, rec); err != nil {
				t.Fatal(err)
			}
			seedLifecycleLedger(t, app.Store, graph, rec.RunID, scope, id)
			runKey := cost.BudgetPrefix + cost.TaskRun + "/" + graph
			if scenario == "increase with a second blocker" {
				seedLifecycleLedger(t, app.Store, graph, rec.RunID, cost.TaskRun, graph)
			} else {
				raw := []byte(`{"scope":"run","id":"` + graph + `","limit":10,"entries":{}}`)
				if _, err := app.Store.PutSetting(ctx, runKey, raw, 0, "fixture"); err != nil {
					t.Fatal(err)
				}
			}
			switch scenario {
			case "remove then resume":
				if c := call(t, srv, "PUT", definitions, adminTok, "application/json", `{"budgets":[]}`, nil); c != 200 {
					t.Fatalf("remove: %d", c)
				}
				if c, _, body := rawCall(t, "POST", srv.URL+path+"/resume", operatorTok, "", nil); c != 200 {
					t.Fatalf("resume after removing the definition: %d %s", c, body)
				}
			case "stop then start":
				if c, _, body := rawCall(t, "POST", srv.URL+path+"/stop", operatorTok, "", nil); c != 200 {
					t.Fatalf("stop: %d %s", c, body)
				}
				for _, scope := range []string{cost.User, cost.TaskRun} {
					if scope == cost.TaskRun {
						row, err := app.Store.GetSetting(ctx, runKey)
						if err != nil {
							t.Fatal(err)
						}
						raw := []byte(`{"scope":"run","id":"` + graph + `","limit":10,"compacted":10,"entries":{}}`)
						if _, err := app.Store.PutSetting(ctx, runKey, raw, row.Revision, "fixture"); err != nil {
							t.Fatal(err)
						}
					}
					c, _, body := rawCall(t, "POST", srv.URL+path+"/start", operatorTok, "", nil)
					if c != 409 || !strings.Contains(body, `"code":"budget_resume_blocked"`) ||
						!strings.Contains(body, `"scope":"`+scope+`"`) {
						t.Fatalf("start with a reached %s ledger: %d %s", scope, c, body)
					}
				}
				saved, err := app.Store.GetGraphRecord(ctx, graph)
				if err != nil || saved.RunID != rec.RunID || saved.DesiredState != domain.GraphStopped {
					t.Fatalf("blocked start created a run: %+v %v", saved, err)
				}
				eventtest.Capture(t, app.Store, domain.EvBudgetGraphTransition)
				return
			default:
				if c := call(t, srv, "PUT", definitions, adminTok, "application/json",
					`{"budgets":[{"scope":"user","id":"alice","limitMicros":40}]}`, nil); c != 200 {
					t.Fatalf("increase: %d", c)
				}
			}

			wantOutcome := map[string]string{
				"remove then resume":             "not_applicable",
				"increase then automatic resume": "resumed",
				"increase with a second blocker": "budget_reached",
			}[scenario]
			waitFor(t, 12*time.Second, "budget transition", func() bool {
				events, err := app.Store.ListEvents(ctx, storage.EventFilter{GraphID: graph, Types: []domain.EventType{domain.EvBudgetGraphTransition}})
				if err != nil {
					t.Fatal(err)
				}
				return slices.ContainsFunc(events, func(event *domain.Event) bool {
					return strings.Contains(string(event.Data), `"scope":"user"`) &&
						strings.Contains(string(event.Data), `"outcome":"`+wantOutcome+`"`)
				})
			})
			wantState := domain.GraphRunning
			if scenario == "increase with a second blocker" {
				wantState = domain.GraphPaused
			}
			saved, err := app.Store.GetGraphRecord(ctx, graph)
			if err != nil || saved.DesiredState != wantState || saved.RunID != rec.RunID {
				t.Fatalf("wrong lifecycle state after edit: %+v %v", saved, err)
			}
			var view cost.View
			if c := call(t, srv, "GET", definitions+"/"+scope+"/"+id, viewerTok, "", "", &view); c != 200 || view.Paused {
				t.Fatalf("old hold still visible: %d %+v", c, view)
			}
			if strings.HasPrefix(scenario, "increase") && (view.Limit != 40 || view.Reached || view.Remaining != 10) {
				t.Fatalf("increased limit not reflected in the ledger: %+v", view)
			}
			eventtest.Capture(t, app.Store, domain.EvBudgetGraphTransition)
		})
	}
}

func seedLifecycleLedger(t *testing.T, st storage.Store, graph, run, scope, id string) {
	t.Helper()
	raw, err := json.Marshal(cost.Ledger{Scope: scope, ID: id, Currency: "USD", Limit: 10, Compacted: 30, Reached: true,
		Entries: map[string]cost.Entry{}, PausedGraphs: []string{graph},
		GraphActions: map[string]cost.GraphAction{graph: {Action: cost.HoldGraph, RunID: run}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.PutSetting(t.Context(), cost.BudgetPrefix+scope+"/"+id, raw, 0, "fixture"); err != nil {
		t.Fatal(err)
	}
}
