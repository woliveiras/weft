package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
)

func call(t *testing.T, gw *Gateway, p *Principal, name string, args any) (*Result, error) {
	t.Helper()
	b, _ := json.Marshal(args)
	return gw.Invoke(context.Background(), p, name, b)
}

func run(t *testing.T, gw *Gateway, p *Principal, name string, args any) *Result {
	t.Helper()
	res, err := call(t, gw, p, name, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("tool error: %s", res.Content)
	}
	return res
}

func TestFilesystemTools(t *testing.T) {
	dir := t.TempDir()
	gw := NewGateway(Builtin(BuiltinConfig{}), nil, nil)
	p := principal(dir, domain.ToolGrant{Name: "filesystem.*"})

	run(t, gw, p, "filesystem.write", map[string]any{"path": "src/main.go", "content": "package main\n\nfunc main() {}\n"})
	run(t, gw, p, "filesystem.write", map[string]any{"path": "notes.md", "content": "a\n"})
	run(t, gw, p, "filesystem.write", map[string]any{"path": "notes.md", "content": "b\n", "append": true})
	if r := run(t, gw, p, "filesystem.read", map[string]any{"path": "notes.md"}); r.Content != "a\nb\n" {
		t.Fatalf("append: %q", r.Content)
	}
	r := run(t, gw, p, "filesystem.read", map[string]any{"path": "src/main.go", "offset": 3, "limit": 1})
	if !strings.Contains(r.Content, "func main") || strings.Contains(r.Content, "package") {
		t.Fatalf("paging: %q", r.Content)
	}
	run(t, gw, p, "filesystem.edit", map[string]any{"path": "src/main.go", "old": "func main() {}", "new": "func main() { println(1) }"})
	if _, err := os.Stat(filepath.Join(dir, "src/main.go")); err != nil {
		t.Fatal(err)
	}
	if r, _ := call(t, gw, p, "filesystem.edit", map[string]any{"path": "src/main.go", "old": "nope", "new": "x"}); !r.IsError {
		t.Fatal("edit of missing text must fail")
	}
	r = run(t, gw, p, "filesystem.list", map[string]any{"recursive": true})
	if !strings.Contains(r.Content, "src/\n") || !strings.Contains(r.Content, "src/main.go") {
		t.Fatalf("list: %q", r.Content)
	}
	r = run(t, gw, p, "filesystem.list", map[string]any{"recursive": true, "glob": "*.md"})
	if strings.Contains(r.Content, "main.go") || !strings.Contains(r.Content, "notes.md") {
		t.Fatalf("glob list: %q", r.Content)
	}
	r = run(t, gw, p, "filesystem.search", map[string]any{"pattern": `println\(\d\)`})
	if !strings.Contains(r.Content, "src/main.go:3:") {
		t.Fatalf("search: %q", r.Content)
	}
	run(t, gw, p, "filesystem.move", map[string]any{"from": "notes.md", "to": "done/notes.md"})
	r = run(t, gw, p, "filesystem.stat", map[string]any{"path": "notes.md"})
	if !strings.Contains(r.Content, "does not exist") {
		t.Fatalf("stat: %q", r.Content)
	}
	run(t, gw, p, "filesystem.mkdir", map[string]any{"path": "empty/dir"})
	if r, _ := call(t, gw, p, "filesystem.delete", map[string]any{"path": "src"}); !r.IsError {
		t.Fatal("non-recursive delete of non-empty dir must fail")
	}
	run(t, gw, p, "filesystem.delete", map[string]any{"path": "src", "recursive": true})
	if r, _ := call(t, gw, p, "filesystem.delete", map[string]any{"path": "."}); !r.IsError {
		t.Fatal("deleting the root must fail")
	}
}

func TestFilesystemPathConstraints(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "src", "pkg"), 0o755)
	os.MkdirAll(filepath.Join(dir, "secrets"), 0o755)
	os.WriteFile(filepath.Join(dir, "src", "pkg", "a.go"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, "secrets", "key"), []byte("k"), 0o644)
	gw := NewGateway(Builtin(BuiltinConfig{}), nil, nil)
	p := principal(dir,
		domain.ToolGrant{Name: "filesystem.read", Paths: []string{"src/**"}},
		domain.ToolGrant{Name: "filesystem.write", Paths: []string{"src/**"}})

	if _, err := call(t, gw, p, "filesystem.read", map[string]any{"path": "secrets/key"}); !errors.Is(err, ErrDenied) {
		t.Fatalf("want denied, got %v", err)
	}
	if _, err := call(t, gw, p, "filesystem.write", map[string]any{"path": "README.md", "content": "x"}); !errors.Is(err, ErrDenied) {
		t.Fatalf("want denied, got %v", err)
	}
	if _, err := call(t, gw, p, "filesystem.move", map[string]any{"from": "src/pkg/a.go", "to": "secrets/a.go"}); !errors.Is(err, ErrDenied) {
		t.Fatalf("move target must be checked, got %v", err)
	}
	r := run(t, gw, p, "filesystem.list", map[string]any{"recursive": true})
	if strings.Contains(r.Content, "secrets") || !strings.Contains(r.Content, "src/pkg/a.go") {
		t.Fatalf("listing must hide ungranted paths: %q", r.Content)
	}
}

