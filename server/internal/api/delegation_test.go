package api_test

import (
	"encoding/json"
	"github.com/woliveiras/weft/server/internal/delegation"
	"strings"
	"testing"
)

func TestDelegationConfigurationAPI(t *testing.T) {
	srv, _ := setupApp(t, nil)
	var initial delegation.PolicyView
	if c := call(t, srv, "GET", "/api/v1/delegation-policy", viewerTok, "", "", &initial); c != 200 || initial.Revision != 0 {
		t.Fatal(c, initial)
	}
	p := initial.Policy
	p.ApprovalMode = "automatic"
	b, _ := json.Marshal(p)
	for _, token := range []string{viewerTok, operatorTok} {
		if c, _, _ := rawCall(t, "PUT", srv.URL+"/api/v1/delegation-policy", token, string(b), map[string]string{"If-Match": "0"}); c != 403 {
			t.Fatal("role bypass", c)
		}
	}
	if c, _, _ := rawCall(t, "PUT", srv.URL+"/api/v1/delegation-policy", adminTok, string(b), nil); c != 400 {
		t.Fatal("missing revision", c)
	}
	if c, _, _ := rawCall(t, "PUT", srv.URL+"/api/v1/delegation-policy", adminTok, string(b), map[string]string{"If-Match": "0"}); c != 200 {
		t.Fatal(c)
	}
	if c, _, _ := rawCall(t, "PUT", srv.URL+"/api/v1/delegation-policy", adminTok, string(b), map[string]string{"If-Match": "0"}); c != 409 {
		t.Fatal("stale write", c)
	}
	profile := `{"id":"review","name":"Review","instructions":"Review text","enabled":true,"maxOutputTokens":100}`
	for _, token := range []string{viewerTok, operatorTok} {
		if c := call(t, srv, "POST", "/api/v1/delegation-profiles", token, "application/json", profile, nil); c != 403 {
			t.Fatal(c)
		}
	}
	if c := call(t, srv, "POST", "/api/v1/delegation-profiles", adminTok, "application/json", profile, nil); c != 201 {
		t.Fatal(c)
	}
	if c := call(t, srv, "POST", "/api/v1/delegation-profiles", adminTok, "application/json", profile, nil); c != 409 {
		t.Fatal(c)
	}
	if c, _, _ := rawCall(t, "PUT", srv.URL+"/api/v1/delegation-profiles/review", adminTok, strings.Replace(profile, `"enabled":true`, `"enabled":false`, 1), map[string]string{"If-Match": "1"}); c != 200 {
		t.Fatal(c)
	}
	var v delegation.ProfileView
	if c := call(t, srv, "GET", "/api/v1/delegation-profiles/review", viewerTok, "", "", &v); c != 200 || v.Revision != 2 || v.Profile.Enabled {
		t.Fatal(c, v)
	}
	if c, _, _ := rawCall(t, "PUT", srv.URL+"/api/v1/delegation-profiles/review", adminTok, profile, map[string]string{"If-Match": "1"}); c != 409 {
		t.Fatal(c)
	}
	for _, body := range []string{`{}`, profile + ` {}`, strings.Replace(profile, `"maxOutputTokens":100`, `"maxOutputTokens":0`, 1), strings.Replace(profile, `"instructions":"Review text"`, `"tools":["shell.exec"]`, 1), strings.Replace(profile, `"instructions":"Review text"`, `"model":"other"`, 1)} {
		if c := call(t, srv, "POST", "/api/v1/delegation-profiles", adminTok, "application/json", body, nil); c != 400 {
			t.Fatal(c, body)
		}
	}
	for _, body := range []string{`{}`, string(b) + ` {}`, strings.Replace(string(b), `"maxDepth":1`, `"maxDepth":5`, 1), strings.Replace(string(b), `"maxConcurrentChildren":4`, `"maxConcurrentChildren":99999`, 1)} {
		if c, _, _ := rawCall(t, "PUT", srv.URL+"/api/v1/delegation-policy", adminTok, body, map[string]string{"If-Match": "1"}); c != 400 {
			t.Fatal(c, body)
		}
	}
	p.MaxDepth = 2
	b, _ = json.Marshal(p)
	if c, _, _ := rawCall(t, "PUT", srv.URL+"/api/v1/delegation-policy", adminTok, string(b), map[string]string{"If-Match": "1"}); c != 200 {
		t.Fatal("bounded recursion rejected", c)
	}
	var saved delegation.PolicyView
	if c := call(t, srv, "GET", "/api/v1/delegation-policy", viewerTok, "", "", &saved); c != 200 || saved.Policy.MaxDepth != 2 || saved.Revision != 2 {
		t.Fatal("recursive policy not persisted", c, saved)
	}
}
