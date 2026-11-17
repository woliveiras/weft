package mcpconfig

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/tools"
	"os"
	"testing"
	"time"
)

// Read-only, opt-in qualification of public anonymous catalogues. No stored
// connection, credentials, registration or tools/call are used by this test.
func TestLivePublicTemplateDiscovery(t *testing.T) {
	if os.Getenv("WEFT_TEST_PUBLIC_MCP") != "1" {
		t.Skip("set WEFT_TEST_PUBLIC_MCP=1 for public network discovery")
	}
	for _, id := range []string{"deepwiki", "hugging-face", "context7"} {
		t.Run(id, func(t *testing.T) {
			var selected *Template
			for _, template := range Templates() {
				if template.ID == id {
					copy := template
					selected = &copy
					break
				}
			}
			if selected == nil {
				t.Fatal("missing public template")
			}
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			catalogue, err := Discover(ctx, selected.Endpoint, "")
			if err != nil {
				t.Fatal(err)
			}
			if len(catalogue) == 0 {
				t.Fatal("empty public catalogue")
			}
			t.Logf("anonymous protocol discovery returned %d tools; no tool execution or account access tested", len(catalogue))
		})
	}
}

func TestLiveDeepWikiReadThroughGateway(t *testing.T) {
	if os.Getenv("WEFT_TEST_PUBLIC_MCP") != "1" {
		t.Skip("set WEFT_TEST_PUBLIC_MCP=1 for public network read")
	}
	manager := testManager(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	in := Input{ID: "public-docs", Name: "Public docs", Service: "generic", Endpoint: "https://mcp.deepwiki.com/mcp", Enabled: true, Template: &TemplateRef{ID: "deepwiki", Version: 1}}
	if _, err := manager.Put(ctx, in, 0, "test"); err != nil {
		t.Fatal(err)
	}
	view, err := manager.Refresh(ctx, in.ID, "test")
	if err != nil || view.Status != "ready" {
		t.Fatal("discovery unavailable", err)
	}
	gateway := tools.NewGateway(tools.NewRegistry(), nil, nil)
	gateway.ResolveTool = manager.Resolve
	principal := &tools.Principal{GraphID: "live-read", NodeID: "reader"}
	name := "mcp.public-docs.read_wiki_structure"
	args := json.RawMessage(`{"repoName":"golang/go"}`)
	if _, err := gateway.Invoke(ctx, principal, name, args); !errors.Is(err, tools.ErrDenied) {
		t.Fatal("missing grant not denied", err)
	}
	principal.Grants = []domain.ToolGrant{{Name: name}}
	result, err := gateway.Invoke(ctx, principal, name, args)
	if err != nil || result == nil || result.IsError || result.Content == "" {
		t.Fatal("public read did not succeed", err)
	}
	t.Log("live anonymous public documentation read succeeded through exact gateway grant")
}

// Public catalogue only: no Google identity, query, job or billing operation.
func TestLiveBigQueryStatelessDiscovery(t *testing.T) {
	if os.Getenv("WEFT_TEST_PUBLIC_MCP") != "1" {
		t.Skip("set WEFT_TEST_PUBLIC_MCP=1 for public network discovery")
	}
	ctx := credentialContext(t.Context(), View{TransportProfile: statelessProfile})
	catalogue, err := Discover(ctx, "https://bigquery.googleapis.com/mcp", "")
	if err != nil || len(catalogue) == 0 {
		t.Fatal("public stateless discovery failed", err)
	}
	t.Logf("stateless public discovery returned %d tools; no query or account access tested", len(catalogue))
}

// Anonymous documentation lookup only; no API key, account or paid job.
func TestLiveContext7ReadThroughGateway(t *testing.T) {
	if os.Getenv("WEFT_TEST_PUBLIC_MCP") != "1" {
		t.Skip("set WEFT_TEST_PUBLIC_MCP=1 for public network read")
	}
	manager := testManager(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	in := Input{ID: "public-context", Name: "Public context", Service: "generic", Endpoint: "https://mcp.context7.com/mcp", Enabled: true, Template: &TemplateRef{ID: "context7", Version: 1}}
	if _, err := manager.Put(ctx, in, 0, "test"); err != nil {
		t.Fatal(err)
	}
	view, err := manager.Refresh(ctx, in.ID, "test")
	if err != nil || view.Status != "ready" {
		t.Fatal("discovery unavailable", err)
	}
	gateway := tools.NewGateway(tools.NewRegistry(), nil, nil)
	gateway.ResolveTool = manager.Resolve
	principal := &tools.Principal{GraphID: "live-context", NodeID: "reader"}
	name := "mcp.public-context.resolve-library-id"
	args := json.RawMessage(`{"libraryName":"React","query":"React useState hook documentation"}`)
	if _, err := gateway.Invoke(ctx, principal, name, args); !errors.Is(err, tools.ErrDenied) {
		t.Fatal("missing grant not denied", err)
	}
	principal.Grants = []domain.ToolGrant{{Name: name}}
	result, err := gateway.Invoke(ctx, principal, name, args)
	if err != nil || result == nil || result.IsError || result.Content == "" {
		t.Fatal("public library lookup did not succeed", err)
	}
	t.Log("live anonymous library lookup succeeded through exact gateway grant; response omitted")
}
