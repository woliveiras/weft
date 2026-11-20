package api_test

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/installation"
	"github.com/woliveiras/weft/server/internal/scm"
)

func TestSCMAPITokenAuthenticationThroughAPI(t *testing.T) {
	const email = "operator@example.invalid"
	const token = "synthetic-api-token-fixture"
	var calls int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if user, pw, ok := r.BasicAuth(); !ok || user != email || pw != token {
			t.Error("wrong API token auth")
			http.Error(w, "denied", 401)
			return
		}
		switch r.URL.Path {
		case "/repositories/acme/app":
			fmt.Fprint(w, `{"full_name":"acme/app","mainbranch":{"name":"main"}}`)
		case "/user/workspaces/acme/permissions/repositories":
			fmt.Fprint(w, `{"values":[{"repository":{"full_name":"acme/app"},"user":{"nickname":"bot"},"permission":"write"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	srv, a := setupApp(t, nil)
	body := fmt.Sprintf(`{"name":"bb-api","kind":"bitbucket-cloud","repository":"acme/app","apiUrl":%q,"authType":"api-token","username":%q,"token":%q}`, upstream.URL, email, token)
	for _, userToken := range []string{viewerTok, operatorTok} {
		if c := call(t, srv, "POST", "/api/v1/scm/connections", userToken, "application/json", body, nil); c != 403 {
			t.Fatal("non-admin create", c)
		}
	}
	if c, _, out := rawCall(t, "POST", srv.URL+"/api/v1/scm/connections", adminTok, body, nil); c != 201 || strings.Contains(out, token) || !strings.Contains(out, `"authType":"api-token"`) || !strings.Contains(out, `"username":"`+email+`"`) {
		t.Fatalf("create: %d %s", c, out)
	}
	var result struct {
		OK       bool
		Identity scm.Identity
	}
	if c := call(t, srv, "POST", "/api/v1/scm/connections/bb-api/test", operatorTok, "", "", &result); c != 200 || !result.OK || !result.Identity.CanPush || calls != 2 {
		t.Fatalf("test: %d %+v calls=%d", c, result, calls)
	}
	if c, _, out := rawCall(t, "GET", srv.URL+"/api/v1/scm/connections", viewerTok, "", nil); c != 200 || strings.Contains(out, token) || strings.Contains(out, base64.StdEncoding.EncodeToString([]byte(email+":"+token))) {
		t.Fatalf("list: %d", c)
	}
	saved, _ := a.Store.GetSetting(t.Context(), scm.SettingKey)
	if strings.Contains(string(saved.Value), token) {
		t.Fatal("plaintext saved")
	}
	update := fmt.Sprintf(`{"kind":"bitbucket-cloud","repository":"acme/app","apiUrl":%q,"authType":"api-token","username":%q}`, upstream.URL, "other@example.invalid")
	var bad apiError
	if c := call(t, srv, "PUT", "/api/v1/scm/connections/bb-api", adminTok, "application/json", update, &bad); c != 422 || bad.Details.Errors[0].Path != "token" {
		t.Fatalf("account switch without token: %d %+v", c, bad)
	}
	if calls != 2 {
		t.Fatal("save contacted upstream")
	}
}

func TestDeclarativeSCMAPITokenRoundTripAndScopeFence(t *testing.T) {
	srv, a := setupApp(t, nil)
	const token = "declarative-bb-api-token-fixture"
	t.Setenv("WEFT_SECRET_BB_API_FIXTURE", token)
	spec := "  kind: bitbucket-cloud\n  repository: acme/app\n  authType: api-token\n  username: operator@example.invalid\n  secretRef: {env: WEFT_SECRET_BB_API_FIXTURE}"
	configApplyRequest(t, srv, installation.Request{Files: []string{yamlObject("SCMConnection", "bb-api", spec)}})
	exported := exportConfig(t, srv)
	if !strings.Contains(exported, "authType: api-token") || !strings.Contains(exported, "username: operator@example.invalid") || !strings.Contains(exported, "preserve: true") || strings.Contains(exported, token) {
		t.Fatal("API-token export contract")
	}
	plan := configApplyRequest(t, srv, installation.Request{Files: []string{exported}})
	for _, object := range plan.Objects {
		if object.Result != "unchanged" {
			t.Fatalf("round trip changed: %+v", object)
		}
	}
	before, _ := a.SCM.Get(t.Context(), "bb-api")
	changed := strings.Replace(exported, "operator@example.invalid", "other@example.invalid", 1)
	plan = configRequest(t, srv, "plan", installation.Request{Files: []string{changed}})
	if plan.OK {
		t.Fatal("preserve accepted changed account")
	}
	after, _ := a.SCM.Get(t.Context(), "bb-api")
	if after.Username != before.Username || after.Token != before.Token || !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatal("failed plan changed connection")
	}
	changed = strings.ReplaceAll(changed, "preserve: true", "env: WEFT_SECRET_BB_API_FIXTURE")
	plan = configApplyRequest(t, srv, installation.Request{Files: []string{changed}})
	if !plan.OK {
		t.Fatal("explicit replacement refused")
	}
	after, _ = a.SCM.Get(t.Context(), "bb-api")
	if after.Username != "other@example.invalid" {
		t.Fatal("replacement not applied")
	}
}
