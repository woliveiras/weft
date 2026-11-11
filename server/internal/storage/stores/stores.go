// Package stores selects and opens a storage.Store from configuration. It
// is the only package that imports every adapter, so the rest of the
// platform depends on the storage interface alone.
package stores

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/storage/fsstore"
	"github.com/woliveiras/weft/server/internal/storage/sqlstore"
)

// Config selects a backend.
type Config struct {
	// Type is "fs", "sqlite" (default) or "postgres".
	Type string `json:"type" yaml:"type"`
	// DSN is the PostgreSQL URL, or the SQLite path/URI. For SQLite an empty
	// DSN means <Dir>/weft.db.
	DSN string `json:"dsn" yaml:"dsn"`
	// Dir is the data directory for "fs" and the default SQLite location.
	Dir string `json:"dir" yaml:"dir"`
}

// Types lists the supported backends.
func Types() []string { return []string{"fs", "sqlite", "postgres"} }

// Open opens the configured store and runs its migrations.
func Open(ctx context.Context, cfg Config) (storage.Store, error) {
	s, err := OpenWithoutMigrations(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := s.Migrate(ctx); err != nil {
		s.Close()
		return nil, fmt.Errorf("stores: migrate %s: %w", s.Name(), err)
	}
	return s, nil
}

// OpenWithoutMigrations connects maintenance commands to the configured store
// without applying schema changes.
func OpenWithoutMigrations(ctx context.Context, cfg Config) (storage.Store, error) {
	dir := cfg.Dir
	if dir == "" {
		dir = "data"
	}
	var (
		s   storage.Store
		err error
	)
	switch strings.ToLower(strings.TrimSpace(cfg.Type)) {
	case "", "sqlite", "sqlite3":
		dsn := cfg.DSN
		if dsn == "" {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, fmt.Errorf("stores: create %s: %w", dir, err)
			}
			dsn = filepath.Join(dir, "weft.db")
		}
		s, err = sqlstore.OpenSQLite(ctx, dsn)
	case "postgres", "postgresql", "pg":
		if cfg.DSN == "" {
			return nil, fmt.Errorf("stores: postgres requires a DSN (postgres://user:pass@host:5432/db)")
		}
		s, err = sqlstore.OpenPostgres(ctx, cfg.DSN)
	case "fs", "file", "filesystem":
		if cfg.DSN != "" && cfg.Dir == "" {
			dir = cfg.DSN
		}
		s, err = fsstore.Open(filepath.Join(dir, "store"))
	default:
		return nil, fmt.Errorf("stores: unknown storage type %q (want one of %s)", cfg.Type, strings.Join(Types(), ", "))
	}
	if err != nil {
		return nil, err
	}
	return s, nil
}
