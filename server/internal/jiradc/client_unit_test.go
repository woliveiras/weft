package jiradc

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestConnectionValidation(t *testing.T) {
	for _, site := range []string{"", "http://jira.example", "https://u:p@jira.example", "https://jira.example?secret=x", "https://jira.example?", "https://jira.example#f", "https://jira.example/a/../b", "https://jira.example/%2f", "https://jira.example/rest/api/2"} {
		if _, err := New(site, "10001", "fixture"); err == nil {
			t.Fatal("accepted site", site)
		}
	}
	for _, project := range []string{"", "APP", "0", "1 OR 1=1", "../1"} {
		if _, err := New("https://jira.example", project, "fixture"); err == nil {
			t.Fatal("accepted project", project)
		}
	}
	for _, token := range []string{"", " \n", "a\nb", strings.Repeat("x", 4097)} {
		if _, err := New("https://jira.example", "10001", token); err == nil {
			t.Fatal("accepted invalid credential")
		}
	}
	c, err := New("https://jira.example/context/", "10001", "fixture")
	if err != nil || c.site != "https://jira.example/context" {
		t.Fatal(c, err)
	}
}
func TestSearchPageBoundaries(t *testing.T) {
	raw := `{"startAt":0,"maxResults":50,"total":1,"issues":[{}]}`
	var p SearchPage
	_ = json.Unmarshal([]byte(raw), &p)
	if next, err := p.next(0); err != nil || next != -1 {
		t.Fatal(next, err)
	}
	for _, bad := range []string{`{}`, `{"startAt":0,"maxResults":50,"total":1,"issues":[]}`, `{"startAt":0,"maxResults":51,"total":0,"issues":[]}`, `{"startAt":1,"maxResults":50,"total":0,"issues":[]}`, `{"startAt":0,"maxResults":50,"total":-1,"issues":[]}`} {
		var p SearchPage
		_ = json.Unmarshal([]byte(bad), &p)
		if _, err := p.next(0); err == nil {
			t.Fatal("accepted page", bad)
		}
	}
}
func TestCredentialReflectionRejectedAfterDecoding(t *testing.T) {
	token := `fixture<&"token`
	for _, v := range []any{token, map[string]any{"nested": []any{token}}, "fixture%3C%26%22token", map[string]any{"fixture%3C%26%22token": "value"}} {
		raw, _ := json.Marshal(v)
		var decoded any
		_ = json.Unmarshal(raw, &decoded)
		if !containsSecret(decoded, token, 0) {
			t.Fatal("missed reflected credential")
		}
	}
	if containsSecret(map[string]any{"description": "ordinary issue text"}, token, 0) {
		t.Fatal("ordinary content rejected")
	}
}
