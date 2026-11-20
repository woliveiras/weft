package jiradc

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

const fixtureTransitions = `{"transitions":[{"id":"31","name":"Review","to":{"id":"20","name":"Review"},"fields":{"resolution":{"required":true,"name":"Resolution","schema":{"type":"resolution"},"operations":["set"],"allowedValues":[{"id":"1","name":"Fixed"}]}}},{"id":"32","name":"In Review","to":{"id":"20","name":"Review"},"fields":{}},{"id":"33","name":"Revisão","to":{"id":"21","name":"Review"}}]}`

func TestTransitionDiscoveryUsesIssueScopedIDsAndCustomDisplayNames(t *testing.T) {
	calls := 0
	c, _ := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "GET" {
			t.Error("discovery wrote state")
		}
		if r.URL.Path == "/jira/rest/api/2/issue/APP-7" {
			fmt.Fprint(w, fixtureIssue)
			return
		}
		if r.URL.Path != "/jira/rest/api/2/issue/10007/transitions" || r.URL.Query().Get("expand") != "transitions.fields" {
			t.Error("unscoped metadata route", r.URL)
		}
		fmt.Fprint(w, fixtureTransitions)
	})
	d, err := c.DiscoverTransitions(t.Context(), "APP-7")
	if err != nil || calls != 3 || d.ProjectID != "10001" || d.IssueTypeID != "10002" || d.IssueID != "10007" || d.Current.ID != "10" || len(d.Transitions) != 3 {
		t.Fatal(d, err, calls)
	}
	if d.Transitions[0].ID == d.Transitions[1].ID || d.Transitions[0].To.ID != d.Transitions[1].To.ID || d.Transitions[0].To.ID == d.Transitions[2].To.ID {
		t.Fatal("display labels collapsed identities")
	}
	if !*d.Transitions[0].Fields["resolution"].Required || len(d.Transitions[1].Fields) != 0 || d.Transitions[1].Fields == nil || d.Transitions[2].Fields != nil {
		t.Fatal("required/known-empty/unavailable fields lost")
	}
}
func TestTransitionDiscoveryRefusesChangedOrForeignIssueAndRemoteFailures(t *testing.T) {
	for _, mode := range []string{"status", "type", "updated", "foreign", "denied", "missing", "reflection"} {
		t.Run(mode, func(t *testing.T) {
			reads, metadata := 0, 0
			c, _ := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/jira/rest/api/2/issue/APP-7" {
					reads++
					issue := fixtureIssue
					if mode == "foreign" {
						issue = strings.Replace(issue, `"project":{"id":"10001"}`, `"project":{"id":"10003"}`, 1)
					}
					if reads == 2 {
						switch mode {
						case "status":
							issue = strings.Replace(issue, `"status":{"id":"10"`, `"status":{"id":"11"`, 1)
						case "type":
							issue = strings.Replace(issue, `"issuetype":{"id":"10002"}`, `"issuetype":{"id":"10003"}`, 1)
						case "updated":
							issue = strings.Replace(issue, `"summary":`, `"updated":"changed","summary":`, 1)
						}
					}
					fmt.Fprint(w, issue)
					return
				}
				metadata++
				switch mode {
				case "denied":
					w.WriteHeader(403)
				case "missing":
					fmt.Fprint(w, `{}`)
				case "reflection":
					fmt.Fprint(w, strings.Replace(fixtureTransitions, "In Review", "fixture-secret", 1))
				default:
					fmt.Fprint(w, fixtureTransitions)
				}
			})
			d, err := c.DiscoverTransitions(t.Context(), "APP-7")
			if err == nil || d.IssueID != "" || strings.Contains(err.Error(), "fixture-secret") {
				t.Fatal("unsafe discovery", d, err)
			}
			if mode == "foreign" && metadata != 0 {
				t.Fatal("foreign project metadata fetched")
			}
		})
	}
}
func TestTransitionMetadataRejectsInvalidIdentityAndUnknownRequiredFields(t *testing.T) {
	for _, raw := range []string{`null`, `[{"id":"1","name":"Review","to":{"id":"20","name":"Review"}},{"id":"1","name":"Review","to":{"id":"21","name":"Review"}}]`, `[{"id":"1","name":"Review","to":{"id":"x","name":"Review"}}]`, `[{"id":"1","name":"Review","to":{"id":"20","name":"Review"},"fields":{"resolution":{"name":"Resolution","schema":{"type":"resolution"}}}}]`} {
		var v []Transition
		if json.Unmarshal([]byte(raw), &v) != nil {
			t.Fatal("bad unit fixture")
		}
		if validateTransitions(v) == nil {
			t.Fatal("invalid metadata accepted")
		}
	}
	if err := validateTransitions([]Transition{}); err != nil {
		t.Fatal("no available transitions must be valid", err)
	}
}
