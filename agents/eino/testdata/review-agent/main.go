// review-agent exercises candidate review through Protocol v1 in the Docker acceptance test.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/cloudwego/eino/compose"
	"github.com/woliveiras/weft/agents/eino/protocol"
)

type turn struct {
	in *protocol.TurnInput
	c  *protocol.Client
}

func prepare(ctx context.Context, t turn) (turn, error) {
	r, err := t.c.Tool(ctx, "shell.exec", map[string]any{"argv": []string{"cat", "/etc/alpine-release"}})
	if err != nil {
		return t, err
	}
	if !strings.Contains(r.Content, "3.22.") {
		return t, fmt.Errorf("Alpine fixture unavailable")
	}
	if len(t.in.OperatorMessages) > 0 {
		return t, nil
	}
	if err = t.c.Emit(ctx, protocol.Emit{Type: "premature", Payload: map[string]string{}}); err != nil {
		return t, err
	}
	_, err = t.c.Tool(ctx, "review.request", map[string]string{"title": "Review candidate", "instructions": "Inspect candidate files"})
	return t, err
}

func finish(ctx context.Context, t turn) (protocol.TurnResult, error) {
	if len(t.in.OperatorMessages) != 1 {
		_, err := t.c.Tool(ctx, "filesystem.write", map[string]string{"path": "after-gate.txt", "content": "unexpected"})
		return protocol.TurnResult{}, fmt.Errorf("continued past review gate: %v", err)
	}
	var result map[string]string
	raw, ok := strings.CutPrefix(t.in.OperatorMessages[0], "Human review result: ")
	if !ok || json.Unmarshal([]byte(raw), &result) != nil || result["state"] != "approved" {
		return protocol.TurnResult{}, fmt.Errorf("invalid review decision")
	}
	result["execution"] = t.in.ExecutionID
	return protocol.TurnResult{Emits: []protocol.Emit{{Type: "reviewed", Payload: result}}}, nil
}

func run() error {
	chain := compose.NewChain[turn, protocol.TurnResult]()
	chain.AppendLambda(compose.InvokableLambda(prepare))
	chain.AppendLambda(compose.InvokableLambda(finish))
	graph, err := chain.Compile(context.Background())
	if err != nil {
		return err
	}
	action := func(ctx context.Context, in *protocol.TurnInput, c *protocol.Client) (protocol.TurnResult, error) {
		return graph.Invoke(ctx, turn{in, c})
	}
	h, err := protocol.NewHandler([]string{os.Getenv("WEFT_URL")}, os.Getenv("WEFT_RUNTIME_SECRET"), map[string]protocol.Agent{"candidate": action})
	if err != nil {
		return err
	}
	server := http.Server{Addr: net.JoinHostPort("127.0.0.1", os.Getenv("PORT")), Handler: h, ReadHeaderTimeout: 5 * time.Second}
	return server.ListenAndServe()
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
