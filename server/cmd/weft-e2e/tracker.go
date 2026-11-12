//go:build e2e

package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/woliveiras/weft/server/internal/app"
	"github.com/woliveiras/weft/server/internal/mcpconfig"
	"github.com/woliveiras/weft/server/internal/trigger"

	"github.com/woliveiras/weft/server/internal/tools"
)

// trackerTool is a fixture GitHub MCP tool for issue trigger journeys on the
// connection "e2e-tracker". It never reaches a network.
type trackerTool struct{ name, content string }

func (t trackerTool) Name() string            { return "mcp.e2e-tracker." + t.name }
func (t trackerTool) Capability() string      { return t.Name() }
func (t trackerTool) Description() string     { return "E2E issue tracker fixture" }
func (t trackerTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (t trackerTool) Invoke(context.Context, *tools.Invocation) (*tools.Result, error) {
	return &tools.Result{Content: t.content}, nil
}

func trackerTools() []tools.Tool {
	return []tools.Tool{
		trackerTool{"search_issues", `{"items":[{"number":11,"title":"Add CSV export","state":"open","html_url":"https://github.example/acme/app/issues/11","labels":[{"name":"weft:todo"}],"assignees":[]},{"number":12,"title":"Waits on design","state":"open","labels":[{"name":"weft:todo"},{"name":"blocked"}],"assignees":[]}]}`},
	}
}

type issueFixtureTransport struct{}

func (issueFixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	const issue = `{"number":11,"title":"Add CSV export","body":"Add CSV export.","state":"open","labels":[{"name":"weft:todo"}],"user":{"login":"reporter","type":"User"}}`
	if r.URL.Host != "api.github.com" || (r.URL.Path != "/user" && !strings.HasPrefix(r.URL.Path, "/repos/acme/app/issues/11")) {
		return nil, errors.New("unexpected fixture tracker request")
	}
	body := "{}"
	switch {
	case r.URL.Path == "/user":
		body = `{"id":700}`
	case r.Method == "GET" && r.URL.Path == "/repos/acme/app/issues/11":
		body = issue
	case strings.HasSuffix(r.URL.Path, "/comments"):
		body = `{"id":1,"user":{"id":700}}`
	}
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
}

func prepareReadinessFixture(ctx context.Context, a *app.App) error {
	_, err := a.MCP.Put(ctx, mcpconfig.Input{ID: "e2e-tracker", Name: "Tracker fixture", Service: "github",
		Endpoint: "https://api.githubcopilot.com/mcp/", Enabled: true, Credential: &mcpconfig.Credential{Value: "e2e-tracker-placeholder-credential"}}, 0, "e2e-fixture")
	if err != nil {
		return err
	}
	a.TrackerAPI.Client = &http.Client{Transport: issueFixtureTransport{}}
	tr, err := a.Triggers.Triggers.Create(ctx, trigger.Input{Name: "Held readiness fixture", Workflow: "git-inspect", NodeID: "worker",
		Source: trigger.SourceConfig{Kind: "github", ConnectionID: "e2e-tracker", Repository: "acme/app"}}, "e2e-fixture")
	if err != nil {
		return err
	}
	a.Triggers.Tick(ctx)
	tr, err = a.Triggers.Triggers.Get(ctx, tr.ID)
	if err != nil {
		return err
	}
	if len(tr.Holds) != 1 || tr.Holds[0].Round != 1 || tr.Holds[0].Publication != "confirmed" {
		return errors.New("readiness fixture was not held with a confirmed question")
	}
	if _, err := a.Triggers.Triggers.Pause(ctx, tr.ID, "Fixture waiting for clarification.", "e2e-fixture"); err != nil {
		return err
	}
	pending, err := a.Triggers.Triggers.Create(ctx, trigger.Input{Name: "Unpublished readiness fixture", Workflow: "git-inspect", NodeID: "worker",
		Source: tr.Source}, "e2e-fixture")
	if err != nil {
		return err
	}
	if _, err := a.Triggers.Triggers.Pause(ctx, pending.ID, "Fixture waiting for comment publication.", "e2e-fixture"); err != nil {
		return err
	}
	_, err = a.Triggers.Triggers.Update(ctx, pending.ID, "e2e-fixture", func(t *trigger.Trigger) error {
		hold := tr.Holds[0]
		hold.Key, hold.URL, hold.Generation, hold.CommentID = "acme/app#13", "", "", ""
		hold.Publication, hold.SnapshotEdits = "label_applied", 2
		t.Holds = []trigger.Hold{hold}
		return nil
	})
	return err
}
