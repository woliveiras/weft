//go:build e2e

// provider-fixture is the loopback-only runtime for the provider browser journey.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/compose"
	"github.com/woliveiras/weft/agents/eino/protocol"
)

type turn struct {
	input  *protocol.TurnInput
	client *protocol.Client
}

func writer(ctx context.Context) (protocol.Agent, error) {
	chain := compose.NewChain[turn, string]()
	chain.AppendLambda(compose.InvokableLambda(func(ctx context.Context, t turn) (string, error) {
		reply, err := t.client.Chat(ctx, protocol.ChatRequest{System: t.input.Prompt + "\n", Messages: []protocol.ChatMessage{{Role: "user", Content: "provider journey"}}})
		if err != nil {
			return "", err
		}
		return reply.Message.Content, nil
	}), compose.WithNodeName("infer"))
	graph, err := chain.Compile(ctx)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, in *protocol.TurnInput, c *protocol.Client) (protocol.TurnResult, error) {
		summary, err := graph.Invoke(ctx, turn{in, c})
		return protocol.TurnResult{Summary: summary}, err
	}, nil
}

func serve(dir string) error {
	agent, err := writer(context.Background())
	if err != nil {
		return err
	}
	handler, err := protocol.NewHandler([]string{os.Getenv("WEFT_URL")}, "", map[string]protocol.Agent{"writer": agent})
	if err != nil {
		return err
	}
	audit, err := os.OpenFile(filepath.Join(dir, "turns.jsonl"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer audit.Close()
	var mu sync.Mutex
	wrapped := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/turn" {
			handler.ServeHTTP(w, r)
			return
		}
		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
		if err != nil {
			http.Error(w, "invalid turn", 400)
			return
		}
		var in map[string]json.RawMessage
		if json.Unmarshal(raw, &in) != nil {
			http.Error(w, "invalid turn", 400)
			return
		}
		var cb map[string]json.RawMessage
		if json.Unmarshal(in["callback"], &cb) != nil || cb == nil {
			http.Error(w, "invalid callback", 400)
			return
		}
		delete(cb, "token")
		in["callback"], err = json.Marshal(cb)
		if err != nil {
			http.Error(w, "invalid callback", 400)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(raw))
		record := httptest.NewRecorder()
		handler.ServeHTTP(record, r)
		mu.Lock()
		err = json.NewEncoder(audit).Encode(map[string]any{"input": in, "status": record.Code, "result": json.RawMessage(record.Body.Bytes())})
		mu.Unlock()
		if err != nil {
			http.Error(w, "fixture audit failed", 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(record.Code)
		_, _ = w.Write(record.Body.Bytes())
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer ln.Close()
	if err := os.WriteFile(filepath.Join(dir, "runtime-url"), []byte("http://"+ln.Addr().String()), 0600); err != nil {
		return err
	}
	return (&http.Server{Handler: wrapped, ReadHeaderTimeout: 5 * time.Second}).Serve(ln)
}

func main() {
	if len(os.Args) != 2 || strings.TrimSpace(os.Args[1]) == "" {
		fmt.Fprintln(os.Stderr, "fixture directory required")
		os.Exit(2)
	}
	if err := serve(os.Args[1]); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
