package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const capShell = "shell.exec"

func shellTools() []Tool {
	return []Tool{&funcTool{
		name: "shell.exec", capability: capShell,
		description: "Run a command in the workspace and return its exit code, stdout and stderr. " +
			"Pass either 'command' (a shell command line) or 'argv' (program and arguments, no shell). " +
			"Commands run non-interactively with a timeout; do not start servers or watchers that never exit.",
		schema: schema(`{"type":"object","properties":{` +
			`"command":{"type":"string","description":"Shell command line, e.g. 'go test ./...'"},` +
			`"argv":{"type":"array","items":{"type":"string"},"description":"Program and arguments, e.g. [\"go\",\"test\",\"./...\"]"},` +
			`"cwd":{"type":"string","description":"Working directory relative to the workspace"},` +
			`"timeoutSec":{"type":"integer","minimum":1,"maximum":1800},` +
			`"env":{"type":"object","additionalProperties":{"type":"string"}}},"additionalProperties":false}`),
		fn: shellExec,
	}}
}

// shellMeta are characters that let one command line run several programs
// or redirect output. A command-restricted grant refuses them, because the
// allowlist can only vouch for the program it can see.
const shellMeta = ";|&`$<>\n\r(){}"

func shellExec(ctx context.Context, inv *Invocation) (*Result, error) {
	var a struct {
		Command    string            `json:"command"`
		Argv       []string          `json:"argv"`
		Cwd        string            `json:"cwd"`
		TimeoutSec int               `json:"timeoutSec"`
		Env        map[string]string `json:"env"`
	}
	if err := decodeArgs(inv, &a); err != nil {
		return nil, err
	}
	if (a.Command == "") == (len(a.Argv) == 0) {
		return nil, errors.New("pass exactly one of 'command' or 'argv'")
	}
	req, err := shellRequest(inv, a.Command, a.Argv, a.Cwd)
	if err != nil {
		return nil, err
	}
	req.Env, req.Timeout = a.Env, time.Duration(a.TimeoutSec)*time.Second
	res, err := sandboxOf(inv).Exec(ctx, req)
	if err != nil {
		return nil, err
	}
	return execResult(res), nil
}

// shellRequest turns a command line or argv into an ExecRequest under the
// caller's shell.exec grant: a command-restricted grant runs the checked
// argv without a shell and refuses shell operators. check.run reuses it so
// a declared check is exactly the shell call it names, never more.
func shellRequest(inv *Invocation, command string, argv []string, cwd string) (ExecRequest, error) {
	req := ExecRequest{Dir: cwd}
	switch {
	case len(argv) > 0:
		if err := CheckCommand(inv.Grant, argv); err != nil {
			return req, err
		}
		req.Argv = argv
	case len(inv.Grant.Commands) > 0:
		if strings.ContainsAny(command, shellMeta) {
			return req, fmt.Errorf("%w: this agent may only run single commands from %v; shell operators (; | & $ ` < > parentheses, newlines) are not allowed", ErrDenied, inv.Grant.Commands)
		}
		split, err := SplitCommand(command)
		if err != nil {
			return req, err
		}
		if err := CheckCommand(inv.Grant, split); err != nil {
			return req, err
		}
		req.Argv = split // run without a shell: what was checked is what runs
	default:
		req.Shell = command
	}
	if req.Dir != "" {
		_, rel, err := ResolvePath(workdirOf(inv), req.Dir)
		if err != nil {
			return req, err
		}
		req.Dir = rel
	}
	return req, nil
}

func execResult(res *ExecResult) *Result {
	var b strings.Builder
	fmt.Fprintf(&b, "exit code: %d", res.ExitCode)
	if res.TimedOut {
		b.WriteString(" (timed out, killed)")
	}
	b.WriteString("\n")
	if res.Stdout != "" {
		b.WriteString("--- stdout ---\n" + res.Stdout)
		if !strings.HasSuffix(res.Stdout, "\n") {
			b.WriteString("\n")
		}
	}
	if res.Stderr != "" {
		b.WriteString("--- stderr ---\n" + res.Stderr)
	}
	data, _ := json.Marshal(res)
	return &Result{Content: b.String(), Data: data, Truncated: res.Truncated}
}

// SplitCommand splits a simple command line into argv, honouring single
// and double quotes and backslash escapes (POSIX-like, no expansion).
func SplitCommand(s string) ([]string, error) {
	var (
		args    []string
		cur     strings.Builder
		inArg   bool
		quote   rune
		escaped bool
	)
	for _, r := range s {
		switch {
		case escaped:
			cur.WriteRune(r)
			escaped = false
		case r == '\\' && quote != '\'':
			escaped, inArg = true, true
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote, inArg = r, true
		case r == ' ' || r == '\t':
			if inArg {
				args = append(args, cur.String())
				cur.Reset()
				inArg = false
			}
		default:
			cur.WriteRune(r)
			inArg = true
		}
	}
	if quote != 0 || escaped {
		return nil, errors.New("unterminated quote or escape in command")
	}
	if inArg {
		args = append(args, cur.String())
	}
	if len(args) == 0 {
		return nil, errors.New("empty command")
	}
	return args, nil
}
