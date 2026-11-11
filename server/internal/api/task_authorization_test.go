package api_test

import (
	"encoding/json"
	"fmt"
	"github.com/woliveiras/weft/server/internal/taskauth"
	"github.com/woliveiras/weft/server/internal/taskconfig"
	"net/http"
	"strings"
	"testing"
)

func TestTaskAuthorizationAPI(t *testing.T) {
	srv := setup(t)
	call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/json", taskGraph, nil)
	var d taskconfig.Draft
	call(t, srv, "POST", "/api/v1/tasks", operatorTok, "application/json", taskBody, &d)
	path := "/api/v1/tasks/" + d.ID + "/authorization"
	var review taskauth.Review
	if c := call(t, srv, "GET", path, viewerTok, "", "", &review); c != 200 || review.Scope == "" || len(review.Capabilities) != 1 {
		t.Fatal(c, review)
	}
	put := func(token, body, revision string) int {
		req, _ := http.NewRequest("PUT", srv.URL+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("If-Match", revision)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		return res.StatusCode
	}
	b, _ := json.Marshal(map[string]any{"mode": "full", "scope": review.Scope, "confirm": true})
	if c := put(operatorTok, string(b), "0"); c != 403 {
		t.Fatal(c)
	}
	if c := put(adminTok, strings.Replace(string(b), "true", "false", 1), "0"); c != 400 {
		t.Fatal(c)
	}
	if c := put(adminTok, string(b), "0"); c != 200 {
		t.Fatal(c)
	}
	if c := put(adminTok, string(b), "0"); c < 400 {
		t.Fatal("stale write", c)
	}
	call(t, srv, "GET", path, viewerTok, "", "", &review)
	if review.Record.Mode != taskauth.Full || review.Record.Actor == "" || review.Record.Revision != 1 {
		t.Fatal(fmt.Sprint(review))
	}
	if c := put(adminTok, `{"mode":"invalid","confirm":true}`, "1"); c != 400 {
		t.Fatal(c)
	}
}