func TestShellExec(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix shell")
	}
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "sub"), 0o755)
	gw := NewGateway(Builtin(BuiltinConfig{}), nil, nil)
	p := principal(dir, domain.ToolGrant{Name: "shell.exec"})

	t.Setenv("WEFT_TEST_SECRET", "s3cr3t")
	r := run(t, gw, p, "shell.exec", map[string]any{"command": "pwd; echo $WEFT_TEST_SECRET; echo err >&2; exit 3", "cwd": "sub"})
	if !strings.Contains(r.Content, "exit code: 3") || !strings.Contains(r.Content, "sub") || !strings.Contains(r.Content, "err") {
		t.Fatalf("exec: %q", r.Content)
	}
	if strings.Contains(r.Content, "s3cr3t") {
		t.Fatal("server environment leaked into the sandbox")
	}
	start := time.Now()
	r = run(t, gw, p, "shell.exec", map[string]any{"command": "sleep 30 & sleep 30; echo never", "timeoutSec": 1})
	if !strings.Contains(r.Content, "timed out") || time.Since(start) > 15*time.Second {
		t.Fatalf("timeout: %q after %s", r.Content, time.Since(start))
	}
	if _, err := call(t, gw, p, "shell.exec", map[string]any{"command": "ls", "cwd": "../"}); !errors.Is(err, ErrDenied) {
		t.Fatalf("cwd escape must be denied, got %v", err)
	}
}

func TestShellExecAllowlist(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix")
	}
	gw := NewGateway(Builtin(BuiltinConfig{}), nil, nil)
	p := principal(t.TempDir(), domain.ToolGrant{Name: "shell.exec", Commands: []string{"echo", "ls *"}})
	r := run(t, gw, p, "shell.exec", map[string]any{"command": "echo 'hello world'"})
	if !strings.Contains(r.Content, "hello world") {
		t.Fatal(r.Content)
	}
	run(t, gw, p, "shell.exec", map[string]any{"argv": []string{"ls", "-la"}})
	for _, cmd := range []string{"echo hi; rm -rf /", "echo $(id)", "echo hi | sh", "echo `id`", "echo > f", "cat /etc/passwd", "echo hi\nid"} {
		if _, err := call(t, gw, p, "shell.exec", map[string]any{"command": cmd}); !errors.Is(err, ErrDenied) {
			t.Errorf("%q must be denied, got %v", cmd, err)
		}
	}
	if _, err := call(t, gw, p, "shell.exec", map[string]any{"argv": []string{"sh", "-c", "id"}}); !errors.Is(err, ErrDenied) {
		t.Fatalf("argv not in allowlist must be denied, got %v", err)
	}
}

func TestGitTools(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	if out, err := exec.Command("git", "-C", dir, "init", "-q", "-b", "main").CombinedOutput(); err != nil {
		t.Skipf("git init: %v %s", err, out)
	}
	gw := NewGateway(Builtin(BuiltinConfig{}), nil, nil)
	p := principal(dir, domain.ToolGrant{Name: "git.*"}, domain.ToolGrant{Name: "filesystem.write"})
	run(t, gw, p, "filesystem.write", map[string]any{"path": "a.txt", "content": "one\n"})
	r := run(t, gw, p, "git.status", map[string]any{})
	if !strings.Contains(r.Content, "a.txt") {
		t.Fatalf("status: %q", r.Content)
	}
	run(t, gw, p, "git.add", map[string]any{"paths": []string{"a.txt"}})
	run(t, gw, p, "git.commit", map[string]any{"message": "first"})
	run(t, gw, p, "git.branch", map[string]any{"name": "feature", "checkout": true})
	run(t, gw, p, "filesystem.write", map[string]any{"path": "a.txt", "content": "two\n"})
	r = run(t, gw, p, "git.diff", map[string]any{})
	if !strings.Contains(r.Content, "+two") {
		t.Fatalf("diff: %q", r.Content)
	}
	run(t, gw, p, "git.commit", map[string]any{"message": "second", "all": true})
	run(t, gw, p, "git.checkout", map[string]any{"ref": "main"})
	run(t, gw, p, "git.merge", map[string]any{"branch": "feature", "noFF": true, "message": "merge feature"})
	r = run(t, gw, p, "git.log", map[string]any{"n": 5})
	if !strings.Contains(r.Content, "merge feature") || !strings.Contains(r.Content, "second") {
		t.Fatalf("log: %q", r.Content)
	}
	run(t, gw, p, "git.show", map[string]any{"ref": "HEAD~1"})
	run(t, gw, p, "git.branches", map[string]any{})
	if r, _ := call(t, gw, p, "git.show", map[string]any{"ref": "--output=/tmp/x"}); r == nil || !r.IsError || !strings.Contains(r.Content, "invalid ref") {
		t.Fatal("option injection must be refused")
	}
	// git.read alone cannot commit.
	ro := principal(dir, domain.ToolGrant{Name: "git.read"})
	if _, err := call(t, gw, ro, "git.commit", map[string]any{"message": "x"}); !errors.Is(err, ErrDenied) {
		t.Fatalf("want denied, got %v", err)
	}
}

