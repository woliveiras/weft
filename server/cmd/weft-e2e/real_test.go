//go:build e2e

package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/llm"
)

func TestRealProviderUsesShellAndPropagatesFailures(t *testing.T) {
	p := realProvider{}
	for _, id := range []string{"fixture-real-a", "fixture-real-b", "fixture-source-weft", "fixture-source-devcontainer", "fixture-source-dockerfile", "fixture-source-compose", "fixture-mise-a", "fixture-mise-b"} {
		response, err := p.Chat(context.Background(), &llm.ChatRequest{Messages: []llm.ChatMessage{{Role: llm.RoleUser, Content: id}}})
		if err != nil {
			t.Fatal(err)
		}
		if len(response.Message.ToolCalls) != 1 || response.Message.ToolCalls[0].Name != "shell.exec" {
			t.Fatalf("missing real tool call: %+v", response)
		}
		var args struct {
			Argv []string `json:"argv"`
		}
		if err := json.Unmarshal(response.Message.ToolCalls[0].Arguments, &args); err != nil {
			t.Fatal(err)
		}
		if len(args.Argv) != 3 || args.Argv[0] != "sh" || args.Argv[1] != "-ec" || !strings.Contains(args.Argv[2], id) || !strings.Contains(args.Argv[2], "test ! -e result.txt") {
			t.Fatalf("invalid shell proof: %+v", args)
		}
		if strings.Contains(args.Argv[2], "pnpm test") != strings.HasPrefix(id, "fixture-mise-") {
			t.Fatalf("unexpected toolchain command: %s", args.Argv[2])
		}
		if strings.Contains(args.Argv[2], "package-lock.json") != (id == "fixture-source-weft") {
			t.Fatalf("unexpected manifest mutation for %s: %s", id, args.Argv[2])
		}
	}
	for _, result := range []string{"exit code: 1\nfailed", "denied", ""} {
		if _, err := p.Chat(context.Background(), &llm.ChatRequest{Messages: []llm.ChatMessage{{Role: llm.RoleTool, Content: result}}}); err == nil {
			t.Fatalf("reported success after %q", result)
		}
	}
	if _, err := p.Chat(context.Background(), &llm.ChatRequest{Messages: []llm.ChatMessage{{Role: llm.RoleUser, Content: "untrusted fixture-real-a suffix"}}}); err == nil {
		t.Fatal("unknown fixture accepted")
	}
	result := "exit code: 0\nuid=10001"
	response, err := p.Chat(context.Background(), &llm.ChatRequest{Messages: []llm.ChatMessage{{Role: llm.RoleTool, Content: result}}})
	if err != nil || !strings.Contains(response.Message.Content, result) {
		t.Fatalf("lost real output: %+v %v", response, err)
	}
	response, err = p.Chat(context.Background(), &llm.ChatRequest{Metadata: map[string]string{"purpose": "environment.proposal"}})
	if err != nil || !strings.Contains(response.Message.Content, "alpine:3.22") {
		t.Fatalf("analysis: %+v %v", response, err)
	}
}

func TestMiseProposalUsesPrivateToolsAndApprovedSetupNetwork(t *testing.T) {
	r, err := (realProvider{}).Chat(context.Background(), &llm.ChatRequest{Metadata: map[string]string{"purpose": "environment.proposal"}, Messages: []llm.ChatMessage{{Role: llm.RoleUser, Content: "weft-mise-example"}}})
	if err != nil {
		t.Fatal(err)
	}
	var proposal struct {
		Environment domain.EnvironmentSpec `json:"environment"`
	}
	if err := json.Unmarshal([]byte(r.Message.Content), &proposal); err != nil {
		t.Fatal(err)
	}
	e := proposal.Environment
	if len(e.Setup) != 3 || e.Setup[2] != "mise exec -- pnpm install --frozen-lockfile --ignore-scripts --store-dir /home/sandbox/.local/share/pnpm/store" {
		t.Fatalf("expected frozen pnpm installation: %v", e.Setup)
	}

	if !strings.Contains(e.Dockerfile, "@sha256:") || !strings.Contains(e.Dockerfile, "MISE_DATA_DIR=/home/sandbox/") || e.Network.Setup != "bridge" || e.Network.Run != "none" || len(e.Setup) != 3 || len(e.Caches) != 1 || e.Caches[0] != "/home/sandbox/.local/share/pnpm/store" {
		t.Fatalf("unsafe or incomplete fixture: %+v", e)
	}
}
