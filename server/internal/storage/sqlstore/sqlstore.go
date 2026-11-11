// Package sqlstore implements storage.Store on database/sql for SQLite and
// PostgreSQL with one code path and a deliberately small dialect layer.
//
// # Why one implementation for two databases
//
// SQLite and PostgreSQL agree on almost everything this store needs:
// INSERT ... ON CONFLICT DO UPDATE, RETURNING, partial updates guarded by a
// WHERE clause, and substr(). Keeping a single implementation means the two
// backends cannot drift semantically - the conformance suite runs against
// both - and every fix lands once. The dialect layer covers only what truly
// differs: placeholder syntax (? vs $n), column types (INTEGER nanoseconds vs
// TIMESTAMPTZ, TEXT vs JSONB, BLOB vs BYTEA), connection pool policy, and the
// advisory locks PostgreSQL needs for multi-instance correctness.
//
// # Schema shape
//
// Each entity has the columns it is filtered and ordered by as real, indexed
// columns, plus the full Go struct as a JSON document. Filters stay fast and
// type-checked, while adding a field to a domain struct needs no migration.
// Events and message history are the exception: they are the platform's
// audit trail, so every attribute is a first-class column operators can query
// with plain SQL.
//
// # Concurrency
//
//   - Graph saves use optimistic concurrency: the version row's primary key
//     (id, version) makes two writers racing for the same next version
//     collide; the loser gets storage.ErrConflict.
//   - Approvals, operator messages and leases use guarded single-statement
//     updates ("... WHERE status = 'pending'", "... WHERE consumed_at IS NULL",
//     upsert "... WHERE holder = excluded.holder OR expires_at < now"), which
//     are atomic in both databases without explicit row locks.
//   - Event sequence numbers come from AUTOINCREMENT / BIGSERIAL. In
//     PostgreSQL, sequence values are handed out before commit, so two
//     concurrent appends could become visible out of order and a reader
//     polling "seq > N" could skip one. Appends therefore take a transaction
//     scoped advisory lock, which serialises them and makes seq order equal
//     commit order. Event appends are short, so the cost is negligible next
//     to the guarantee that SSE resumption never loses an event.
//   - Lease expiry is compared against the caller's clock (time.Now on the
//     platform instance). Instances must keep clocks synchronised (NTP) to
//     well within the lease TTL; this is stated in the deployment guide.
//
// # SQLite specifics
//
// The pool is limited to one connection. SQLite permits a single writer at a
// time anyway; with several pooled connections, concurrent writers surface
// as SQLITE_BUSY errors and transactions that read-then-write can deadlock on
// lock upgrades. A single connection serialises access inside the process,
// which is the honest model for an embedded single-node database, and WAL
// mode still lets external readers (backups, the sqlite3 CLI) read while the
// platform writes. Deployments that need concurrent throughput use
// PostgreSQL.
package sqlstore

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // registers driver "pgx"
	_ "modernc.org/sqlite"             // registers driver "sqlite"

	"github.com/woliveiras/weft/server/internal/storage"
)

//go:embed migrations/sqlite/*.sql migrations/postgres/*.sql
var migrationsFS embed.FS

// Dialect identifies the SQL backend.
type Dialect string

const (
	SQLite   Dialect = "sqlite"
	Postgres Dialect = "postgres"
)

// Store implements storage.Store.
type Store struct {
	db      *sql.DB
	dialect Dialect
}

var _ storage.Store = (*Store)(nil)

// advisory lock keys (PostgreSQL). Arbitrary constants, unique per purpose.
const (
	lockMigrate int64 = 0x4c46_0001 // "LF" migrate
	lockEvents  int64 = 0x4c46_0002 // "LF" event append
)

