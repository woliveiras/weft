package scm

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestBitbucketAPITokenAuthValidation(t *testing.T) {
	for _, tc := range []struct {
		kind, mode, username string
		valid                bool
	}{
		{"bitbucket-cloud", "", "", true},
		{"bitbucket-cloud", AuthBearer, "", true},
		{"github", "", "", true},
		{"bitbucket-cloud", AuthAPIToken, "operator@example.invalid", true},
		{"bitbucket-cloud", AuthAPIToken, "", false},
		{"bitbucket-cloud", AuthAPIToken, "operator", false},
		{"bitbucket-cloud", AuthAPIToken, "Operator <operator@example.invalid>", false},
		{"bitbucket-cloud", AuthAPIToken, ":operator@example.invalid", false},
		{"bitbucket-cloud", AuthAPIToken, "operator@example.invalid\r\nX: injected", false},
		{"bitbucket-cloud", AuthBearer, "operator@example.invalid", false},
		{"bitbucket-cloud", "unknown", "", false},
		{"github", AuthAPIToken, "operator@example.invalid", false},
		{"bitbucket-dc", AuthAPIToken, "operator@example.invalid", false},
	} {
		t.Run(tc.kind+"/"+tc.mode+"/"+tc.username, func(t *testing.T) {
			if got := len(validateAuth(tc.kind, tc.mode, tc.username)) == 0; got != tc.valid {
				t.Fatalf("valid=%v, want %v", got, tc.valid)
			}
		})
	}
	for _, value := range []string{"", " ", "api\ttoken", "api\x00token", "api\ntoken"} {
		if validAPIToken(value) {
			t.Fatalf("accepted control/empty token")
		}
	}
	if !validAPIToken("synthetic-api-token") {
		t.Fatal("rejected valid token")
	}
}

func TestAPITokenCredentialScopeRequiresReplacement(t *testing.T) {
	old := Connection{Kind: "bitbucket-cloud", AuthType: AuthAPIToken, Username: "operator@example.invalid", Repository: "acme/app", APIURL: "https://api.bitbucket.org/2.0", GitURL: "https://bitbucket.org/acme/app.git"}
	in := Input{Kind: old.Kind, AuthType: old.AuthType, Username: old.Username, Repository: old.Repository, APIURL: old.APIURL, GitURL: old.GitURL}
	if credentialScopeChanged(old, in) {
		t.Fatal("unchanged scope must preserve")
	}
	for _, change := range []func(*Input){
		func(x *Input) { x.Kind = "github" }, func(x *Input) { x.Repository = "acme/other" },
		func(x *Input) { x.APIURL = "https://other.invalid" }, func(x *Input) { x.GitURL = "https://other.invalid/app.git" },
		func(x *Input) { x.Username = "another@example.invalid" }, func(x *Input) { x.AuthType = AuthBearer; x.Username = "" },
	} {
		changed := in
		change(&changed)
		if !credentialScopeChanged(old, changed) {
			t.Fatalf("changed scope accepted: %+v", changed)
		}
	}
	legacy := Connection{Kind: "bitbucket-cloud", Repository: "acme/app"}
	if credentialScopeChanged(legacy, Input{Kind: legacy.Kind, AuthType: AuthBearer, Repository: "acme/other"}) {
		t.Fatal("legacy Bearer update contract changed")
	}
}

