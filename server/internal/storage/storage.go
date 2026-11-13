// Package storage defines the persistence abstraction. Everything that must
// survive a restart goes through Store: graph definitions and their version
// history, desired/actual control state, message history, executions, the
// append-only event log, approvals, operator messages, agent memory (KV) and
// the replica leases that let several platform instances share work.
//
// Adapters: filesystem (single node, zero dependencies, human-inspectable),
// SQLite (single node, transactional, default) and PostgreSQL (multi-instance,
// production). All three pass the same conformance suite.
package storage

import (
	"context"
	"errors"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
)

var (
	ErrNotFound = errors.New("storage: not found")
	ErrConflict = errors.New("storage: conflict")
)

// Store is the root persistence interface.
type Store interface {
	// Migrate brings the schema up to date. Idempotent.
	Migrate(ctx context.Context) error
	Ping(ctx context.Context) error
	Close() error
	Name() string

	// --- graphs ---------------------------------------------------------
	// SaveGraph stores a new version of a graph document. If expectedVersion
	// > 0 and the current version differs, ErrConflict is returned
	// (optimistic concurrency for concurrent editors). The stored version
	// number is written into g.Metadata.Version and returned.
	SaveGraph(ctx context.Context, g *domain.Graph, expectedVersion int) (int, error)
	// GetGraph returns the latest version (version 0) or a specific one.
	GetGraph(ctx context.Context, id string, version int) (*domain.Graph, error)
	ListGraphs(ctx context.Context) ([]*domain.Graph, error)
	ListGraphVersions(ctx context.Context, id string) ([]GraphVersion, error)
	DeleteGraph(ctx context.Context, id string) error

	// GraphRecord is the control state (desired/actual/run id).
	GetGraphRecord(ctx context.Context, id string) (*domain.GraphRecord, error)
	PutGraphRecord(ctx context.Context, r *domain.GraphRecord) error
	// PutGraphRecordIf writes the record only if all setting revisions match.
	PutGraphRecordIf(ctx context.Context, r *domain.GraphRecord, guards []SettingRevision) error
	// UpdateGraphState changes observed state only if the complete snapshot still matches.
	UpdateGraphState(ctx context.Context, expected *domain.GraphRecord, state domain.GraphState) error
	ListGraphRecords(ctx context.Context) ([]*domain.GraphRecord, error)

	// --- events ---------------------------------------------------------
	// AppendEvent assigns ev.Seq and persists it.
	AppendEvent(ctx context.Context, ev *domain.Event) error
	ListEvents(ctx context.Context, f EventFilter) ([]*domain.Event, error)

	// --- messages -------------------------------------------------------
	// RecordMessage inserts or updates history for (message id, channel, group).
	RecordMessage(ctx context.Context, r *domain.MessageRecord) error
	ListMessages(ctx context.Context, f MessageFilter) ([]*domain.MessageRecord, error)
	GetMessage(ctx context.Context, id string) ([]*domain.MessageRecord, error)
	// MarkProcessed records that group processed message id. It returns false
	// if it was already recorded (duplicate delivery). This is the consumer
	// side of effectively-once processing.
	MarkProcessed(ctx context.Context, group, messageID string) (bool, error)
	// IsProcessed reports whether group already processed message id. The
	// engine checks it before a turn so a redelivered duplicate is acked
	// and skipped instead of being processed again.
	IsProcessed(ctx context.Context, group, messageID string) (bool, error)

	// --- executions -----------------------------------------------------
	PutExecution(ctx context.Context, e *domain.Execution) error
	GetExecution(ctx context.Context, id string) (*domain.Execution, error)
	ListExecutions(ctx context.Context, f ExecutionFilter) ([]*domain.Execution, error)

	// --- approvals ------------------------------------------------------
	PutApproval(ctx context.Context, a *domain.Approval) error
	GetApproval(ctx context.Context, id string) (*domain.Approval, error)
	ListApprovals(ctx context.Context, f ApprovalFilter) ([]*domain.Approval, error)
	// DecideApproval atomically moves a pending approval to a decided state;
	// ErrConflict if it is no longer pending (two humans clicking at once).
	DecideApproval(ctx context.Context, id string, status domain.ApprovalStatus, by, comment, answer string) (*domain.Approval, error)

	// --- operator messages ------------------------------------------------
	PutOperatorMessage(ctx context.Context, m *domain.OperatorMessage) error
	// PutOperatorMessageOnce inserts without overwriting a previously consumed message.
	PutOperatorMessageOnce(ctx context.Context, m *domain.OperatorMessage) error
	// ConsumeOperatorMessages atomically marks and returns unconsumed messages
	// addressed to this replica (or to any replica: Replica == -1).
	ConsumeOperatorMessages(ctx context.Context, graph, node string, replica int) ([]*domain.OperatorMessage, error)
	ListOperatorMessages(ctx context.Context, graph, node string, limit int) ([]*domain.OperatorMessage, error)

	// --- replica status ---------------------------------------------------
	PutReplicaStatus(ctx context.Context, s *domain.ReplicaStatus) error
	ListReplicaStatus(ctx context.Context, graph string) ([]*domain.ReplicaStatus, error)
	DeleteReplicaStatus(ctx context.Context, graph, node string, replica int) error

	// --- agent memory (KV) ------------------------------------------------
	// Namespaces are "<graph>/<node>" (shared by replicas) or
	// "<graph>/<node>#<n>" (private to one replica).
	KVGet(ctx context.Context, ns, key string) ([]byte, error)
	KVPut(ctx context.Context, ns, key string, value []byte) error
	KVDelete(ctx context.Context, ns, key string) error
	KVList(ctx context.Context, ns, prefix string) (map[string][]byte, error)

	// --- leases -----------------------------------------------------------
	// AcquireLease takes or renews key for holder. It succeeds if the key is
	// free, expired, or already held by holder.
	AcquireLease(ctx context.Context, key, holder string, ttl time.Duration) (bool, error)
	ReleaseLease(ctx context.Context, key, holder string) error
	ListLeases(ctx context.Context, prefix string) ([]domain.Lease, error)

	// --- installation settings ---------------------------------------------
	// Settings are operator-managed documents (model providers, integration
	// connections, ...) keyed by a path-like name ("llm/providers"). Values
	// are opaque to the store; callers encrypt anything secret before writing.
	// Every write is a compare-and-set on the revision so two operators
	// editing the same document cannot silently overwrite each other.
	GetSetting(ctx context.Context, key string) (*Setting, error)
	// PutSetting stores value when the current revision equals
	// expectedRevision (0 = the key must not exist yet) and returns the new
	// revision. A mismatch returns ErrConflict.
	PutSetting(ctx context.Context, key string, value []byte, expectedRevision int64, by string) (int64, error)
	// PutSettingIf atomically checks the target revision and any additional guards.
	PutSettingIf(ctx context.Context, key string, value []byte, expectedRevision int64, by string, guards []SettingRevision) (int64, error)
	// DeleteSetting removes key when its revision equals expectedRevision.
	// ErrNotFound if absent, ErrConflict on a revision mismatch.
	DeleteSetting(ctx context.Context, key string, expectedRevision int64) error
	// ListSettings returns every setting whose key starts with prefix
	// (literal, case-sensitive), ordered by key.
	ListSettings(ctx context.Context, prefix string) ([]*Setting, error)
}

