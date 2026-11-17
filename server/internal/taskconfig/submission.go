package taskconfig

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
	"regexp"
)

var submissionKey = regexp.MustCompile(`^[a-zA-Z0-9-]{16,80}$`)

type submission struct {
	Fingerprint string `json:"fingerprint"`
	TaskID      string `json:"taskId"`
}

// SaveSubmission reserves the client key before any model or launch side effect.
// A replay only reads the task; it never resolves or dispatches again. Uncertain
// reservations fail closed and are retained for diagnosis, not automatically reset.
func (m Manager) SaveSubmission(ctx context.Context, p PromptInput, key, actor string) (*Draft, bool, error) {
	if !submissionKey.MatchString(key) {
		return nil, false, fmt.Errorf("%w: provide a bounded submission key", ErrInvalid)
	}
	data, err := json.Marshal(p)
	if err != nil {
		return nil, false, err
	}
	hash := sha256.Sum256(data)
	fingerprint := hex.EncodeToString(hash[:])
	scoped := sha256.Sum256([]byte(actor + "\n" + key))
	path := "tasks/submissions/" + hex.EncodeToString(scoped[:])
	replay := func() (*Draft, bool, error) {
		row, e := m.Store.GetSetting(ctx, path)
		if e != nil {
			return nil, false, e
		}
		var saved submission
		if e = json.Unmarshal(row.Value, &saved); e != nil {
			return nil, false, e
		}
		if saved.Fingerprint != fingerprint {
			return nil, false, storage.ErrConflict
		}
		d, e := m.Get(ctx, saved.TaskID)
		if errors.Is(e, storage.ErrNotFound) {
			return nil, false, fmt.Errorf("%w: submission is pending or uncertain; inspect Tasks before a new submission", storage.ErrConflict)
		}
		return d, false, e
	}
	if _, err = m.Store.GetSetting(ctx, path); err == nil {
		return replay()
	} else if !errors.Is(err, storage.ErrNotFound) {
		return nil, false, err
	}
	id := p.ID
	if id == "" {
		id = domain.NewID("tsk")
	}
	row, _ := json.Marshal(submission{Fingerprint: fingerprint, TaskID: id})
	if _, err = m.Store.PutSetting(ctx, path, row, 0, actor); err != nil {
		if errors.Is(err, storage.ErrConflict) {
			return replay()
		}
		return nil, false, err
	}
	d, err := m.savePrompt(ctx, p, actor, id, true)
	return d, true, err
}
