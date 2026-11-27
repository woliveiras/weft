// weft-sandbox-exec runs inside the unprivileged workload container. Its
// installation-owned root and expected pod UID are independent of agent input.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/woliveiras/weft/server/internal/tools"
	"io"
	"os"
	"os/signal"
	"syscall"
)

func run(ctx context.Context, root, actual, expected string, in io.Reader, out io.Writer) error {
	if actual == "" || actual != expected {
		return errors.New("sandbox pod identity changed")
	}
	const limit = 2 << 20
	raw, err := io.ReadAll(io.LimitReader(in, limit+1))
	if err != nil || len(raw) > limit {
		return errors.New("invalid sandbox request size")
	}
	var req tools.ExecRequest
	if json.Unmarshal(raw, &req) != nil || (len(req.Argv) == 0) == (req.Shell == "") || req.MaxOutput < 0 || req.MaxOutput > 8<<20 || req.TailOutput < 0 || req.TailOutput > 1<<20 {
		return errors.New("invalid sandbox command")
	}
	if req.Env == nil {
		req.Env = map[string]string{}
	}
	for k, v := range map[string]string{"HOME": "/tmp", "GOCACHE": "/tmp/go-cache", "GO111MODULE": "auto", "GOTOOLCHAIN": "local"} {
		if _, set := req.Env[k]; !set {
			req.Env[k] = v
		}
	}
	result, err := (&tools.ProcessSandbox{Root: root}).Exec(ctx, req)
	if err != nil {
		return fmt.Errorf("execute sandbox command: %w", err)
	}
	return json.NewEncoder(out).Encode(result)
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "expected sandbox pod UID")
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, "/workspace", os.Getenv("WEFT_POD_UID"), os.Args[1], os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
