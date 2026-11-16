package tools

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
)

func TestRestrictedShellDenialExplainsRecoveryWithoutEchoingArguments(t *testing.T) {
	inv := &Invocation{Grant: domain.ToolGrant{Name: "shell.exec", Commands: []string{"go", "gofmt", "git"}}}
	for _, command := range []string{"cd server && go test ./...", "curl secret-argument", "go test; secret-argument"} {
		_, err := shellRequest(inv, command, nil, "")
		if !errors.Is(err, ErrDenied) {
			t.Fatal(err)
		}
		for _, want := range []string{"shell.exec is granted", "go gofmt git", "shell operators", "cwd", "argv", "separate calls"} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("missing %q: %v", want, err)
			}
		}
		if strings.Contains(err.Error(), "secret-argument") {
			t.Fatal("rejected arguments leaked")
		}
	}
	if !strings.Contains(shellTools()[0].Description(), "cwd") {
		t.Fatal("missing advance guidance")
	}
}

func TestRestrictedShellRecoveryUsesRealWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "server"), 0700); err != nil {
		t.Fatal(err)
	}
	gw := NewGateway(Builtin(BuiltinConfig{}), nil, nil)
	p := principal(dir, domain.ToolGrant{Name: "shell.exec", Commands: []string{"pwd"}})
	if _, err := call(t, gw, p, "shell.exec", map[string]any{"command": "cd server && pwd"}); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
	r := run(t, gw, p, "shell.exec", map[string]any{"argv": []string{"pwd"}, "cwd": "server"})
	if !strings.Contains(r.Content, filepath.Join(dir, "server")) {
		t.Fatal(r.Content)
	}
}
