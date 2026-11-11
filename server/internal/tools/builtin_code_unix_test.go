//go:build unix

package tools

import (
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
)

func TestCodeOutlineRefusesSpecialFiles(t *testing.T) {
	dir := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(dir, "pipe.go"), 0o644); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	gw := NewGateway(Builtin(BuiltinConfig{}), nil, nil)
	p := principal(dir, domain.ToolGrant{Name: "filesystem.read"})
	r, err := call(t, gw, p, "code.outline", map[string]any{"path": "pipe.go"})
	if err != nil || !r.IsError || !strings.Contains(r.Content, "not a regular file") {
		t.Fatalf("a FIFO must be refused without blocking: %v %+v", err, r)
	}
}
