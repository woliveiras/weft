package environment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/woliveiras/weft/server/internal/storage"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const requestFixture = `{"time":"2026-10-16T12:00:00Z","msg":"egress request","host":"example.org","outcome":"denied","reason":"host_not_allowed","durationMs":2,"secret":"must-not-return"}`

func TestProxyRequestProjection(t *testing.T) {
	input := "arbitrary secret\n" + requestFixture + "\n" + strings.ReplaceAll(requestFixture, "example.org", "https://user:secret@host/path?token=secret") + "\n" + strings.ReplaceAll(requestFixture, "host_not_allowed", "sensitive raw failure")
	rows := parseProxyRequests([]byte(input))
	if len(rows) != 3 || rows[1].Host != "" || rows[2].Reason != "unspecified" {
		t.Fatal(rows)
	}
	b, _ := json.Marshal(rows)
	if strings.Contains(string(b), "secret") || strings.Contains(string(b), "sensitive") {
		t.Fatal(string(b))
	}
	if rows := parseProxyRequests([]byte(strings.Repeat(requestFixture+"\n", 120))); len(rows) != 100 {
		t.Fatal(len(rows))
	}
	for _, bad := range []string{strings.ReplaceAll(requestFixture, "egress request", "other"), strings.ReplaceAll(requestFixture, "denied", "unknown"), strings.ReplaceAll(requestFixture, "2026-10-16T12:00:00Z", "invalid"), strings.ReplaceAll(requestFixture, `"durationMs":2`, `"durationMs":-1`)} {
		if len(parseProxyRequests([]byte(bad))) != 0 {
			t.Fatal(bad)
		}
	}
	var output proxyOutput
	if _, err := output.Write(make([]byte, 256<<10)); err != nil {
		t.Fatal(err)
	}
	if _, err := output.Write([]byte("overflow")); err == nil {
		t.Fatal("unbounded diagnostics")
	}
}
func proxyFixture(t *testing.T) (*harness, string) {
	h := setup(t)
	dir := t.TempDir()
	bin := filepath.Join(dir, "docker")
	script := `#!/bin/sh
set -eu
d='` + dir + `'
echo "$*" >> "$d/calls"
case "$1" in
 ps) if [ -f "$d/fail-list" ]; then echo private-error >&2; exit 1; fi; if [ -f "$d/names" ]; then cat "$d/names"; elif [ ! -f "$d/empty" ]; then echo proxy-one; fi;;
 inspect) cat "$d/state";;
 logs) if [ -f "$d/fail-logs" ]; then echo token=secret >&2; exit 1; fi; cat "$d/logs";;
esac
`
	if err := os.WriteFile(bin, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"state": `{"managed":"true","graph":"bookaneer","phase":"preparation","state":"running"}`, "logs": requestFixture} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	h.m.opt.DockerBin = bin
	return h, dir
}
func TestProxyDiagnosticsIntegration(t *testing.T) {
	h, dir := proxyFixture(t)
	ctx := context.Background()
	v, err := h.m.ProxyStatus(ctx, "bookaneer")
	if err != nil || len(v.Proxies) != 1 || len(v.Proxies[0].Requests) != 1 {
		t.Fatal(v, err)
	}
	if err := h.m.capturePreparationProxy(ctx, "bookaneer"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "empty"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	v, err = h.m.ProxyStatus(ctx, "bookaneer")
	if err != nil || len(v.Proxies) != 0 || v.LastPreparation == nil || len(v.LastPreparation.Requests) != 1 {
		t.Fatal(v, err)
	}
	if err := os.Remove(filepath.Join(dir, "empty")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fail-logs"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	v, err = h.m.ProxyStatus(ctx, "bookaneer")
	if err != nil || v.Proxies[0].Warning == "" || len(v.Proxies[0].Requests) != 0 {
		t.Fatal(v, err)
	}
	before, _ := os.ReadFile(filepath.Join(dir, "calls"))
	v, err = h.m.ProxyStatus(ctx, "another-graph")
	if err != nil || v.Proxies[0].State != "unknown" {
		t.Fatal(v, err)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "calls"))
	if strings.Contains(string(after[len(before):]), "logs") {
		t.Fatal("read logs from another graph")
	}
	if err := os.WriteFile(filepath.Join(dir, "fail-list"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = h.m.ProxyStatus(ctx, "bookaneer"); err == nil || strings.Contains(err.Error(), "private-error") {
		t.Fatal(err)
	}
}

func TestProxyInventoryBoundsAndHistoryReset(t *testing.T) {
	h, dir := proxyFixture(t)
	ctx := context.Background()
	var names strings.Builder
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&names, "proxy-%d\n", i)
	}
	if err := os.WriteFile(filepath.Join(dir, "names"), []byte(names.String()), 0600); err != nil {
		t.Fatal(err)
	}
	v, err := h.m.ProxyStatus(ctx, "bookaneer")
	if err != nil || !v.Truncated || len(v.Proxies) != 32 {
		t.Fatal(v, err)
	}
	if err := h.m.capturePreparationProxy(ctx, "bookaneer"); err != nil {
		t.Fatal(err)
	}
	if err := h.m.Reset(ctx, "bookaneer", "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.KVGet(ctx, proxyHistoryNamespace, "bookaneer"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatal("reset retained preparation diagnostics", err)
	}
}

type proxyWriteFailure struct{ storage.Store }

func (proxyWriteFailure) KVPut(context.Context, string, string, []byte) error {
	return errors.New("storage unavailable")
}
func TestProxySnapshotStorageFailure(t *testing.T) {
	h, _ := proxyFixture(t)
	h.m.opt.Store = proxyWriteFailure{h.store}
	if err := h.m.capturePreparationProxy(context.Background(), "bookaneer"); err == nil {
		t.Fatal("snapshot write failure hidden")
	}
}
