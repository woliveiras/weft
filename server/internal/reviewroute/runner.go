package reviewroute

import (
	"context"
	"errors"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/scm"
	"github.com/woliveiras/weft/server/internal/storage"
)

// Tick scans at most ten route settings, twenty delivery facts per route and
// twenty receipt settings. Persisted exclusive cursors wrap at the end, so a
// large completed history never prevents reaching later pending work.
func (m Manager) Tick(ctx context.Context) error {
	if m.Store == nil {
		return ErrUnavailable
	}
	var failures []error
	if err := m.scanRoutes(ctx); err != nil {
		failures = append(failures, err)
	}
	if err := m.drain(ctx); err != nil {
		failures = append(failures, err)
	}
	return errors.Join(failures...)
}
func (m Manager) scanRoutes(ctx context.Context) error {
	const key = "scm/review-route-scan-cursor"
	c, rev, err := m.loadCursor(ctx, key)
	if err != nil {
		return err
	}
	routes, next, err := m.ListPage(ctx, c.AfterKey, 10)
	if err != nil {
		return err
	}
	var failures []error
	for _, r := range routes {
		if !r.Enabled {
			continue
		}
		if err = m.scanRoute(ctx, r); err != nil {
			failures = append(failures, err)
		}
	}
	err = m.saveCursor(ctx, key, cursor{AfterKey: next}, rev)
	if err != nil && !errors.Is(err, storage.ErrConflict) {
		failures = append(failures, err)
	}
	return errors.Join(failures...)
}
func (m Manager) scanRoute(ctx context.Context, r Route) error {
	key := eventCursorKey(r)
	c, rev, err := m.loadCursor(ctx, key)
	if err != nil {
		return err
	}
	if c.AfterSeq < r.ActivationSeq {
		c.AfterSeq = r.ActivationSeq
	}
	events, err := m.Store.ListEvents(ctx, storage.EventFilter{Types: []domain.EventType{domain.EvPullRequestOpened}, AfterSeq: c.AfterSeq, Ascending: true, Limit: 20})
	if err != nil {
		return err
	}
	for _, ev := range events {
		if ev.Seq <= c.AfterSeq {
			return errors.New("review route event cursor did not advance")
		}
		if err = m.enqueue(ctx, r, ev); err != nil {
			return err
		}
		c.AfterSeq = ev.Seq
	}
	if len(events) == 0 {
		return nil
	}
	return m.saveCursor(ctx, key, c, rev, storage.SettingRevision{Key: RoutePrefix + r.Name, Revision: r.Revision})
}
func (m Manager) drain(ctx context.Context) error {
	const key = "scm/review-route-receipt-scan-cursor"
	c, rev, err := m.loadCursor(ctx, key)
	if err != nil {
		return err
	}
	rows, next, err := m.ReceiptsPage(ctx, c.AfterKey, 20)
	if err != nil {
		return err
	}
	var failures []error
	for _, v := range rows {
		if v.State != "queued" && v.State != "dispatching" {
			continue
		}
		if err = m.process(ctx, v.ID); err != nil {
			failures = append(failures, err)
		}
	}
	err = m.saveCursor(ctx, key, cursor{AfterKey: next}, rev)
	if err != nil && !errors.Is(err, storage.ErrConflict) {
		failures = append(failures, err)
	}
	return errors.Join(failures...)
}

func (m Manager) process(ctx context.Context, id string) (resultErr error) {
	holder := domain.NewID("review-route")
	lease := "review-route/" + id
	ok, err := m.Store.AcquireLease(ctx, lease, holder, 2*time.Minute)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	defer func() { resultErr = errors.Join(resultErr, m.release(ctx, lease, holder)) }()
	v, err := m.GetReceipt(ctx, id)
	if err != nil {
		return err
	}
	if v.State == "dispatching" {
		v.State = "unknown"
		v.Error = ErrUnconfirmed.Error()
		_, err = m.saveReceipt(ctx, v, v.Revision)
		return errors.Join(ErrUnconfirmed, err)
	}
	if v.State != "queued" {
		return nil
	}
	fail := func(cause error) error {
		v.State = "failed"
		v.Error = "Review routing prerequisites changed before dispatch. Review the route, connection and running graph."
		_, err := m.saveReceipt(ctx, v, v.Revision)
		return errors.Join(cause, err)
	}
	r, err := m.Get(ctx, v.Route.Name)
	if err != nil {
		return fail(err)
	}
	if !r.Enabled || r.Revision != v.Route.Revision || r.Input != v.Route.Input || r.ActivationSeq != v.Route.ActivationSeq {
		return fail(storage.ErrConflict)
	}
	docRev, err := m.validate(ctx, r)
	if err != nil {
		return fail(err)
	}
	if m.Dispatch == nil {
		return fail(ErrUnavailable)
	}
	record, err := m.Store.GetGraphRecord(ctx, r.GraphID)
	if err != nil {
		return fail(err)
	}
	if record.Version != r.GraphVersion || record.State != domain.GraphRunning || record.DesiredState != domain.GraphRunning || record.RunID == "" {
		return fail(ErrUnavailable)
	}
	v.ExpectedRunID = record.RunID
	v.State = "dispatching"
	v.Error = ""
	v, err = m.saveReceipt(ctx, v, v.Revision, storage.SettingRevision{Key: RoutePrefix + r.Name, Revision: r.Revision}, storage.SettingRevision{Key: scm.SettingKey, Revision: docRev})
	if err != nil {
		return err
	}
	// From here onward, every failure is retained uncertainty. A future process
	// may only reconcile it; it must never call Dispatch again automatically.
	markUnknown := func(cause error) error {
		v.State = "unknown"
		v.Error = ErrUnconfirmed.Error()
		writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		_, saveErr := m.saveReceipt(writeCtx, v, v.Revision)
		return errors.Join(cause, saveErr)
	}
	current, err := m.Get(ctx, r.Name)
	if err != nil || current.Revision != r.Revision {
		return markUnknown(errors.Join(storage.ErrConflict, err))
	}
	if _, err = m.validate(ctx, r); err != nil {
		return markUnknown(err)
	}
	currentRun, err := m.Store.GetGraphRecord(ctx, r.GraphID)
	if err != nil {
		return markUnknown(err)
	}
	if currentRun.RunID != v.ExpectedRunID || currentRun.Version != r.GraphVersion || currentRun.State != domain.GraphRunning || currentRun.DesiredState != domain.GraphRunning {
		return markUnknown(storage.ErrConflict)
	}
	dispatchCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	dispatchRunID, dispatchErr := m.Dispatch(dispatchCtx, r, v)
	cancel()
	if dispatchErr != nil {
		return markUnknown(dispatchErr)
	}
	if dispatchRunID != v.ExpectedRunID {
		return markUnknown(ErrUnconfirmed)
	}
	v.State = "published"
	v.DispatchRunID = dispatchRunID
	writeCtx, cancelWrite := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancelWrite()
	_, err = m.saveReceipt(writeCtx, v, v.Revision)
	if err != nil {
		return errors.Join(ErrUnconfirmed, err)
	}
	return nil
}
