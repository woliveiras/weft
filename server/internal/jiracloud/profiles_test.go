package jiracloud

import (
	"errors"
	"testing"
)

func profileInput() Input {
	return Input{Name: "cloud-team", MCPConnection: "atlassian", MCPRevision: 1, CloudID: "11111111-2222-3333-4444-555555555555",
		SiteURL: "https://api.atlassian.com/ex/jira/11111111-2222-3333-4444-555555555555", ProjectID: "10001", Enabled: true, ProjectRestricted: true}
}

func TestProfileRequiresExplicitCloudScopeAndIndependentMCPRevision(t *testing.T) {
	good := profileInput()
	if _, err := normalize(good); err != nil {
		t.Fatal(err)
	}
	for _, edit := range []func(*Input){
		func(v *Input) { v.Name = "../profile" },
		func(v *Input) { v.MCPConnection = "https://mcp.example" },
		func(v *Input) { v.MCPRevision = 0 },
		func(v *Input) { v.CloudID = "APP" },
		func(v *Input) { v.CloudID = "22222222-2222-3333-4444-555555555555" },
		func(v *Input) { v.SiteURL = "https://team.atlassian.net" },
		func(v *Input) { v.SiteURL += "?token=unsafe" },
		func(v *Input) { v.SiteURL += "/rest/api/3" },
		func(v *Input) { v.SiteURL = "https://api.atlassian.com.example/ex/jira/" + v.CloudID },
		func(v *Input) { v.SiteURL = "https://user:pass@api.atlassian.com/ex/jira/" + v.CloudID },
		func(v *Input) { v.ProjectID = "APP" },
		func(v *Input) { v.ProjectID = "0" },
	} {
		in := good
		edit(&in)
		if _, err := normalize(in); !errors.Is(err, ErrInvalid) {
			t.Fatal("invalid profile accepted", in, err)
		}
	}
	if err := ValidateScope(good.SiteURL, good.ProjectID); err != nil {
		t.Fatal(err)
	}
	for _, site := range []string{"https://jira.example/jira", good.SiteURL + "/", good.SiteURL + "?x=1", "http://api.atlassian.com/ex/jira/" + good.CloudID} {
		if ValidateScope(site, good.ProjectID) == nil {
			t.Fatal("foreign/noncanonical backend scope accepted", site)
		}
	}
}

func TestProfileMissingStorageAndTransportFailClosed(t *testing.T) {
	m := Profiles{}
	if _, err := m.Get(t.Context(), "cloud-team"); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	if _, err := m.Preview(t.Context(), profileInput()); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	if _, err := m.List(t.Context()); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	if _, err := m.Put(t.Context(), profileInput(), -1, "operator"); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
}