// SettingRevision pins a setting to a revision. Zero requires an absent key.
type SettingRevision struct {
	Key      string
	Revision int64
}

// Setting is one stored installation setting.
type Setting struct {
	Key       string    `json:"key"`
	Value     []byte    `json:"value"`
	Revision  int64     `json:"revision"`
	UpdatedAt time.Time `json:"updatedAt"`
	UpdatedBy string    `json:"updatedBy,omitempty"`
}

type GraphVersion struct {
	Version   int       `json:"version"`
	CreatedAt time.Time `json:"createdAt"`
	Author    string    `json:"author,omitempty"`
}

type EventFilter struct {
	GraphID   string
	NodeID    string
	Types     []domain.EventType
	AfterSeq  int64
	BeforeSeq int64 // exclusive cursor for newest-first pages
	Limit     int   // default 200, max 5000
	Ascending bool
}

type MessageFilter struct {
	GraphID       string
	Channel       string
	NodeID        string // source or target
	CorrelationID string
	Status        domain.MessageStatus
	Limit         int
	Before        time.Time
}

type ExecutionFilter struct {
	ParentExecutionID string
	GraphID           string
	NodeID            string
	Status            domain.ExecutionStatus
	Limit             int
}

type ApprovalFilter struct {
	GraphID string
	Status  domain.ApprovalStatus
	Limit   int
}

func Limit(n, def, max int) int {
	if n <= 0 {
		return def
	}
	if n > max {
		return max
	}
	return n
}
