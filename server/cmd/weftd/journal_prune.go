package main

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/storage/stores"
	"github.com/woliveiras/weft/server/internal/workspacejournal"
)

func runJournalPrune(ctx context.Context, cfg *config.Config, path string, keep int, apply bool, out io.Writer) (err error) {
	if path == "" {
		return fmt.Errorf("WEFT_WORKSPACE_JOURNAL is required")
	}
	if keep < 0 {
		return fmt.Errorf("--journal-keep-archives must be nonnegative")
	}
	store, err := stores.OpenWithoutMigrations(ctx, stores.Config{Type: cfg.Storage.Type, DSN: cfg.Storage.DSN, Dir: cfg.Storage.Dir})
	if err != nil {
		// Driver errors can include connection details from the installation.
		return fmt.Errorf("could not open the configured store")
	}
	defer func() { err = errors.Join(err, store.Close()) }()
	plan, err := workspacejournal.Prune(ctx, store, path, keep, apply)
	if err != nil {
		return err
	}
	action := "Eligible for pruning"
	if apply {
		action = "Pruned"
	}
	if _, err := fmt.Fprintf(out, "%s: %d archived segments (%d bytes)\n", action, len(plan.Archives), plan.Bytes); err != nil {
		return err
	}
	for _, archive := range plan.Archives {
		if _, err := fmt.Fprintln(out, archive); err != nil {
			return err
		}
	}
	return nil
}
