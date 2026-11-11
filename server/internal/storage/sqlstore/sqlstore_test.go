package sqlstore_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/storage/conformance"
	"github.com/woliveiras/weft/server/internal/storage/sqlstore"
)

func TestSQLiteConformance(t *testing.T) {
	conformance.Run(t, func(t *testing.T) storage.Store {
		s, err := sqlstore.OpenSQLite(context.Background(), filepath.Join(t.TempDir(), "weft.db"))
		if err != nil {
			t.Fatal(err)
		}
		return s
	})
}

// TestPostgresConformance runs against a live server when
// WEFT_TEST_POSTGRES_DSN is set. Each subtest gets a fresh schema.
func TestPostgresConformance(t *testing.T) {
	dsn := os.Getenv("WEFT_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("WEFT_TEST_POSTGRES_DSN not set")
	}
	n := 0
	conformance.Run(t, func(t *testing.T) storage.Store {
		n++
		schema := "conf_" + strings.ToLower(strings.NewReplacer("/", "_", "-", "_").Replace(t.Name()))
		admin, err := sqlstore.OpenPostgres(context.Background(), dsn)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := admin.DB().Exec(`DROP SCHEMA IF EXISTS ` + schema + ` CASCADE; CREATE SCHEMA ` + schema); err != nil {
			t.Fatal(err)
		}
		admin.Close()
		sep := "?"
		if strings.Contains(dsn, "?") {
			sep = "&"
		}
		s, err := sqlstore.OpenPostgres(context.Background(), dsn+sep+"search_path="+schema)
		if err != nil {
			t.Fatal(err)
		}
		return s
	})
}

func TestSQLiteDSNMemory(t *testing.T) {
	s, err := sqlstore.OpenSQLite(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestExecutionParentMigrationBackfillsExistingRecords(t *testing.T) {
	ctx := context.Background()
	s, err := sqlstore.OpenSQLite(ctx, filepath.Join(t.TempDir(), "old.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	// Build a genuine pre-parent-index fixture, including its migration journal.
	for _, file := range []string{"0001_init.sql", "0002_settings.sql"} {
		b, e := os.ReadFile("migrations/sqlite/" + file)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = s.DB().ExecContext(ctx, string(b)); e != nil {
			t.Fatal(e)
		}
	}
	if _, err = s.DB().ExecContext(ctx, `CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY,name TEXT NOT NULL,applied_at TEXT NOT NULL);INSERT INTO schema_migrations VALUES(1,'init','fixture'),(2,'settings','fixture');INSERT INTO executions(id,graph_id,node_id,status,started_at,doc) VALUES('old-child','g','n','succeeded',1,'{"id":"old-child","graphId":"g","parentExecutionId":"old-root","summary":"preserved"}')`); err != nil {
		t.Fatal(err)
	}
	if err = s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err = s.Migrate(ctx); err != nil {
		t.Fatal("repeat migration", err)
	}
	children, err := s.ListExecutions(ctx, storage.ExecutionFilter{ParentExecutionID: "old-root"})
	if err != nil || len(children) != 1 || children[0].Summary != "preserved" {
		t.Fatal(children, err)
	}
}