func TestHTTPFetchSSRF(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><head><title>t</title><style>x{}</style></head><body><h1>Hello &amp; welcome</h1><script>evil()</script><p>Para</p></body></html>`)
	}))
	defer srv.Close()

	gw := NewGateway(Builtin(BuiltinConfig{}), nil, nil)
	p := principal(t.TempDir(), domain.ToolGrant{Name: "http.fetch"})
	if _, err := call(t, gw, p, "http.fetch", map[string]any{"url": srv.URL}); !errors.Is(err, ErrDenied) {
		t.Fatalf("loopback must be refused, got %v", err)
	}
	if _, err := call(t, gw, p, "http.fetch", map[string]any{"url": "http://169.254.169.254/latest/meta-data"}); !errors.Is(err, ErrDenied) {
		t.Fatalf("metadata endpoint must be refused, got %v", err)
	}
	if r, _ := call(t, gw, p, "http.fetch", map[string]any{"url": "file:///etc/passwd"}); r == nil || !r.IsError {
		t.Fatal("file:// must be refused")
	}

	open := NewGateway(Builtin(BuiltinConfig{AllowPrivateNetworks: true}), nil, nil)
	r := run(t, open, p, "http.fetch", map[string]any{"url": srv.URL})
	if !strings.Contains(r.Content, "Hello & welcome") || strings.Contains(r.Content, "evil") || strings.Contains(r.Content, "<p>") {
		t.Fatalf("html to text: %q", r.Content)
	}
	hosts := principal(t.TempDir(), domain.ToolGrant{Name: "http.fetch", Hosts: []string{"example.org"}})
	if _, err := call(t, open, hosts, "http.fetch", map[string]any{"url": srv.URL}); !errors.Is(err, ErrDenied) {
		t.Fatalf("host allowlist must apply, got %v", err)
	}
}

func TestDockerSandboxArgs(t *testing.T) {
	s, err := NewDockerSandbox(DockerSandboxConfig{Name: "lf-g-dev-1", Image: "alpine:3", HostDir: "/srv/ws/g", CPU: "1", Memory: "512m", Labels: map[string]string{"weft.graph": "g"},
		Volumes: []string{"weft-env-g-1a2b3c4d:/home/sandbox/go/pkg/mod"}})
	if err != nil {
		t.Fatal(err)
	}
	run := strings.Join(s.RunArgs(), " ")
	for _, want := range []string{"--network none", "--cpus 1", "--memory 512m", "-v /srv/ws/g:/workspace", "--label weft.graph=g", "--cap-drop ALL", "-v weft-env-g-1a2b3c4d:/home/sandbox/go/pkg/mod"} {
		if !strings.Contains(run, want) {
			t.Errorf("run args missing %q: %s", want, run)
		}
	}
	args, err := s.ExecArgs(ExecRequest{Shell: "go test ./...", Dir: "svc", Env: map[string]string{"A": "1"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(args, " "); got != "exec -w /workspace/svc -e A=1 lf-g-dev-1 sh -c go test ./..." {
		t.Fatalf("exec args: %s", got)
	}
	if _, err := s.ExecArgs(ExecRequest{Argv: []string{"ls"}, Dir: "../x"}); !errors.Is(err, ErrDenied) {
		t.Fatalf("dir escape must be denied, got %v", err)
	}
}

func TestWritesIntoGitDirAreRefused(t *testing.T) {
	dir := t.TempDir()
	p := &Principal{Workdir: dir, Grants: []domain.ToolGrant{{Name: "filesystem.*"}}}
	gw := NewGateway(Builtin(BuiltinConfig{}), nil, nil)
	for _, c := range []struct{ tool, args string }{
		{"filesystem.write", `{"path":".git/hooks/post-commit","content":"#!/bin/sh\nid\n"}`},
		{"filesystem.write", `{"path":"sub/.GIT/config","content":"[core]\nfsmonitor=evil"}`},
		{"filesystem.mkdir", `{"path":".git/hooks"}`},
		{"filesystem.move", `{"from":"a.txt","to":".git/hooks/pre-commit"}`},
		{"filesystem.delete", `{"path":".git","recursive":true}`},
	} {
		res, err := gw.Invoke(context.Background(), p, c.tool, json.RawMessage(c.args))
		if err == nil && (res == nil || !res.IsError) {
			t.Fatalf("%s %s was allowed", c.tool, c.args)
		}
	}
	// Reading repository metadata stays allowed.
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0o644)
	res, err := gw.Invoke(context.Background(), p, "filesystem.read", json.RawMessage(`{"path":".git/HEAD"}`))
	if err != nil || res.IsError {
		t.Fatalf("reading .git/HEAD: %v %+v", err, res)
	}
}
