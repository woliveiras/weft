package taskconfig

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
)

// RetryFailed prepares a fresh draft; it never resumes execution. A source
// snapshot has one deterministic successor and a create-only CAS, so retries
// after an uncertain response cannot duplicate, overwrite or resurrect it.
func (m Manager) RetryFailed(ctx context.Context, id string, revision int64, actor string) (*Draft, bool, error) {
	source, err := m.Get(ctx, id)
	if err != nil {
		return nil, false, err
	}
	if revision < 1 || source.Revision != revision || source.Status != "failed" || source.TriggerIssueOrigin != nil || source.TriggerGitHubIssue != "" {
		return nil, false, fmt.Errorf("%w: retry requires the unchanged failed manual task; recover issue-trigger tasks through their trigger", storage.ErrConflict)
	}
	hash := sha256.Sum256([]byte("task-retry\n" + id + "\n" + strconv.FormatInt(revision, 10)))
	childID := "tsk_" + hex.EncodeToString(hash[:])
	readChild := func() (*Draft, error) {
		row, err := m.Store.GetSetting(ctx, prefix+childID)
		if err != nil {
			return nil, err
		}
		child, err := decode(row)
		if err != nil {
			return nil, err
		}
		if child.Status == "deleted" {
			return nil, fmt.Errorf("%w: the retry draft was deleted; create a new task manually", storage.ErrConflict)
		}
		if child.ID != childID || child.RetryOfTaskID != id || child.RetryOfRevision != revision {
			return nil, fmt.Errorf("%w: inconsistent retry task identity", storage.ErrConflict)
		}
		return child, nil
	}
	if child, err := readChild(); err == nil {
		return child, false, nil
	} else if !errors.Is(err, storage.ErrNotFound) {
		return nil, false, err
	}
	// Copy only editable input, not launch state, resolutions, approvals or
	// consent. Keep old pins even if the current workflow has changed: the normal
	// review must require an explicit repair rather than silently adopting it.
	inputJSON, err := json.Marshal(source.Input)
	if err != nil {
		return nil, false, err
	}
	var input Input
	if err = json.Unmarshal(inputJSON, &input); err != nil {
		return nil, false, err
	}
	now := time.Now().UTC()
	child := &Draft{Input: input, ID: childID, Status: "draft", RetryOfTaskID: id, RetryOfRevision: revision, CreatedBy: actor, UpdatedBy: actor, CreatedAt: now, UpdatedAt: now}
	if err = m.Store.AppendEvent(ctx, domain.NewEvent("task.retry_requested", source.GraphID, map[string]any{"taskId": childID, "retryOfTaskId": id, "retryOfRevision": revision}).WithActor(actor)); err != nil {
		return nil, false, err
	}
	payload, err := json.Marshal(child)
	if err != nil {
		return nil, false, err
	}
	child.Revision, err = m.Store.PutSetting(ctx, prefix+childID, payload, 0, actor)
	if errors.Is(err, storage.ErrConflict) {
		existing, readErr := readChild()
		return existing, false, readErr
	}
	if err != nil {
		return nil, false, err
	}
	return child, true, nil
}
