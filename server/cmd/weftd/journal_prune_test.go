package main

import (
	"bytes"
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/storage/sqlstore"
	"github.com/woliveiras/weft/server/internal/workspacejournal"
)

func TestJournalPruneCommandRejectsInvalidArguments(t *testing.T) {
	for _, tc := range []struct {
		path string
		keep int
		want string
	}{
		{"", 4, "WEFT_WORKSPACE_JOURNAL"},
		{"events", -1, "--journal-keep-archives"},
	} {
		if err := runJournalPrune(t.Context(), nil, tc.path, tc.keep, false, io.Discard); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("invalid arguments: %v", err)
		}
	}
}

func TestJournalPruneCommandUsesConfiguredStore(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	path := filepath.Join(dir, "events")
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
	store, err := sqlstore.OpenSQLite(ctx, cfg.Storage.DSN)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := runJournalPrune(ctx, cfg, path, workspacejournal.DefaultKeepArchives, false, &out); err == nil || !strings.Contains(err.Error(), "durable cursor") {
		t.Fatalf("preview without a committed cursor: %v", err)
	}
	importer := workspacejournal.Importer{Store: store, Path: path, Scopes: func(context.Context) ([]workspacejournal.Scope, error) {
		return nil, nil
	}}
	if err := importer.Import(ctx); err != nil {
		t.Fatal(err)
	}
	if err := runJournalPrune(ctx, cfg, path, workspacejournal.DefaultKeepArchives, false, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Eligible for pruning: 0 archived segments") {
		t.Fatalf("preview output: %s", out.String())
	}
}
