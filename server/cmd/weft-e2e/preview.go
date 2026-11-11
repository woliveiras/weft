//go:build e2e

package main

import (
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/woliveiras/weft/server/internal/app"
	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/llm"
)

func previewInference(r *llm.ChatRequest) (*llm.ChatResponse, error) {
	if strings.Contains(r.System, "Human review result:") && !strings.Contains(r.System, `"state":"changes_requested"`) {
		return &llm.ChatResponse{Message: llm.ChatMessage{Content: "Human review consumed; work complete"}, StopReason: "stop"}, nil
	}
	for _, m := range r.Messages {
		if m.Role == llm.RoleTool {
			return nil, fmt.Errorf("review fixture tool failed: %s", m.Content)
		}
		if strings.Contains(m.Content, "Human review result:") {
			return &llm.ChatResponse{Message: llm.ChatMessage{Content: "Human review consumed; work complete"}, StopReason: "stop"}, nil
		}
	}
	args, _ := json.Marshal(map[string]any{"title": "Review sandbox React and HTTP", "instructions": "Increment the counter, save via HTTP, verify the WebSocket and approve.", "command": []string{"./fixture-app", "preview-app"}, "port": 5173, "checks": []string{"Fixture built with React and Vite"}})
	return &llm.ChatResponse{Message: llm.ChatMessage{ToolCalls: []llm.ToolCall{{ID: "preview", Name: "review.request", Arguments: args}}}, StopReason: "tool_calls"}, nil
}
func seedPreview(ctx context.Context, a *app.App, cfg *config.Config) error {
	dir := filepath.Join(cfg.Workspaces.Root, "fixture-preview")
	if e := os.MkdirAll(dir, 0755); e != nil {
		return e
	}
	if e := os.CopyFS(dir, os.DirFS("preview-web")); e != nil {
		return e
	}
	exe, e := os.Executable()
	if e != nil {
		return e
	}
	b, e := os.ReadFile(exe)
	if e != nil {
		return e
	}
	if e = os.WriteFile(filepath.Join(dir, "fixture-app"), b, 0755); e != nil {
		return e
	}
	g := &domain.Graph{Metadata: domain.GraphMetadata{Name: "fixture-preview"}, Spec: domain.GraphSpec{Defaults: domain.NodeDefaults{Model: domain.ModelRef{Provider: "e2e", Name: "fixture"}}, Nodes: []domain.Node{{ID: "worker", Prompt: "fixture-preview", Runtime: domain.RuntimeRef{Kind: domain.RuntimeNative}, Loop: domain.LoopSpec{Mode: domain.LoopOnce}, Tools: []domain.ToolGrant{{Name: "review.request"}, {Name: "shell.exec", Commands: []string{"fixture-app"}}}, Isolation: domain.IsolationSpec{Mode: domain.IsolationDocker, Image: "alpine:3.22"}}}}}
	_, _, e = a.Engine.SaveGraph(ctx, g, 0, "fixture")
	return e
}
func previewApp() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/value", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer application-token" {
			http.Error(w, "application auth missing", 401)
			return
		}
		body, e := io.ReadAll(io.LimitReader(r.Body, 128))
		if e != nil {
			http.Error(w, "body", 400)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "application", Value: "tested", Path: "/"})
		fmt.Fprintf(w, "Saved: %s", body)
	})
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, b, e := w.(http.Hijacker).Hijack()
		if e != nil {
			return
		}
		defer conn.Close()
		hash := sha1.Sum([]byte(r.Header.Get("Sec-WebSocket-Key") + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
		fmt.Fprintf(b, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", base64.StdEncoding.EncodeToString(hash[:]))
		if b.Flush() != nil {
			return
		}
		for {
			var head [2]byte
			if _, e = io.ReadFull(b, head[:]); e != nil {
				return
			}
			n := int(head[1] & 127)
			if n > 125 || head[0]&15 == 8 {
				return
			}
			var mask [4]byte
			if head[1]&128 != 0 {
				if _, e = io.ReadFull(b, mask[:]); e != nil {
					return
				}
			}
			body := make([]byte, n)
			if _, e = io.ReadFull(b, body); e != nil {
				return
			}
			for i := range body {
				body[i] ^= mask[i%4]
			}
			if _, e = conn.Write(append([]byte{0x81, byte(n)}, body...)); e != nil {
				return
			}
		}
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/nested" {
			http.ServeFile(w, r, "index.html")
			return
		}
		http.FileServer(http.Dir(".")).ServeHTTP(w, r)
	})
	return http.ListenAndServe("127.0.0.1:5173", mux)
}
