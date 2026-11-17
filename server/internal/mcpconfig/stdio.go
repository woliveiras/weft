package mcpconfig

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// processSpec is deliberately internal: registering host commands requires a
// separate native-only, administrator-reviewed API boundary.
type processSpec struct {
	Command     string
	Args        []string
	Directory   string
	Environment map[string]string
}

var environmentName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func (s processSpec) validate() error {
	if !filepath.IsAbs(s.Command) || len(s.Command) > 4096 || strings.ContainsRune(s.Command, 0) || !filepath.IsAbs(s.Directory) || len(s.Directory) > 4096 || strings.ContainsRune(s.Directory, 0) {
		return discoveryError("MCP process requires absolute executable and working directory paths")
	}
	if len(s.Args) > 128 || len(s.Environment) > 128 {
		return discoveryError("MCP process configuration exceeds limits")
	}
	size := 0
	for _, value := range s.Args {
		size += len(value)
		if strings.ContainsRune(value, 0) {
			return discoveryError("invalid MCP process argument")
		}
	}
	for name, value := range s.Environment {
		size += len(name) + len(value)
		if !environmentName.MatchString(name) || strings.ContainsRune(value, 0) {
			return discoveryError("invalid MCP process environment")
		}
	}
	if size > 65536 {
		return discoveryError("MCP process configuration exceeds limits")
	}
	return nil
}

// processOperation starts one private session per operation. It never retries a
// dispatched call, inherits no environment, and never forwards child stderr.
func processOperation(ctx context.Context, spec processSpec, action func(rpcCall) error) ([]Tool, error) {
	if err := spec.validate(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, spec.Command, spec.Args...)
	cmd.Dir = spec.Directory
	cmd.Env = []string{}
	for name, value := range spec.Environment {
		cmd.Env = append(cmd.Env, name+"="+value)
	}
	cmd.Stderr = io.Discard
	if err := prepareMCPProcess(cmd); err != nil {
		return nil, err
	}
	cmd.WaitDelay = time.Second
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, discoveryError("MCP process input unavailable")
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		_ = in.Close()
		return nil, discoveryError("MCP process output unavailable")
	}
	if err = cmd.Start(); err != nil {
		_ = in.Close()
		_ = out.Close()
		return nil, discoveryError("MCP process could not start")
	}
	defer func() {
		_ = in.Close()
		_ = cmd.Cancel()
		_ = cmd.Wait() // forced teardown is expected; operation errors are reported below
	}()
	scanner := bufio.NewScanner(out)
	scanner.Buffer(make([]byte, 4096), maxBody+1)
	call := func(id int, method string, params any) (json.RawMessage, error) {
		message := map[string]any{"jsonrpc": "2.0", "method": method, "params": params}
		if id > 0 {
			message["id"] = id
		}
		if err := json.NewEncoder(in).Encode(message); err != nil {
			return nil, discoveryError("MCP process request failed; outcome unknown; no automatic retry")
		}
		if id == 0 {
			return nil, nil
		}
		// Bound unsolicited notifications as well as individual frames.
		for count := 0; count < 100; count++ {
			if !scanner.Scan() {
				return nil, discoveryError("MCP process response unavailable or oversized; no automatic retry")
			}
			raw := scanner.Bytes()
			if len(raw) > maxBody {
				return nil, discoveryError("MCP process response too large")
			}
			var envelope struct {
				JSONRPC string          `json:"jsonrpc"`
				ID      *int            `json:"id"`
				Method  string          `json:"method"`
				Result  json.RawMessage `json:"result"`
				Error   json.RawMessage `json:"error"`
			}
			if !utf8.Valid(raw) || json.Unmarshal(raw, &envelope) != nil || envelope.JSONRPC != "2.0" {
				return nil, discoveryError("invalid MCP process response")
			}
			if envelope.Method != "" {
				if envelope.ID != nil {
					return nil, discoveryError("MCP server-initiated requests are unsupported")
				}
				continue
			}
			if envelope.ID == nil || *envelope.ID != id || len(envelope.Error) > 0 || len(envelope.Result) == 0 {
				return nil, discoveryError("invalid or failed MCP process response")
			}
			for _, secret := range spec.Environment {
				if containsCredential(envelope.Result, secret) {
					return nil, discoveryError("MCP process response contains environment material")
				}
			}
			return envelope.Result, nil
		}
		return nil, discoveryError("MCP process notification limit exceeded")
	}
	return protocolOperation(ctx, call, false, "", action)
}
