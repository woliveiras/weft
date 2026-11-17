package app

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/workspacejournal"
)

func TestWorkspaceJournalMetricsEndpointWithoutImport(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	t.Setenv("WEFT_WORKSPACE_JOURNAL", path)
	writer, err := workspacejournal.OpenWriter(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Append(workspacejournal.Record{Gap: "collector_started"}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Storage.DSN = filepath.Join(dir, "weft.db")
	cfg.Queue.Type = "memory"
	cfg.Workspaces.Root = filepath.Join(dir, "workspaces")
	cfg.Secrets = config.Secrets{KeyFile: filepath.Join(dir, "test.key"), GenerateKeyFile: true}
	cfg.Server.ProtectMetrics = true
	cfg.Auth.Tokens = []config.Token{{Name: "fixture", Role: config.RoleViewer, Token: "journal-test-viewer"}}
	previousLog := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previousLog) })
	a, err := New(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close(ctx)
	handler := a.API.Handler()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("protected metrics without authorization: %d", response.Code)
	}
	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	request.Header.Set("Authorization", "Bearer journal-test-viewer")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "\nweft_workspace_journal_active_bytes ") ||
		!strings.Contains(response.Body.String(), "\nweft_workspace_journal_collector_heartbeat_timestamp_seconds 0\n") {
		t.Fatalf("journal metrics unavailable before the first import: HTTP %d", response.Code)
	}
}