// OpenSQLite opens (creating if needed) a SQLite database. dsn may be a
// plain path ("/data/weft.db") or a "file:" URI; the pragmas the store
// relies on are added when absent.
func OpenSQLite(ctx context.Context, dsn string) (*Store, error) {
	db, err := sql.Open("sqlite", sqliteDSN(dsn))
	if err != nil {
		return nil, fmt.Errorf("sqlstore: open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1) // see package doc: one writer, no SQLITE_BUSY races
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)
	s := &Store{db: db, dialect: SQLite}
	if err := s.Ping(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func sqliteDSN(dsn string) string {
	if dsn == "" {
		dsn = "weft.db"
	}
	if dsn == ":memory:" {
		// A shared in-memory database must stay on one connection; the pool
		// limit above guarantees that.
		dsn = "file::memory:"
	}
	if !strings.HasPrefix(dsn, "file:") {
		dsn = "file:" + dsn
	}
	base, query, _ := strings.Cut(dsn, "?")
	vals, err := url.ParseQuery(query)
	if err != nil {
		vals = url.Values{}
	}
	pragmas := vals["_pragma"]
	has := func(name string) bool {
		for _, p := range pragmas {
			if strings.HasPrefix(strings.ToLower(p), name) {
				return true
			}
		}
		return false
	}
	defaults := []struct{ name, value string }{
		{"journal_mode", "journal_mode(WAL)"},
		{"busy_timeout", "busy_timeout(10000)"},
		{"foreign_keys", "foreign_keys(1)"},
		{"synchronous", "synchronous(NORMAL)"},
	}
	for _, d := range defaults {
		if !has(d.name) {
			vals.Add("_pragma", d.value)
		}
	}
	if vals.Get("_txlock") == "" {
		// BEGIN IMMEDIATE: take the write lock up front so a read-then-write
		// transaction never fails at its first write.
		vals.Set("_txlock", "immediate")
	}
	return base + "?" + vals.Encode()
}

// OpenPostgres connects to PostgreSQL with a DSN or URL understood by pgx
// ("postgres://user:pass@host:5432/db?sslmode=disable").
func OpenPostgres(ctx context.Context, dsn string) (*Store, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("sqlstore: open postgres: %w", err)
	}
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(30 * time.Minute)
	db.SetConnMaxIdleTime(5 * time.Minute)
	s := &Store{db: db, dialect: Postgres}
	if err := s.Ping(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// New wraps an existing *sql.DB (tests, custom pools).
func New(db *sql.DB, d Dialect) *Store { return &Store{db: db, dialect: d} }

// DB exposes the pool for diagnostics.
func (s *Store) DB() *sql.DB { return s.db }

func (s *Store) Name() string { return string(s.dialect) }

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Ping(ctx context.Context) error {
	if err := s.db.PingContext(ctx); err != nil {
		return fmt.Errorf("sqlstore: ping %s: %w", s.dialect, err)
	}
	return nil
}

// Migrate applies every embedded migration not yet recorded in
// schema_migrations, each in its own transaction. On PostgreSQL a session
// advisory lock makes concurrent instances apply migrations one at a time.
func (s *Store) Migrate(ctx context.Context) error {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("sqlstore: migrate: %w", err)
	}
	defer conn.Close()

	if s.dialect == Postgres {
		if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", lockMigrate); err != nil {
			return fmt.Errorf("sqlstore: migrate lock: %w", err)
		}
		defer conn.ExecContext(context.Background(), "SELECT pg_advisory_unlock($1)", lockMigrate) //nolint:errcheck
	}

	if _, err := conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    INTEGER PRIMARY KEY,
		name       TEXT    NOT NULL,
		applied_at TEXT    NOT NULL
	)`); err != nil {
		return fmt.Errorf("sqlstore: create schema_migrations: %w", err)
	}

	applied := map[int]bool{}
	rows, err := conn.QueryContext(ctx, "SELECT version FROM schema_migrations")
	if err != nil {
		return fmt.Errorf("sqlstore: read schema_migrations: %w", err)
	}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return err
		}
		applied[v] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	dir := "migrations/" + string(s.dialect)
	entries, err := fs.ReadDir(migrationsFS, dir)
	if err != nil {
		return fmt.Errorf("sqlstore: read migrations: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".sql") {
			continue
		}
		num, _, _ := strings.Cut(name, "_")
		version, err := strconv.Atoi(num)
		if err != nil {
			return fmt.Errorf("sqlstore: migration %s: name must start with a number", name)
		}
		if applied[version] {
			continue
		}
		body, err := migrationsFS.ReadFile(dir + "/" + name)
		if err != nil {
			return err
		}
		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("sqlstore: apply %s: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx, s.q("INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, ?)"),
			version, name, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			tx.Rollback()
			return fmt.Errorf("sqlstore: record %s: %w", name, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("sqlstore: commit %s: %w", name, err)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// dialect helpers
// ---------------------------------------------------------------------------

// q rewrites "?" placeholders to "$n" for PostgreSQL. Queries in this
// package never contain a literal '?' inside string constants.
func (s *Store) q(query string) string {
	if s.dialect != Postgres {
		return query
	}
	var b strings.Builder
	b.Grow(len(query) + 16)
	n := 0
	for i := 0; i < len(query); i++ {
		if query[i] == '?' {
			n++
			b.WriteByte('$')
			b.WriteString(strconv.Itoa(n))
			continue
		}
		b.WriteByte(query[i])
	}
	return b.String()
}

// ts converts a time to the column representation.
func (s *Store) ts(t time.Time) any {
	if s.dialect == Postgres {
		return t.UTC()
	}
	return t.UTC().UnixNano()
}

// tsPtr is ts for nullable columns.
func (s *Store) tsPtr(t *time.Time) any {
	if t == nil || t.IsZero() {
		return nil
	}
	return s.ts(*t)
}

// jsonArg passes a JSON document; nil/empty becomes SQL NULL.
func jsonArg(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return string(b)
}

// tsScanner reads either representation back as UTC time.
type tsScanner struct{ t *time.Time }

func (s tsScanner) Scan(v any) error {
	switch x := v.(type) {
	case nil:
		*s.t = time.Time{}
	case time.Time:
		*s.t = x.UTC()
	case int64:
		*s.t = time.Unix(0, x).UTC()
	case float64:
		*s.t = time.Unix(0, int64(x)).UTC()
	case []byte:
		return s.parse(string(x))
	case string:
		return s.parse(x)
	default:
		return fmt.Errorf("sqlstore: cannot scan %T into time", v)
	}
	return nil
}

func (s tsScanner) parse(v string) error {
	if n, err := strconv.ParseInt(v, 10, 64); err == nil {
		*s.t = time.Unix(0, n).UTC()
		return nil
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999999-07:00", "2006-01-02 15:04:05.999999999"} {
		if t, err := time.Parse(layout, v); err == nil {
			*s.t = t.UTC()
			return nil
		}
	}
	return fmt.Errorf("sqlstore: cannot parse time %q", v)
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique constraint") || // sqlite
		strings.Contains(msg, "duplicate key") || // postgres
		strings.Contains(msg, "sqlstate 23505")
}

func notFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return storage.ErrNotFound
	}
	return err
}

// where accumulates AND-ed conditions and their arguments.
type where struct {
	conds []string
	args  []any
}

func (w *where) add(cond string, args ...any) {
	w.conds = append(w.conds, cond)
	w.args = append(w.args, args...)
}

func (w *where) String() string {
	if len(w.conds) == 0 {
		return ""
	}
	return " WHERE " + strings.Join(w.conds, " AND ")
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat("?, ", n-1) + "?"
}
