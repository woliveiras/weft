package api_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
)

func TestBranchPublicationRouteChecksRoleScopeAndAvailability(t *testing.T) {
	srv, a := setupApp(t, nil)
	if got := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/yaml", checkpointDoc, nil); got != 201 {
		t.Fatal(got)
	}
	path := "/api/v1/graphs/cp-api/nodes/dev/replicas/0/git/publish"
	body := `{"commit":"` + strings.Repeat("a", 40) + `","previous":""}`
	for _, tc := range []struct {
		path, token, body string
		want              int
	}{
		{path, "", body, 401}, {path, viewerTok, body, 403}, {path, operatorTok, `{}`, 400},
		{path, operatorTok, body + strings.Repeat(" ", 1024), 400},
		{strings.Replace(path, "/0/", "/-1/", 1), operatorTok, body, 400},
		{strings.Replace(path, "/dev/", "/other/", 1), operatorTok, body, 404},
		{strings.Replace(path, "/cp-api/", "/unknown/", 1), operatorTok, body, 404},
		{path, operatorTok, body, 409},
	} {
		if got := call(t, srv, "POST", tc.path, tc.token, "application/json", tc.body, nil); got != tc.want {
			t.Fatalf("%s: got %d, want %d", tc.path, got, tc.want)
		}
	}
	evs, err := a.Store.ListEvents(t.Context(), storage.EventFilter{GraphID: "cp-api", Types: []domain.EventType{domain.EvBranchPublication}})
	if err != nil || len(evs) != 1 {
		t.Fatalf("audit %+v %v", evs, err)
	}
	var data struct{ Outcome, Commit string }
	if err := json.Unmarshal(evs[0].Data, &data); err != nil {
		t.Fatal(err)
	}
	if data.Outcome != "unavailable" || data.Commit != strings.Repeat("a", 40) || evs[0].Actor != "operator" || evs[0].ExecutionID != "" || evs[0].NodeID != "dev" {
		t.Fatalf("unexpected event %+v %s", evs[0], evs[0].Data)
	}
}
