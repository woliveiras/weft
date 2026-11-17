package main

import (
	"bytes"
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/app"
	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/storage"
)

func cliServer(t *testing.T) (*client, *app.App) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Storage = config.Storage{Type: "sqlite", Dir: dir}
	cfg.Queue = config.Queue{Type: "memory"}
	cfg.Workspaces.Root = filepath.Join(dir, "ws")
	cfg.Secrets = config.Secrets{KeyFile: filepath.Join(dir, "key"), GenerateKeyFile: true}
	cfg.LLM.DevMock = true
	cfg.Observability.LogLevel = "error"
	cfg.Auth.Tokens = []config.Token{{Name: "admin", Role: config.RoleAdmin, Token: "cli-test-admin-token"}}
	a, err := app.New(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(a.API.Handler())
	t.Cleanup(func() {
		srv.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		a.Close(ctx)
	})
	return &client{base: srv.URL, token: "cli-test-admin-token", http: srv.Client()}, a
}

const cliGraph = "apiVersion: weft.io/v1\nkind: Graph\nmetadata: {name: cli-graph}\nspec:\n  defaults: {runtime: {kind: echo}}\n  nodes: [{id: worker}]\n"

func TestCLIConfigurationFileDirectoryDryRunConfirmAndExport(t *testing.T) {
	c, a := cliServer(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "graph.yaml")
	if err := os.WriteFile(path, []byte(cliGraph), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := c.applyConfiguration([]string{"-f", path, "--dry-run"}, strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Store.GetGraph(t.Context(), "cli-graph", 0); err != storage.ErrNotFound {
		t.Fatalf("dry-run wrote graph: %v", err)
	}
	if _, err := os.Stat(path + ".weft-state.json"); !os.IsNotExist(err) {
		t.Fatal("dry-run wrote state")
	}
	if err := c.applyConfiguration([]string{"-f", path}, strings.NewReader("n\n"), &out); err == nil {
		t.Fatal("missing cancellation")
	}
	if err := c.applyConfiguration([]string{"-f", path}, strings.NewReader("yes\n"), &out); err != nil {
		t.Fatal(err)
	}
	g, err := a.Store.GetGraph(t.Context(), "cli-graph", 0)
	if err != nil || g.Metadata.Version != 1 {
		t.Fatalf("legacy graph: %+v %v", g, err)
	}
	if err = c.applyConfiguration([]string{"-f", path, "--yes"}, strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	g, _ = a.Store.GetGraph(t.Context(), "cli-graph", 0)
	if g.Metadata.Version != 1 {
		t.Fatal("idempotent apply saved another version")
	}
	exported := filepath.Join(t.TempDir(), "export.yaml")
	if err = c.exportConfiguration([]string{"-o", exported}); err != nil {
		t.Fatal(err)
	}
	if err = c.applyConfiguration([]string{"-f", exported, "--dry-run"}, strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	if err = os.WriteFile(filepath.Join(directory, "01.yaml"), []byte(strings.ReplaceAll(cliGraph, "cli-graph", "directory-graph")), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(directory, "02.yml"), []byte("apiVersion: weft.io/v1\nkind: Budget\nmetadata: {name: installation/*}\nspec: {scope: installation, id: '*', limitMicros: 10000000}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = c.applyConfiguration([]string{"-f", directory, "--yes"}, strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	if _, err = a.Store.GetGraph(t.Context(), "directory-graph", 0); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "unchanged Graph/cli-graph") {
		t.Fatal("missing printed plan")
	}
}
func TestReadConfigFilesRejectsDuplicatesAndSymlinks(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.yaml", "b.yml"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(cliGraph), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := readConfigFiles(dir); err == nil {
		t.Fatal("duplicate object accepted")
	}
	if err := os.Symlink(filepath.Join(dir, "a.yaml"), filepath.Join(t.TempDir(), "linked.yaml")); err != nil {
		t.Fatal(err)
	}
}

func TestCLIForceAndMissingConfirmation(t *testing.T) {
	c, a := cliServer(t)
	path := filepath.Join(t.TempDir(), "graph.yaml")
	if err := os.WriteFile(path, []byte(cliGraph), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := c.applyConfiguration([]string{"-f", path}, strings.NewReader(""), &out); err == nil {
		t.Fatal("EOF approved apply")
	}
	if err := c.applyConfiguration([]string{"-f", path, "--yes"}, strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	g, err := a.Store.GetGraph(t.Context(), "cli-graph", 0)
	if err != nil {
		t.Fatal(err)
	}
	g.Metadata.Description = "console edit"
	if _, _, err = a.Engine.SaveGraph(t.Context(), g, 1, "console-admin"); err != nil {
		t.Fatal(err)
	}
	if err = c.applyConfiguration([]string{"-f", path, "--yes"}, strings.NewReader(""), &out); err == nil {
		t.Fatal("console edit silently overwritten")
	}
	if err = c.applyConfiguration([]string{"-f", path, "--yes", "--force", "Graph/cli-graph"}, strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	g, err = a.Store.GetGraph(t.Context(), "cli-graph", 0)
	if err != nil || g.Metadata.Description != "" {
		t.Fatalf("forced update failed: %+v %v", g, err)
	}
}
