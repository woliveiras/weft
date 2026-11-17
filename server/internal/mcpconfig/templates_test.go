package mcpconfig

import (
	"errors"
	"testing"
)

func TestTemplateValidation(t *testing.T) {
	for _, template := range Templates() {
		t.Run(template.ID, func(t *testing.T) {
			in := Input{ID: "connection", Name: "Connection", Service: template.Service, Endpoint: template.Endpoint, Template: &template.TemplateRef}
			if template.Service == "sentry" {
				in.OAuth = &OAuthConfig{Issuer: "https://mcp.sentry.dev", ClientID: "client", Scopes: []string{"org:read"}}
			}
			if template.Service == "gitlab" {
				in.OAuth = &OAuthConfig{Issuer: "https://gitlab.com", ClientID: "client", Scopes: []string{"mcp"}}
			}
			if err := validate(in); err != nil {
				t.Fatal(err)
			}
			in.Template = &TemplateRef{ID: template.ID, Version: 999}
			if !errors.Is(validate(in), ErrInvalid) {
				t.Fatal("unknown version accepted")
			}
			in.Template = &template.TemplateRef
			in.Endpoint = "https://other.example/mcp"
			if !errors.Is(validate(in), ErrInvalid) {
				t.Fatal("changed defaults accepted")
			}
			in.Template = nil
			in.OAuth = nil
			if err := validate(in); err != nil {
				t.Fatal("custom configuration rejected", err)
			}
		})
	}
	a := Templates()
	a[0].Name = "mutated"
	if Templates()[0].Name == "mutated" {
		t.Fatal("catalogue is mutable")
	}
}

func TestParameterizedTemplateBoundaries(t *testing.T) {
	var github, gitlab Template
	for _, tpl := range Templates() {
		if tpl.ID == "github" && tpl.Version == 2 {
			github = tpl
		}
		if tpl.ID == "gitlab" {
			gitlab = tpl
		}
	}
	for _, tc := range []struct {
		template Template
		params   map[string]string
		endpoint string
	}{
		{github, nil, "https://api.githubcopilot.com/mcp/readonly"},
		{github, map[string]string{"toolset": "repos"}, "https://api.githubcopilot.com/mcp/x/repos/readonly"},
		{gitlab, map[string]string{"instance": "https://gitlab.example/"}, "https://gitlab.example/api/v4/mcp"},
	} {
		got, err := templateEndpoint(tc.template, tc.params)
		if err != nil || got != tc.endpoint {
			t.Fatal(got, err)
		}
	}
	for _, params := range []map[string]string{{"toolset": "all"}, {"token": "secret"}, {"toolset": "../write"}} {
		if _, err := templateEndpoint(github, params); err == nil {
			t.Fatal("unsafe parameter accepted")
		}
	}
	for _, origin := range []string{"http://gitlab.example", "https://user:pass@gitlab.example", "https://gitlab.example/path", "https://gitlab.example?token=x", "https://gitlab.example?", "https://gitlab.example#", ""} {
		if _, err := templateEndpoint(gitlab, map[string]string{"instance": origin}); err == nil {
			t.Fatal("invalid origin", origin)
		}
	}
	in := Input{ID: "fixture", Name: "Fixture", Service: "github", Endpoint: github.Endpoint, Template: &github.TemplateRef, TemplateParameters: map[string]string{"toolset": "repos"}}
	if !errors.Is(validate(in), ErrInvalid) {
		t.Fatal("endpoint/parameter mismatch accepted")
	}
	in.Template = nil
	if !errors.Is(validate(in), ErrInvalid) {
		t.Fatal("orphan parameters accepted")
	}
}

func TestDatabricksTemplateBoundaries(t *testing.T) {
	var tpl Template
	for _, v := range Templates() {
		if v.ID == "databricks" {
			tpl = v
		}
	}
	params := map[string]string{"workspace": "https://workspace.example/", "service": "team.data.github-reader"}
	got, err := templateEndpoint(tpl, params)
	if err != nil || got != "https://workspace.example/ai-gateway/mcp-services/team.data.github-reader" {
		t.Fatal(got, err)
	}
	for _, service := range []string{"", "a.b", "a.b.c.d", "a.b/../c", "a.b.%2e", "a.b.c?token=secret", "a.b.c#x", "a.b." + string(make([]byte, 64))} {
		params["service"] = service
		if _, err := templateEndpoint(tpl, params); err == nil {
			t.Fatal("invalid service accepted", service)
		}
	}
	params["service"] = "a.b.c"
	for _, origin := range []string{"http://workspace.example", "https://user:secret@workspace.example", "https://workspace.example/path", "https://workspace.example?", "https://workspace.example#"} {
		params["workspace"] = origin
		if _, err := templateEndpoint(tpl, params); err == nil {
			t.Fatal("invalid origin accepted", origin)
		}
	}
}
