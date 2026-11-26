package taskrun

import (
	"context"
	"time"

	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/taskauth"
	"github.com/woliveiras/weft/server/internal/taskconfig"
)

// PreparedBuild identifies the native build being offered for task admission.
// A rebuild of the same definition has a different completion timestamp.
type PreparedBuild struct {
	Graph     string `json:"graph"`
	Hash      string `json:"hash"`
	Completed string `json:"completed"`
}

func (r *Review) PreparedBuild() *PreparedBuild {
	v := r.Environment
	if r.Task == nil || r.Task.Status != "environment_pending" || v == nil || !v.UpToDate || v.Hash == "" || v.Hash != v.CurrentHash || v.Graph != ExecutionID(r.Task.ID) || v.FinishedAt == nil {
		return nil
	}
	return &PreparedBuild{Graph: v.Graph, Hash: v.Hash, Completed: v.FinishedAt.UTC().Format(time.RFC3339Nano)}
}

func (m Manager) StartPreparedForOperation(ctx context.Context, id string, revision int64, actor, operation string, consent taskauth.StartConsent, build PreparedBuild) (*taskconfig.Draft, error) {
	if operation == "" || consent.Scope == "" || build.Hash == "" {
		return nil, taskconfig.ErrInvalid
	}
	d, err := (taskconfig.Manager{Store: m.Store}).Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if d.Status != "environment_pending" {
		return nil, storage.ErrConflict
	}
	return m.startPreparedEnvironment(ctx, d, revision, actor, operation, consent, &build)
}
