package mcpconfig

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestProcessSpecValidation(t *testing.T) {
	base := processSpec{Command: filepath.Join(t.TempDir(), "mcp"), Directory: t.TempDir()}
	if err := base.validate(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"relative executable", "relative directory", "null argument", "environment name", "large config", "too many args"} {
		t.Run(name, func(t *testing.T) {
			s := base
			switch name {
			case "relative executable":
				s.Command = "mcp"
			case "relative directory":
				s.Directory = "."
			case "null argument":
				s.Args = []string{"bad\x00"}
			case "environment name":
				s.Environment = map[string]string{"BAD=NAME": "value"}
			case "large config":
				s.Args = []string{strings.Repeat("a", 65537)}
			case "too many args":
				s.Args = make([]string, 129)
			}
			if s.validate() == nil {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
}

// A real child executable exercises pipes, framing, process failures and the
// shared discovery validator without substituting an in-memory RPC transport.
func TestMCPProcessFixture(t *testing.T) {
	if len(os.Args) < 3 || os.Args[len(os.Args)-2] != "--mcp-fixture" {
		return
	}
	mode := os.Args[len(os.Args)-1]
	if mode == "exit" {
		os.Exit(3)
	}
	if mode == "hang" {
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	decoder := json.NewDecoder(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	for {
		var request struct {
			ID     int    `json:"id"`
			Method string `json:"method"`
		}
		if decoder.Decode(&request) != nil {
			os.Exit(0)
		}
		if request.ID == 0 {
			continue
		}
		if mode == "malformed" {
			fmt.Println("not JSON")
			continue
		}
		if mode == "oversized" {
			fmt.Println(strings.Repeat("x", maxBody+2))
			continue
		}
		if mode == "notifications" {
			for range 101 {
				_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "method": "notifications/message"})
			}
			continue
		}
		var result any
		switch request.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": protocol, "capabilities": map[string]any{"tools": map[string]any{}}}
		case "tools/list":
			name := "inspect"
			if mode == "invalid-tool" {
				name = "../bad"
			}
			result = map[string]any{"tools": []Tool{{Name: name, InputSchema: json.RawMessage(`{"type":"object"}`)}}}
		case "tools/call":
			if mode == "lost-call" {
				os.Exit(7)
			}
			value := "fixture result"
			if os.Getenv("WEFT_PARENT_ONLY") != "" {
				value = "inherited parent environment"
			}
			if mode == "echo-env" {
				value = os.Getenv("FIXTURE_SECRET")
			}
			result = map[string]any{"content": []any{map[string]any{"type": "text", "text": value}}}
		}
		id := request.ID
		if mode == "wrong-id" {
			id++
		}
		if encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result}) != nil {
			os.Exit(0)
		}
	}
}

func processFixture(t *testing.T, mode string) processSpec {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("native process lifecycle explicitly unsupported on Windows")
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return processSpec{Command: executable, Directory: t.TempDir(), Args: []string{"-test.run=^TestMCPProcessFixture$", "--", "--mcp-fixture", mode}}
}

func TestProcessDiscoveryAndCallIntegration(t *testing.T) {
	spec := processFixture(t, "success")
	t.Setenv("WEFT_PARENT_ONLY", "must not be inherited")
	for range 2 {
		catalog, err := processOperation(context.Background(), spec, nil)
		if err != nil || len(catalog) != 1 || catalog[0].Name != "inspect" {
			t.Fatalf("discovery: %v %v", catalog, err)
		}
		_, err = processOperation(context.Background(), spec, func(call rpcCall) error {
			result, err := call(2, "tools/call", map[string]any{"name": "inspect", "arguments": map[string]any{}})
			if err == nil && !strings.Contains(string(result), "fixture result") {
				t.Errorf("unexpected response: %s", result)
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestProcessFailureIntegration(t *testing.T) {
	for _, mode := range []string{"exit", "malformed", "oversized", "wrong-id", "invalid-tool", "notifications", "hang", "lost-call", "echo-env", "missing"} {
		t.Run(mode, func(t *testing.T) {
			spec := processFixture(t, mode)
			if mode == "missing" {
				spec.Command = filepath.Join(t.TempDir(), "missing")
			}
			spec.Environment = map[string]string{"FIXTURE_SECRET": "synthetic-private-fixture-value"}
			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancel()
			var action func(rpcCall) error
			if mode == "lost-call" || mode == "echo-env" {
				action = func(call rpcCall) error {
					_, err := call(2, "tools/call", map[string]any{"name": "inspect"})
					return err
				}
			}
			_, err := processOperation(ctx, spec, action)
			if err == nil {
				t.Fatal("expected failure")
			}
			if strings.Contains(err.Error(), "synthetic-private-fixture-value") {
				t.Fatal("credential exposed")
			}
		})
	}
}
