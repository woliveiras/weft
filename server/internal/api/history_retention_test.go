package api_test

import (
	"context"
	"encoding/json"
	"github.com/woliveiras/weft/server/internal/environment"
	"github.com/woliveiras/weft/server/internal/storage"
	"testing"
	"time"
)

func TestHistoryRetentionAPI(t *testing.T) {
	srv, app := setupApp(t, nil)
	const path = "/api/v1/blocked-history-retention"
	const body = `{"policy":{"days":3,"maxEvents":20},"revision":0,"confirm":true}`
	for _, tok := range []string{viewerTok, operatorTok, adminTok} {
		var v environment.RetentionView
		if code := call(t, srv, "GET", path, tok, "", "", &v); code != 200 || v.Policy.Days != 7 {
			t.Fatal(code, v)
		}
	}
	for _, tok := range []string{viewerTok, operatorTok} {
		if code := call(t, srv, "PUT", path, tok, "application/json", body, nil); code != 403 {
			t.Fatal(code)
		}
	}
	if code := call(t, srv, "GET", path, "", "", "", nil); code != 401 {
		t.Fatal(code)
	}
	for _, body := range []string{
		`{"policy":{"days":3,"maxEvents":20},"revision":0}`, `{"policy":{"days":3,"maxEvents":20},"confirm":true}`,
		`{"policy":{"days":0,"maxEvents":20},"revision":0,"confirm":true}`,
		`{"policy":{"days":3,"maxEvents":10001},"revision":0,"confirm":true}`,
		`{"policy":{"days":1.5,"maxEvents":20},"revision":0,"confirm":true}`,
		`{"policy":{"days":3,"maxEvents":20},"revision":-1,"confirm":true}`,
		`{"policy":{"days":3,"maxEvents":20},"revision":0,"confirm":true,"unknown":1}`,
		`{} {}`, `null`,
	} {
		if code := call(t, srv, "PUT", path, adminTok, "application/json", body, nil); code != 400 {
			t.Fatal(code, body)
		}
	}
	doc := "apiVersion: weft.io/v1\nkind: Graph\nmetadata: {name: retention-test}\nspec:\n  nodes: [{id: worker, runtime: {kind: echo}}]\n"
	if code := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/yaml", doc, nil); code != 201 {
		t.Fatal(code)
	}
	rows := []environment.BlockedRequest{}
	now := time.Now().UTC().Add(-time.Second)
	for i := 0; i < 30; i++ {
		rows = append(rows, environment.BlockedRequest{ProxyRequest: environment.ProxyRequest{Time: now.Add(-time.Duration(i) * time.Hour), Outcome: "denied"}})
	}
	data, err := json.Marshal(environment.BlockedHistory{Requests: rows})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.Store.PutSetting(context.Background(), "environment/blocked-history/retention-test", data, 0, ""); err != nil {
		t.Fatal(err)
	}
	var v environment.RetentionView
	if code := call(t, srv, "PUT", path, adminTok, "application/json", body, &v); code != 200 || v.Policy.Days != 3 || v.Revision != 1 || v.UpdatedBy == "" {
		t.Fatal(code, v)
	}
	if code := call(t, srv, "PUT", path, adminTok, "application/json", body, nil); code != 409 {
		t.Fatal(code)
	}
	if code := call(t, srv, "GET", path, viewerTok, "", "", &v); code != 200 || v.Policy.MaxEvents != 20 || v.Revision != 1 {
		t.Fatal(code, v)
	}
	var history environment.BlockedHistory
	if code := call(t, srv, "GET", "/api/v1/graphs/retention-test/environment/blocked-history", viewerTok, "", "", &history); code != 200 || len(history.Requests) != 20 || history.RetentionDays != 3 || history.Limit != 20 {
		t.Fatal(code, history)
	}
	evs, err := app.Store.ListEvents(context.Background(), storage.EventFilter{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, ev := range evs {
		if ev.Type == "installation.history_retention.updated" {
			found++
		}
	}
	if found != 1 {
		t.Fatal("missing or duplicate retention change event", found)
	}

}
