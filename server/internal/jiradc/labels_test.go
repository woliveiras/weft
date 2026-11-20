package jiradc

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/storage"
)

func TestLabelChangeValidation(t *testing.T) {
	for _, change := range []LabelChange{
		{}, {Add: "invalid label"}, {Remove: []string{"../bad?"}},
		{Remove: []string{""}},
		{Remove: make([]string, 6)},
	} {
		if change.validate() == nil {
			t.Fatalf("invalid change accepted: %#v", change)
		}
	}
	for _, change := range []LabelChange{{Add: "weft:wip", Remove: []string{"weft:todo"}}} {
		if err := change.validate(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLabelsUseRelativeUpdatesAndNumericIdentity(t *testing.T) {
	for _, mode := range []string{"claim", "assigned-human", "foreign", "denied", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			writes := 0
			c, _ := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "GET" {
					body := fixtureIssue
					if mode == "foreign" {
						body = strings.Replace(body, `"project":{"id":"10001"}`, `"project":{"id":"10099"}`, 1)
					}
					if mode == "assigned-human" {
						body = strings.Replace(body, `"assignee":null`, `"assignee":{"key":"human-id","name":"human"}`, 1)
					}
					fmt.Fprint(w, body)
					return
				}
				writes++
				if r.Method != "PUT" || r.URL.Path != "/jira/rest/api/2/issue/10007" {
					t.Error("wrong mutation target", r.Method, r.URL.Path)
				}
				var body map[string]json.RawMessage
				if json.NewDecoder(r.Body).Decode(&body) != nil {
					t.Error("invalid body")
				}
				if string(body["update"]) != `{"labels":[{"remove":"weft:todo"},{"add":"weft:wip"}]}` || body["fields"] != nil {
					t.Error("not a relative labels-only update", body)
				}
				if mode == "denied" {
					w.WriteHeader(403)
					return
				}
				if mode == "unknown" {
					conn, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					_ = conn.Close()
					return
				}
				w.WriteHeader(204)
			})
			change := LabelChange{Add: "weft:wip", Remove: []string{"weft:todo"}}
			err := c.ChangeLabels(t.Context(), "APP-7", change)
			wantError := mode == "foreign" || mode == "denied" || mode == "unknown"
			if (err != nil) != wantError {
				t.Fatal(err)
			}
			wantWrites := 1
			if mode == "foreign" {
				wantWrites = 0
			}
			if writes != wantWrites {
				t.Fatal(writes)
			}
		})
	}
}

func TestLabelsRecheckStoredWriteAuthorityAfterPreflight(t *testing.T) {
	m, _ := connectionManager(t)
	token := "restricted-fixture-token"
	in := ConnectionInput{Name: "jira", Kind: "jira-dc", ProjectID: "10001", Enabled: true, ProjectRestricted: true, Token: &token}
	writes := 0
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			writes++
			w.WriteHeader(204)
			return
		}
		in.Token = nil
		in.Enabled = false
		if _, err := m.Put(r.Context(), in, 1, "admin"); err != nil {
			t.Error(err)
		}
		fmt.Fprint(w, fixtureIssue)
	}))
	defer s.Close()
	in.SiteURL = s.URL
	if _, err := m.Put(t.Context(), in, 0, "admin"); err != nil {
		t.Fatal(err)
	}
	c, err := m.OpenForWrite(t.Context(), in.Name, 1)
	if err != nil {
		t.Fatal(err)
	}
	c.http = s.Client()
	if err := c.ChangeLabels(t.Context(), "APP-7", LabelChange{Add: "weft:wip"}); !errors.Is(err, storage.ErrConflict) || writes != 0 {
		t.Fatal("stale write dispatched", err, writes)
	}
}

func TestLabelWriteDeniedBeforeHTTPAndProjectAccessIdentity(t *testing.T) {
	requests := 0
	c, _ := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/jira/rest/api/2/project/10001" {
			t.Error("wrong project scope")
		}
		fmt.Fprint(w, `{"id":"10099"}`)
	})
	c.writesEnabled = false
	if err := c.ChangeLabels(t.Context(), "APP-7", LabelChange{Add: "weft:wip"}); !errors.Is(err, ErrWriteDisabled) || requests != 0 {
		t.Fatal(err, requests)
	}
	c.writesEnabled = true
	for _, change := range []LabelChange{{Add: "bad label"}, {Add: "fixture%2Dsecret"}, {Add: "fixture-secret"}} {
		if err := c.ChangeLabels(t.Context(), "APP-7", change); err == nil || requests != 0 {
			t.Fatal(err, requests)
		}
	}
	if err := c.ProjectAccess(t.Context()); err == nil || requests != 1 {
		t.Fatal("foreign project accepted", err, requests)
	}
}

func TestProjectAccessChecksNumericIdentityAndErrors(t *testing.T) {
	for _, mode := range []string{"success", "403", "404", "invalid"} {
		t.Run(mode, func(t *testing.T) {
			c, _ := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != "/jira/rest/api/2/project/10001" {
					t.Error("wrong project request")
				}
				switch mode {
				case "403":
					w.WriteHeader(403)
				case "404":
					w.WriteHeader(404)
				case "invalid":
					fmt.Fprint(w, `{}`)
				default:
					fmt.Fprint(w, `{"id":"10001"}`)
				}
			})
			if err := c.ProjectAccess(t.Context()); (err != nil) != (mode != "success") {
				t.Fatal(err)
			}
		})
	}
}
