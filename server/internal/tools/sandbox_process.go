package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"time"
)

const (
	DefaultExecTimeout = 2 * time.Minute
	MaxExecTimeout     = 30 * time.Minute
	DefaultMaxOutput   = 64 << 10
)

// ProcessSandbox runs commands as child processes of the platform, starting
// in (a directory below) Root, with a minimal environment, a hard timeout
// that kills the whole process group, and capped output.
//
// It confines where a command starts, what environment it sees and how long
// it lives - not what it can do: a child process has the same OS rights as
// the platform user. Use DockerSandbox when that is not acceptable.
type ProcessSandbox struct {
	Root string
	// Path overrides PATH (default: the platform's PATH, which is not secret).
	Path string
}

func (s *ProcessSandbox) Kind() string { return "process" }
func (s *ProcessSandbox) Close() error { return nil }

func (s *ProcessSandbox) Exec(ctx context.Context, req ExecRequest) (*ExecResult, error) {
	dir, _, err := ResolvePath(s.Root, req.Dir)
	if err != nil {
		return nil, err
	}
	argv := req.Argv
	if req.Shell != "" {
		if runtime.GOOS == "windows" {
			argv = []string{"cmd", "/C", req.Shell}
		} else {
			argv = []string{"/bin/sh", "-c", req.Shell}
		}
	}
	if len(argv) == 0 {
		return nil, errors.New("exec: empty command")
	}
	timeout := clampTimeout(req.Timeout)
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.Env = s.env(req.Env)
	if req.Stdin != "" {
		cmd.Stdin = bytes.NewBufferString(req.Stdin)
	}
	limit := req.MaxOutput
	if limit <= 0 {
		limit = DefaultMaxOutput
	}
	stdout, stderr := &cappedBuffer{limit: limit, tail: req.TailOutput}, &cappedBuffer{limit: limit, tail: req.TailOutput}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	setProcessGroup(cmd)
	cmd.Cancel = func() error { return killProcessGroup(cmd) }
	// Grandchildren holding the pipes open must not hang Wait forever.
	cmd.WaitDelay = 5 * time.Second

	start := time.Now()
	runErr := cmd.Run()
	res := &ExecResult{
		Stdout:    stdout.String(),
		Stderr:    stderr.String(),
		Duration:  time.Since(start),
		Truncated: stdout.truncated || stderr.truncated,
		TimedOut:  errors.Is(runCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil,
	}
	if cmd.ProcessState != nil {
		res.ExitCode = cmd.ProcessState.ExitCode()
	}
	if runErr != nil {
		var ee *exec.ExitError
		switch {
		case errors.As(runErr, &ee), res.TimedOut:
			if res.ExitCode == 0 {
				res.ExitCode = -1
			}
		case ctx.Err() != nil:
			return res, ctx.Err()
		case cmd.ProcessState == nil:
			return nil, fmt.Errorf("exec %s: %w", argv[0], runErr)
		}
	}
	return res, nil
}

// env builds the scrubbed environment. The server environment is never
// inherited: provider API keys and platform credentials live there.
func (s *ProcessSandbox) env(extra map[string]string) []string {
	path := s.Path
	if path == "" {
		path = os.Getenv("PATH")
	}
	if path == "" {
		path = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
	}
	m := map[string]string{
		"PATH":                path,
		"HOME":                s.Root,
		"LANG":                "C.UTF-8",
		"LC_ALL":              "C.UTF-8",
		"TMPDIR":              os.TempDir(),
		"GIT_TERMINAL_PROMPT": "0",
		"NO_COLOR":            "1",
	}
	if runtime.GOOS == "windows" {
		for _, k := range []string{"SystemRoot", "ComSpec", "PATHEXT", "TEMP", "TMP"} {
			if v := os.Getenv(k); v != "" {
				m[k] = v
			}
		}
		m["USERPROFILE"] = s.Root
	}
	for k, v := range extra {
		m[k] = v
	}
	out := make([]string, 0, len(m))
	for k, v := range m {
		out = append(out, k+"="+v)
	}
	sort.Strings(out)
	return out
}

func clampTimeout(d time.Duration) time.Duration {
	if d <= 0 {
		return DefaultExecTimeout
	}
	if d > MaxExecTimeout {
		return MaxExecTimeout
	}
	return d
}

// cappedBuffer keeps the first limit bytes and remembers that it dropped
// more. With tail > 0 it also keeps the last tail bytes of what overflowed.
type cappedBuffer struct {
	buf       bytes.Buffer
	limit     int
	truncated bool
	tail      int
	tailBuf   []byte
	dropped   int
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if room := c.limit - c.buf.Len(); room > 0 {
		k := min(room, len(p))
		c.buf.Write(p[:k])
		p = p[k:]
	}
	if len(p) == 0 {
		return n, nil
	}
	c.truncated = true
	if c.tail <= 0 {
		return n, nil
	}
	c.tailBuf = append(c.tailBuf, p...)
	if over := len(c.tailBuf) - c.tail; over > c.tail {
		c.dropped += over
		c.tailBuf = append([]byte(nil), c.tailBuf[over:]...)
	}
	return n, nil
}

func (c *cappedBuffer) String() string {
	if len(c.tailBuf) == 0 {
		return c.buf.String()
	}
	keep, omitted := c.tailBuf, c.dropped
	if over := len(keep) - c.tail; over > 0 {
		keep, omitted = keep[over:], omitted+over
	}
	for len(keep) > 0 && keep[0]&0xC0 == 0x80 { // start on a rune boundary
		keep, omitted = keep[1:], omitted+1
	}
	return fmt.Sprintf("%s\n…[%d bytes omitted]…\n%s", c.buf.String(), omitted, keep)
}
