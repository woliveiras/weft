package environment

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

func markInterrupted(st *Status) {
	st.Phase = ""
	st.State, st.Error = StateFailed, "build interrupted: the Weft instance running it stopped; build again"
	st.FinishedAt = st.LastProgressAt
	if st.FinishedAt == nil {
		st.FinishedAt = st.StartedAt
	}
	if st.FinishedAt == nil {
		now := time.Now().UTC()
		st.FinishedAt = &now
	}
}

func (m *Manager) RunBuildRecovery(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		if err := m.recoverBuilds(ctx); err != nil && ctx.Err() == nil {
			m.opt.Log.Warn("environment build recovery failed", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (m *Manager) recoverBuilds(ctx context.Context) error {
	rows, err := m.opt.Store.KVList(ctx, kvNamespace, "")
	if err != nil {
		return err
	}
	var failures []error
	for id, data := range rows {
		var st Status
		if err := json.Unmarshal(data, &st); err != nil {
			failures = append(failures, err)
			continue
		}
		if st.State != StateBuilding || m.leaseAlive(ctx, id) {
			continue
		}
		if err := m.recoverBuild(ctx, id); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (m *Manager) recoverBuild(ctx context.Context, id string) error {
	work, release, err := m.holdEnvironmentLease(ctx, id, "environment-recovery")
	if errors.Is(err, ErrBuildRunning) || errors.Is(err, ErrResetRunning) || errors.Is(err, ErrEnvironmentRecovering) {
		return nil
	}
	if err != nil {
		return err
	}
	defer release()
	st, err := m.load(work, id)
	if err != nil || st.State != StateBuilding {
		return err
	}
	markInterrupted(st)
	if err := work.Err(); err != nil {
		return err
	}
	return m.save(work, st)
}
