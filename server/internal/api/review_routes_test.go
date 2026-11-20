package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/reviewroute"
	"github.com/woliveiras/weft/server/internal/scm"
	"github.com/woliveiras/weft/server/internal/storage"
)

func TestReviewRouteAPIActivationCASAndRunningDispatch(t *testing.T) {
	srv, a := setupApp(t, nil)
	synthetic := "route-fixture-token"
	c, err := a.SCM.Save(t.Context(), scm.Input{Name: "native-scm", Kind: "github", Repository: "acme/app", Token: &synthetic}, "admin", false)
	if err != nil {
		t.Fatal(err)
	}
	g := &domain.Graph{Metadata: domain.GraphMetadata{ID: "portable-review"}, Spec: domain.GraphSpec{Nodes: []domain.Node{{ID: "reviewer", Runtime: domain.RuntimeRef{Kind: domain.RuntimeEcho}, Loop: domain.LoopSpec{Mode: domain.LoopEvent}, Tools: []domain.ToolGrant{{Name: scm.CapPullRequestRead, Repositories: []string{c.Name}}, {Name: scm.CapReviewSummary, Repositories: []string{c.Name}}}}}, Channels: []domain.Channel{{ID: "requests", To: []string{"reviewer"}}}}}
	if _, err = a.Store.SaveGraph(t.Context(), g, 0); err != nil {
		t.Fatal(err)
	}
	if err = a.Store.PutGraphRecord(t.Context(), &domain.GraphRecord{ID: g.Metadata.ID, Version: 1, State: domain.GraphStopped, DesiredState: domain.GraphStopped}); err != nil {
		t.Fatal(err)
	}
	in := reviewroute.Input{Name: "auto-review", Connection: c.Name, ConnectionRevision: c.UpdatedAt.UTC().Format(time.RFC3339Nano), Provider: c.Kind, Instance: "https://api.github.com", Repository: c.Repository, GraphID: g.Metadata.ID, GraphVersion: 1, ChannelID: "requests", NodeID: "reviewer", Enabled: true}
	body, _ := json.Marshal(map[string]any{"route": in, "confirm": true})
	for _, tok := range []string{viewerTok, operatorTok} {
		if code := call(t, srv, "POST", "/api/v1/review-routes", tok, "application/json", string(body), nil); code != 403 {
			t.Fatal("role", code)
		}
	}
	before, _ := a.Store.ListEvents(t.Context(), storage.EventFilter{Ascending: true, Limit: 1000})
	if code := call(t, srv, "POST", "/api/v1/review-routes/preview", adminTok, "application/json", string(body), nil); code != 200 {
		t.Fatal("preview", code)
	}
	after, _ := a.Store.ListEvents(t.Context(), storage.EventFilter{Ascending: true, Limit: 1000})
	if len(before) != len(after) {
		t.Fatal("preview wrote activation")
	}
	var saved reviewroute.Route
	if code := call(t, srv, "POST", "/api/v1/review-routes", adminTok, "application/json", string(body), &saved); code != 201 || saved.Revision != 1 || saved.ActivationSeq < 1 {
		t.Fatal("save", code, saved)
	}
	code, headers, raw := rawCall(t, "GET", srv.URL+"/api/v1/review-routes/auto-review", viewerTok, "", nil)
	if code != 200 || headers.Get("ETag") != `"1"` || strings.Contains(raw, synthetic) {
		t.Fatal(code, headers, raw)
	}
	// Event delivery against a stopped graph is retained as an independent failure;
	// the route never starts it to process the event.
	event := domain.NewEvent(domain.EvPullRequestOpened, "task-flow", map[string]any{"connection": c.Name, "connectionRevision": in.ConnectionRevision, "provider": c.Kind, "instance": in.Instance, "repository": c.Repository, "number": 7, "url": "https://github.com/acme/app/pull/7", "headSha": strings.Repeat("a", 40), "baseSha": strings.Repeat("b", 40), "reviewRevisionAvailable": true})
	if err = a.Store.AppendEvent(t.Context(), event); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		_ = a.ReviewRoutes.Tick(t.Context())
	}
	receipts, _, err := a.ReviewRoutes.ReceiptsPage(t.Context(), "", 100)
	if err != nil || len(receipts) != 1 || receipts[0].State != "failed" {
		t.Fatal(receipts, err)
	}
	rec, err := a.Store.GetGraphRecord(t.Context(), g.Metadata.ID)
	if err == nil && (rec.DesiredState == domain.GraphRunning || rec.RunID != "") {
		t.Fatal("route started graph", rec)
	}
	var receipt reviewroute.Receipt
	if code := call(t, srv, "GET", "/api/v1/review-route-receipts/"+receipts[0].ID, viewerTok, "", "", &receipt); code != 200 || receipt.MessageID == "" {
		t.Fatal("receipt", code, receipt)
	}
	// Once explicitly started by the fixture operator, a new scoped revision
	// reaches the real memory broker and echo runtime through the app callback.
	if err = a.Engine.GraphAction(t.Context(), g.Metadata.ID, "start", "admin"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		states, e := a.Store.ListReplicaStatus(t.Context(), g.Metadata.ID)
		if e == nil && len(states) == 1 && states[0].State == domain.ReplicaIdle {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("runtime not loaded")
		}
		time.Sleep(10 * time.Millisecond)
	}
	failedPath := "/api/v1/review-route-receipts/" + receipt.ID + "/retry"
	if code := call(t, srv, "POST", failedPath, operatorTok, "application/json", `{"confirm":true}`, nil); code != 403 {
		t.Fatal("retry role", code)
	}
	code, _, raw = rawCall(t, "POST", srv.URL+failedPath, adminTok, `{"confirm":true}`, map[string]string{"If-Match": "999"})
	if code != 409 {
		t.Fatal("retry CAS", code, raw)
	}
	code, _, raw = rawCall(t, "POST", srv.URL+failedPath, adminTok, `{"confirm":true}`, map[string]string{"If-Match": strconv.FormatInt(receipt.Revision, 10)})
	if code != 200 || !strings.Contains(raw, `"state":"queued"`) {
		t.Fatal("preflight retry", code, raw)
	}
	second := domain.NewEvent(domain.EvPullRequestOpened, "task-flow", map[string]any{"connection": c.Name, "connectionRevision": in.ConnectionRevision, "provider": c.Kind, "instance": in.Instance, "repository": c.Repository, "number": 8, "url": "https://github.com/acme/app/pull/8", "headSha": strings.Repeat("c", 40), "baseSha": strings.Repeat("b", 40), "reviewRevisionAvailable": true})
	if err = a.Store.AppendEvent(t.Context(), second); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err = a.ReviewRoutes.Tick(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	var delivered reviewroute.Receipt
	rows, _, err := a.ReviewRoutes.ReceiptsPage(t.Context(), "", 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range rows {
		if v.Number == 8 {
			delivered = v
		}
	}
	if delivered.State != "published" || delivered.DispatchRunID == "" {
		t.Fatal("dispatch", delivered)
	}
	deadline = time.Now().Add(5 * time.Second)
	for {
		executions, e := a.Store.ListExecutions(t.Context(), storage.ExecutionFilter{GraphID: g.Metadata.ID, Limit: 100})
		found := false
		for _, x := range executions {
			if x.Status == domain.ExecSucceeded && slices.Contains(x.InputIDs, delivered.MessageID) {
				found = true
			}
		}
		if e == nil && found {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("review execution missing", executions, e)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if code := call(t, srv, "POST", "/api/v1/review-route-receipts/"+delivered.ID+"/reconcile", viewerTok, "application/json", `{"confirm":true}`, nil); code != 403 {
		t.Fatal("reconcile role", code)
	}
	var history struct {
		Items []reviewroute.Receipt
		Next  string
	}
	if code := call(t, srv, "GET", "/api/v1/review-route-receipts", viewerTok, "", "", &history); code != 200 || len(history.Items) != 2 {
		t.Fatal("history", code, history)
	}
	actualDispatch := a.ReviewRoutes.Dispatch
	a.ReviewRoutes.Dispatch = func(ctx context.Context, r reviewroute.Route, v reviewroute.Receipt) (string, error) {
		_, e := actualDispatch(ctx, r, v)
		if e != nil {
			return "", e
		}
		return "", errors.New("synthetic lost dispatch response")
	}
	third := domain.NewEvent(domain.EvPullRequestOpened, "task-flow", map[string]any{"connection": c.Name, "connectionRevision": in.ConnectionRevision, "provider": c.Kind, "instance": in.Instance, "repository": c.Repository, "number": 9, "url": "https://github.com/acme/app/pull/9", "headSha": strings.Repeat("e", 40), "baseSha": strings.Repeat("b", 40), "reviewRevisionAvailable": true})
	if err = a.Store.AppendEvent(t.Context(), third); err != nil {
		t.Fatal(err)
	}
	_ = a.ReviewRoutes.Tick(t.Context())
	rows, _, err = a.ReviewRoutes.ReceiptsPage(t.Context(), "", 100)
	if err != nil {
		t.Fatal(err)
	}
	var uncertain reviewroute.Receipt
	for _, v := range rows {
		if v.Number == 9 {
			uncertain = v
		}
	}
	if uncertain.State != "unknown" {
		t.Fatal("uncertain dispatch", uncertain)
	}
	reconcilePath := "/api/v1/review-route-receipts/" + uncertain.ID + "/reconcile"
	code, _, raw = rawCall(t, "POST", srv.URL+reconcilePath, operatorTok, `{"confirm":true}`, map[string]string{"If-Match": strconv.FormatInt(uncertain.Revision, 10)})
	if code != 200 || !strings.Contains(raw, `"state":"published"`) {
		t.Fatal("history reconciliation", code, raw)
	}
	a.ReviewRoutes.Dispatch = actualDispatch
	in.Enabled = false
	body, _ = json.Marshal(map[string]any{"route": in, "confirm": true})
	for _, match := range []string{"", "0", "999"} {
		code, _, _ = rawCall(t, "PUT", srv.URL+"/api/v1/review-routes/auto-review", adminTok, string(body), map[string]string{"If-Match": match})
		if code == 200 {
			t.Fatal("invalid revision", match)
		}
	}
	code, _, raw = rawCall(t, "PUT", srv.URL+"/api/v1/review-routes/auto-review", adminTok, string(body), map[string]string{"If-Match": "1"})
	if code != 200 {
		t.Fatal("disable", code, raw)
	}
	// Requiring both named grants prevents an event route from expanding authority.
	in.Name = "missing-grant"
	in.Enabled = true
	g.Metadata.ID = "no-grant"
	g.Spec.Nodes[0].Tools = nil
	g.Metadata.Version = 0
	if _, err = a.Store.SaveGraph(t.Context(), g, 0); err != nil {
		t.Fatal(err)
	}
	in.GraphID = g.Metadata.ID
	body, _ = json.Marshal(map[string]any{"route": in, "confirm": true})
	code, _, raw = rawCall(t, "POST", srv.URL+"/api/v1/review-routes", adminTok, string(body), nil)
	if code != 400 || !strings.Contains(raw, "explicit repository-scoped") {
		t.Fatal("grant validation", code, raw)
	}
	var page struct {
		Items []reviewroute.Route
		Next  string
	}
	if code := call(t, srv, "GET", "/api/v1/review-routes", viewerTok, "", "", &page); code != 200 || len(page.Items) != 1 {
		t.Fatal("page", code, fmt.Sprint(page))
	}
}
