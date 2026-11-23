package mcpconfig

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestJiraRESTIdentityEmailRedactionKeepsReflectionGuard(t *testing.T) {
	basic := base64.StdEncoding.EncodeToString([]byte(restTestEmail + ":" + restTestToken))
	identity := `{"accountId":"human-account","emailAddress":` + fmt.Sprintf("%q", restTestEmail) + `,"displayName":"Fixture"}`
	for _, tc := range []struct{ name, method, path, raw string }{
		{"reporter", http.MethodGet, restTestPath, `{"id":9007199254740993,"fields":{"reporter":` + identity + `}}`},
		{"assignee creator embedded comments", http.MethodGet, restTestPath, `{"fields":{"assignee":` + identity + `,"creator":` + identity + `,"comment":{"comments":[{"author":` + identity + `,"updateAuthor":` + identity + `}]}}}`},
		{"myself", http.MethodGet, "/ex/jira/" + restTestCloud + "/rest/api/3/myself", identity},
		{"comments", http.MethodGet, "/ex/jira/" + restTestCloud + "/rest/api/3/issue/APP-7/comment", `{"comments":[{"author":` + identity + `,"updateAuthor":` + identity + `}]}`},
		{"single comment", http.MethodGet, "/ex/jira/" + restTestCloud + "/rest/api/3/issue/APP-7/comment/1", `{"author":` + identity + `}`},
		{"posted comment receipt", http.MethodPost, "/ex/jira/" + restTestCloud + "/rest/api/3/issue/APP-7/comment", `{"author":` + identity + `,"updateAuthor":` + identity + `}`},
		{"changelog author", http.MethodGet, "/ex/jira/" + restTestCloud + "/rest/api/3/issue/APP-7/changelog?startAt=0&maxResults=10", `{"total":1,"values":[{"id":"7","author":` + identity + `,"items":[{"field":"labels","toString":"weft:todo"}]}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !containsJiraRESTCredential([]byte(tc.raw), basic) {
				t.Fatal("fixture did not reproduce existing username reflection rejection")
			}
			out, err := redactJiraRESTIdentityEmail([]byte(tc.raw), basic, tc.method, tc.path)
			if err != nil || containsJiraRESTCredential(out, basic) || strings.Contains(string(out), restTestEmail) || strings.Contains(string(out), "emailAddress") || !strings.Contains(string(out), "human-account") {
				t.Fatalf("identity response not safely redacted: %v", err)
			}
			if strings.Contains(tc.raw, "9007199254740993") && !strings.Contains(string(out), "9007199254740993") {
				t.Fatal("provider numeric metadata rounded")
			}
		})
	}
	for _, tc := range []struct{ name, method, path, raw string }{
		{"summary", http.MethodGet, restTestPath, `{"fields":{"summary":` + fmt.Sprintf("%q", restTestEmail) + `}}`},
		{"unknown identity path", http.MethodGet, restTestPath, `{"custom":{"reporter":` + identity + `}}`},
		{"unrelated route", http.MethodGet, "/ex/jira/" + restTestCloud + "/rest/api/3/project/APP", `{"fields":{"reporter":` + identity + `}}`},
		{"unrelated method", http.MethodPut, restTestPath, `{"fields":{"reporter":` + identity + `}}`},
		{"partial email value", http.MethodGet, restTestPath, `{"fields":{"reporter":{"emailAddress":"prefix ` + restTestEmail + `"}}}`},
		{"encoded email value", http.MethodGet, restTestPath, `{"fields":{"reporter":{"emailAddress":"rest-fixture%40example.test"}}}`},
		{"changelog wrong method", http.MethodPost, "/ex/jira/" + restTestCloud + "/rest/api/3/issue/APP-7/changelog", `{"values":[{"author":` + identity + `}]}`},
		{"changelog unknown identity path", http.MethodGet, "/ex/jira/" + restTestCloud + "/rest/api/3/issue/APP-7/changelog", `{"values":[{"reviewer":` + identity + `}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := redactJiraRESTIdentityEmail([]byte(tc.raw), basic, tc.method, tc.path)
			if err == nil && !containsJiraRESTCredential(out, basic) {
				t.Fatal("credential email outside exact identity value was accepted")
			}
		})
	}
	for _, secret := range []string{restTestToken, basic, "Basic " + basic, restTestEmail + ":" + restTestToken, "%72est-scoped-fixture-token"} {
		raw, err := json.Marshal(map[string]any{"fields": map[string]any{"reporter": map[string]string{"emailAddress": secret}}})
		if err != nil {
			t.Fatal(err)
		}
		if out, err := redactJiraRESTIdentityEmail(raw, basic, http.MethodGet, restTestPath); err == nil && !containsJiraRESTCredential(out, basic) {
			t.Fatal("reusable secret removed as an identity email")
		}
	}
}

func TestJiraRESTIdentityEmailRedactionOverTLS(t *testing.T) {
	for _, tc := range []struct {
		name, method, path, raw string
		reject                  bool
	}{
		{"issue reporter", http.MethodGet, restTestPath, `{"id":9007199254740993,"key":"APP-7","fields":{"reporter":{"accountId":"human","emailAddress":"` + restTestEmail + `"}}}`, false},
		{"comment receipt", http.MethodPost, "/ex/jira/" + restTestCloud + "/rest/api/3/issue/APP-7/comment", `{"id":"1","author":{"accountId":"human","emailAddress":"` + restTestEmail + `"}}`, false},
		{"changelog author", http.MethodGet, "/ex/jira/" + restTestCloud + "/rest/api/3/issue/APP-7/changelog?startAt=0&maxResults=10", `{"total":1,"values":[{"id":"7","author":{"accountId":"human","emailAddress":"` + restTestEmail + `"},"items":[{"field":"labels","toString":"weft:todo"}]}]}`, false},
		{"changelog reflected token", http.MethodGet, "/ex/jira/" + restTestCloud + "/rest/api/3/issue/APP-7/changelog", `{"values":[{"author":{"accountId":"human","emailAddress":"` + restTestEmail + `"},"items":[{"toString":"` + restTestToken + `"}]}]}`, true},
		{"reflected outside identity", http.MethodGet, restTestPath, `{"fields":{"reporter":{"emailAddress":"` + restTestEmail + `"},"summary":"` + restTestEmail + `"}}`, true},
		{"duplicate hidden token", http.MethodGet, restTestPath, `{"fields":{"summary":"\u0072est-scoped-fixture-token"},"fields":{"reporter":{"emailAddress":"` + restTestEmail + `"}}}`, true},
		{"POST receipt token", http.MethodPost, "/ex/jira/" + restTestCloud + "/rest/api/3/issue/APP-7/comment", `{"author":{"emailAddress":"` + restTestEmail + `"},"body":"%72est-scoped-fixture-token"}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api, _, v, transport := restTLSFixture(t, func(w http.ResponseWriter, r *http.Request) {
				expected := "Basic " + base64.StdEncoding.EncodeToString([]byte(restTestEmail+":"+restTestToken))
				if r.Header.Get("Authorization") != expected || r.Method != tc.method || r.URL.RequestURI() != tc.path {
					t.Error("independent REST auth/route changed")
				}
				io.WriteString(w, tc.raw)
			})
			raw, status, err := api.Call(t.Context(), v.ID, "jira", v.Revision, tc.method, tc.path, nil)
			if status != 200 || transport.calls.Load() != 1 {
				t.Fatal("unexpected retry or status")
			}
			if tc.reject {
				if err == nil || len(raw) != 0 || strings.Contains(err.Error(), restTestEmail) || strings.Contains(err.Error(), restTestToken) {
					t.Fatal("unsafe REST reflection reached caller")
				}
				return
			}
			if err != nil || strings.Contains(string(raw), restTestEmail) || strings.Contains(string(raw), restTestToken) || !strings.Contains(string(raw), "human") {
				t.Fatalf("safe identity failed: %v", err)
			}
		})
	}
}

func TestJiraRESTIdentityRedactionRetainsResponseSizeBound(t *testing.T) {
	api, _, v, _ := restTLSFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, `{"fields":{"reporter":{"emailAddress":"`+restTestEmail+`"}},"text":"`+strings.Repeat("<", maxBody/6)+`"}`)
	})
	if raw, _, err := api.Call(t.Context(), v.ID, "jira", v.Revision, http.MethodGet, restTestPath, nil); err == nil || len(raw) != 0 {
		t.Fatal("redaction expanded output past response limit")
	}
}

func TestJiraRESTIdentityRedactionRejectsDuplicateAndEscapedSecrets(t *testing.T) {
	basic := base64.StdEncoding.EncodeToString([]byte(restTestEmail + ":" + restTestToken))
	identity := `{"emailAddress":` + fmt.Sprintf("%q", restTestEmail) + `}`
	for _, raw := range []string{
		`{"fields":{"summary":"` + restTestToken + `"},"fields":{"reporter":` + identity + `}}`,
		`{"fields":{"summary":"` + restTestEmail + `"},"fields":{"reporter":` + identity + `}}`,
		`{"fields":{"reporter":` + identity + `},"hidden":"\u002572est-scoped-fixture-token"}`,
		`{"fields":{"reporter":` + identity + `},"hidden":"\u0072est-scoped-fixture-token"}`,
		`{"fields":{"reporter":` + identity + `},"%72est-scoped-fixture-token":true}`,
		`{"fields":{"reporter":` + identity + `},"bad":"%XX","hidden":"%72est-scoped-fixture-token"}`,
		`{"fields":{"reporter":` + identity + `},"a":true,"a":false}`,
	} {
		if out, err := redactJiraRESTIdentityEmail([]byte(raw), basic, http.MethodGet, restTestPath); err == nil || len(out) != 0 || strings.Contains(err.Error(), restTestToken) || strings.Contains(err.Error(), restTestEmail) {
			t.Fatal("unsafe JSON/secret reached redaction result")
		}
	}
	degenerate := base64.StdEncoding.EncodeToString([]byte(restTestEmail + ":" + restTestEmail))
	if _, err := redactJiraRESTIdentityEmail([]byte(`{"fields":{"reporter":`+identity+`}}`), degenerate, http.MethodGet, restTestPath); err == nil {
		t.Fatal("token equal to username removed")
	}
}