func TestAPITokenFactoryHeadersAndDefensiveValidation(t *testing.T) {
	var authorization string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization = r.Header.Get("Authorization")
		fmt.Fprint(w, `{}`)
	}))
	defer srv.Close()
	f, _ := factory("bitbucket-cloud")
	c := Connection{Kind: "bitbucket-cloud", Repository: "acme/app", APIURL: srv.URL, AuthType: AuthAPIToken, Username: "operator@example.invalid"}
	p, err := f(c, "synthetic-api-token")
	if err != nil {
		t.Fatal(err)
	}
	if err = p.Comment(context.Background(), 1, "Review complete"); err != nil {
		t.Fatal(err)
	}
	if authorization != "Basic "+base64.StdEncoding.EncodeToString([]byte("operator@example.invalid:synthetic-api-token")) {
		t.Fatal("REST must use account email and API token")
	}
	if p.GitAuthHeader() != "Basic "+base64.StdEncoding.EncodeToString([]byte("x-bitbucket-api-token-auth:synthetic-api-token")) {
		t.Fatal("Git must use the documented API-token username")
	}
	c.Username = ""
	if _, err = f(c, "synthetic-api-token"); err == nil {
		t.Fatal("factory accepted missing account")
	}
	c.Username = "operator@example.invalid"
	if _, err = f(c, ""); err == nil {
		t.Fatal("factory accepted missing token")
	}
}

func TestAPITokenRedactionIncludesRawAndEncodedCredentials(t *testing.T) {
	token := `synthetic-<&"token`
	header := BasicAuth("operator@example.invalid", token)
	for _, value := range []string{token, header, strings.TrimPrefix(header, "Basic ")} {
		if got := redactAuthMaterial(value, token, header); strings.Contains(got, value) || !strings.Contains(got, "[redacted]") {
			t.Fatal("raw or encoded credential survived redaction")
		}
		if !containsCredentialMaterial(Connection{AuthType: AuthAPIToken, Username: "operator@example.invalid"}, token, value) {
			t.Fatal("outgoing protected material was not detected")
		}
	}
	if containsCredentialMaterial(Connection{AuthType: AuthAPIToken, Username: "operator@example.invalid"}, token, "A clean review") {
		t.Fatal("ordinary summary rejected")
	}
}

func TestAPITokenRedactionIncludesPercentEncodedBasicPayloads(t *testing.T) {
	const token = "synthetic-token-for-encoding"
	header := BasicAuth("operator@example.invalid", token)
	payload := strings.TrimPrefix(header, "Basic ")
	for _, encoded := range []string{
		url.QueryEscape(payload),
		strings.ReplaceAll(url.QueryEscape(payload), "%3D", "%3d"),
		"%" + fmt.Sprintf("%02x", payload[0]) + url.QueryEscape(payload[1:]),
	} {
		raw := "https://bitbucket.org/acme/app/pull-requests/7?reflection=" + encoded + "&keep=%2Ftest%2f"
		got := redactAuthMaterial(raw, token, header)
		if strings.Contains(got, payload) || strings.Contains(got, encoded) || !strings.Contains(got, "[redacted]") || !strings.Contains(got, "&keep=%2Ftest%2f") {
			t.Fatalf("encoded Basic credential survived: %s", got)
		}
		if !containsCredentialMaterial(Connection{AuthType: AuthAPIToken, Username: "operator@example.invalid"}, token, raw) {
			t.Fatal("outgoing encoded credential not detected")
		}
	}
	if got := redactAuthMaterial("malformed%tail "+url.QueryEscape(payload), token, header); strings.Contains(got, url.QueryEscape(payload)) {
		t.Fatal("malformed escape bypassed redaction")
	}
}

func TestAPITokenRedirectAndCancellationDoNotDispatchOutsideScope(t *testing.T) {
	var leaked bool
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked = true }))
	defer sink.Close()
	var calls int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		http.Redirect(w, r, sink.URL+"/repository", http.StatusFound)
	}))
	defer upstream.Close()
	f, _ := factory("bitbucket-cloud")
	p, err := f(Connection{Kind: "bitbucket-cloud", Repository: "acme/app", AuthType: AuthAPIToken, Username: "operator@example.invalid", APIURL: upstream.URL}, "synthetic-api-token")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.Identity(context.Background()); err == nil || leaked || calls != 1 {
		t.Fatal("redirect followed or hidden", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = p.Identity(ctx); err != context.Canceled || leaked || calls != 1 {
		t.Fatal("cancelled request dispatched", err)
	}
}
