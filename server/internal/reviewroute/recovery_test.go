package reviewroute

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/scm"
	"github.com/woliveiras/weft/server/internal/storage"
)

func dispatchEvidence(v Receipt) *domain.MessageRecord {
	return &domain.MessageRecord{
		GraphID: v.Route.GraphID, Channel: v.Route.ChannelID, Status: domain.MsgPublished,
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		Message: domain.Message{ID: v.MessageID, Type: MessageType, Source: "system:review-router", Target: v.Route.NodeID,
			Payload: []byte(`{}`), Metadata: map[string]any{
				"review_dispatch": true, domain.MetaInjectedBy: "system:review-router",
				domain.MetaGraph: v.Route.GraphID, domain.MetaChannel: v.Route.ChannelID,
				domain.MetaRunID: v.ExpectedRunID, domain.MetaGraphVersion: v.Route.GraphVersion,
			}},
	}
}

func TestHistoricalMessageRequiresExactInternalEnvelope(t *testing.T) {
	v := Receipt{Route: Route{Input: validInput()}, MessageID: "stable-review-input", ExpectedRunID: "run-reviewed"}
	for name, mutate := range map[string]func(*domain.MessageRecord){
		"graph row":   func(r *domain.MessageRecord) { r.GraphID = "other" },
		"channel row": func(r *domain.MessageRecord) { r.Channel = "other" },
		"id":          func(r *domain.MessageRecord) { r.Message.ID = "other" },
		"type":        func(r *domain.MessageRecord) { r.Message.Type = "other" },
		"source":      func(r *domain.MessageRecord) { r.Message.Source = "external:admin" },
		"target":      func(r *domain.MessageRecord) { r.Message.Target = "other" },
		"flag":        func(r *domain.MessageRecord) { r.Message.Metadata["review_dispatch"] = "true" },
		"actor":       func(r *domain.MessageRecord) { r.Message.Metadata[domain.MetaInjectedBy] = "external:admin" },
		"graph":       func(r *domain.MessageRecord) { r.Message.Metadata[domain.MetaGraph] = "other" },
		"channel":     func(r *domain.MessageRecord) { r.Message.Metadata[domain.MetaChannel] = "other" },
		"run":         func(r *domain.MessageRecord) { r.Message.Metadata[domain.MetaRunID] = "other" },
		"version":     func(r *domain.MessageRecord) { r.Message.Metadata[domain.MetaGraphVersion] = v.Route.GraphVersion + 1 },
		"fraction": func(r *domain.MessageRecord) {
			r.Message.Metadata[domain.MetaGraphVersion] = float64(v.Route.GraphVersion) + .5
		},
		"missing version": func(r *domain.MessageRecord) { delete(r.Message.Metadata, domain.MetaGraphVersion) },
		"dead":            func(r *domain.MessageRecord) { r.Status = domain.MsgDeadLettered },
		"rejected":        func(r *domain.MessageRecord) { r.Status = domain.MsgRejected },
	} {
		t.Run(name, func(t *testing.T) {
			r := dispatchEvidence(v)
			if !historicalMessageMatches(r, v) {
				t.Fatal("valid evidence rejected")
			}
			mutate(r)
			if historicalMessageMatches(r, v) {
				t.Fatal("foreign or invalid evidence accepted")
			}
		})
	}
	for _, status := range []domain.MessageStatus{domain.MsgPublished, domain.MsgDelivered, domain.MsgAcked, domain.MsgHeld} {
		r := dispatchEvidence(v)
		r.Status = status
		r.Message.Metadata[domain.MetaGraphVersion] = float64(v.Route.GraphVersion)
		if !historicalMessageMatches(r, v) {
			t.Fatalf("valid status %s rejected", status)
		}
	}
	if historicalMessageMatches(nil, v) {
		t.Fatal("nil evidence accepted")
	}
	for _, actor := range []string{"", " admin", "admin\n", "\x00", string([]byte{0xff})} {
		if recoveryInput(strings.Repeat("a", 64), 1, actor) {
			t.Fatal("invalid recovery input accepted")
		}
	}
}

func unknownReceipt(t *testing.T, f *fixture) Receipt {
	t.Helper()
	r := f.put(t)
	f.event(t, 1)
	if err := f.manager.scanRoute(t.Context(), r); err != nil {
		t.Fatal(err)
	}
	f.manager.Dispatch = func(context.Context, Route, Receipt) (string, error) {
		f.calls.Add(1)
		return "", errors.New("lost broker acknowledgement")
	}
	v := f.receipts(t)[0]
	if err := f.manager.process(t.Context(), v.ID); err == nil {
		t.Fatal("uncertainty was hidden")
	}
	return f.receipts(t)[0]
}

func TestReconcileUsesHistoricalEvidenceAcrossRestartAndChangedConfiguration(t *testing.T) {
	for _, backend := range []string{"fs", "sqlite"} {
		for _, status := range []domain.MessageStatus{domain.MsgPublished, domain.MsgAcked, domain.MsgDelivered, domain.MsgHeld} {
			t.Run(backend+"/"+string(status), func(t *testing.T) {
				f := newFixture(t, backend, "github")
				v := unknownReceipt(t, f)
				row := dispatchEvidence(v)
				row.Status = status
				if err := f.store.RecordMessage(t.Context(), row); err != nil {
					t.Fatal(err)
				}
				// Reconciliation is historical: a disabled route, rotated connection,
				// and stopped graph must not erase proof of an earlier dispatch.
				r, err := f.manager.Get(t.Context(), f.input.Name)
				if err != nil {
					t.Fatal(err)
				}
				r.Enabled = false
				if _, err = f.manager.Put(t.Context(), r.Input, r.Revision, "admin"); err != nil {
					t.Fatal(err)
				}
				token := "replacement-synthetic-token"
				if _, err = f.manager.SCM.Save(t.Context(), scm.Input{Name: f.input.Connection, Kind: f.input.Provider, Repository: f.input.Repository, Token: &token}, "admin", true); err != nil {
					t.Fatal(err)
				}
				if err = f.store.PutGraphRecord(t.Context(), &domain.GraphRecord{ID: f.input.GraphID, Version: 2, RunID: "another-run", State: domain.GraphStopped}); err != nil {
					t.Fatal(err)
				}
				f.reopen(t)
				f.manager.Validate = func(context.Context, Route) error {
					t.Fatal("historical recovery used current authorization")
					return nil
				}
				got, err := f.manager.Reconcile(t.Context(), v.ID, v.Revision, "operator")
				if err != nil || got.State != "published" || got.DispatchRunID != v.ExpectedRunID || f.calls.Load() != 1 {
					t.Fatal(got, err, f.calls.Load())
				}
				if _, err = f.manager.Reconcile(t.Context(), v.ID, got.Revision, "operator"); !errors.Is(err, storage.ErrConflict) {
					t.Fatal("published receipt reconciled again", err)
				}
			})
		}
	}
}

func TestRetryFailedRequiresUnchangedAuthorityAndKnownPredispatchFailure(t *testing.T) {
	for _, backend := range []string{"fs", "sqlite"} {
		t.Run(backend, func(t *testing.T) {
			f := newFixture(t, backend, "github")
			r := f.put(t)
			f.event(t, 1)
			if err := f.manager.scanRoute(t.Context(), r); err != nil {
				t.Fatal(err)
			}
			if err := f.store.PutGraphRecord(t.Context(), &domain.GraphRecord{ID: f.input.GraphID, Version: 1, State: domain.GraphStopped, DesiredState: domain.GraphStopped}); err != nil {
				t.Fatal(err)
			}
			v := f.receipts(t)[0]
			if err := f.manager.process(t.Context(), v.ID); err == nil {
				t.Fatal("stopped destination accepted")
			}
			v = f.receipts(t)[0]
			if v.State != "failed" || f.calls.Load() != 0 {
				t.Fatal(v)
			}
			if _, err := f.manager.RetryFailed(t.Context(), v.ID, v.Revision, "admin"); !errors.Is(err, ErrUnavailable) {
				t.Fatal("stopped destination requeued", err)
			}
			if err := f.store.PutGraphRecord(t.Context(), &domain.GraphRecord{ID: f.input.GraphID, Version: 1, RunID: "run-reviewed", State: domain.GraphRunning, DesiredState: domain.GraphRunning}); err != nil {
				t.Fatal(err)
			}
			f.reopen(t)
			if _, err := f.manager.RetryFailed(t.Context(), v.ID, v.Revision+1, "admin"); !errors.Is(err, storage.ErrConflict) {
				t.Fatal("stale revision accepted", err)
			}
			got, err := f.manager.RetryFailed(t.Context(), v.ID, v.Revision, "admin")
			if err != nil || got.State != "queued" || got.MessageID != v.MessageID || got.EventID != v.EventID || got.SourceExecutionID != v.SourceExecutionID || f.calls.Load() != 0 {
				t.Fatal(got, err)
			}
			if err = f.manager.process(t.Context(), v.ID); err != nil {
				t.Fatal(err)
			}
			got = f.receipts(t)[0]
			if got.State != "published" || f.calls.Load() != 1 {
				t.Fatal(got, f.calls.Load())
			}
			if _, err = f.manager.RetryFailed(t.Context(), v.ID, got.Revision, "admin"); !errors.Is(err, storage.ErrConflict) {
				t.Fatal("published receipt retried", err)
			}
		})
	}
	for _, kind := range []string{"route", "connection", "policy", "unknown", "dispatching", "journaled failed", "queued"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t, "fs", "github")
			r := f.put(t)
			f.event(t, 1)
			if err := f.manager.scanRoute(t.Context(), r); err != nil {
				t.Fatal(err)
			}
			v := f.receipts(t)[0]
			v.State = "failed"
			switch kind {
			case "unknown", "dispatching":
				v.State, v.ExpectedRunID = kind, "previous-run"
			case "journaled failed":
				v.ExpectedRunID = "previous-run"
			case "queued":
				v.State = "queued"
			case "route":
				r.Enabled = false
				if _, err := f.manager.Put(t.Context(), r.Input, r.Revision, "admin"); err != nil {
					t.Fatal(err)
				}
			case "connection":
				if _, err := f.manager.SCM.Save(t.Context(), scm.Input{Name: f.input.Connection, Kind: f.input.Provider, Repository: f.input.Repository}, "admin", true); err != nil {
					t.Fatal(err)
				}
			case "policy":
				f.manager.Validate = func(context.Context, Route) error { return ErrUnavailable }
			}
			var err error
			v, err = f.manager.saveReceipt(t.Context(), v, v.Revision)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = f.manager.RetryFailed(t.Context(), v.ID, v.Revision, "admin"); err == nil {
				t.Fatal("unsafe retry accepted")
			}
			got, err := f.manager.GetReceipt(t.Context(), v.ID)
			if err != nil || got.State != v.State || got.Revision != v.Revision || f.calls.Load() != 0 {
				t.Fatal(got, err)
			}
		})
	}
}

func TestReconcileAbsenceAndForeignEvidenceRemainUnknown(t *testing.T) {
	for _, kind := range []string{"missing", "foreign graph", "foreign run", "foreign target", "public injection", "failed delivery", "dispatching"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t, "fs", "github")
			v := unknownReceipt(t, f)
			if kind == "dispatching" {
				v.State = "dispatching"
				var err error
				v, err = f.manager.saveReceipt(t.Context(), v, v.Revision)
				if err != nil {
					t.Fatal(err)
				}
			} else if kind != "missing" {
				row := dispatchEvidence(v)
				switch kind {
				case "foreign graph":
					row.GraphID = "other"
				case "foreign run":
					row.Message.Metadata[domain.MetaRunID] = "other"
				case "foreign target":
					row.Message.Target = "other"
				case "public injection":
					row.Message.Source = "external:admin"
				case "failed delivery":
					row.Status = domain.MsgDeadLettered
				}
				if err := f.store.RecordMessage(t.Context(), row); err != nil {
					t.Fatal(err)
				}
			}
			f.reopen(t)
			got, err := f.manager.Reconcile(t.Context(), v.ID, v.Revision, "operator")
			if err != nil || got.State != "unknown" || got.Error == "" || got.DispatchRunID != "" {
				t.Fatal(got, err)
			}
			if err = f.manager.Tick(t.Context()); err != nil {
				t.Fatal(err)
			}
			if f.calls.Load() != 1 {
				t.Fatal("recovery replayed dispatch")
			}
		})
	}
}

type recoveryFaultStore struct {
	storage.Store
	read, audit, save, release bool
}

var errRecoveryFault = errors.New("synthetic recovery storage failure")

func (s recoveryFaultStore) GetMessage(ctx context.Context, id string) ([]*domain.MessageRecord, error) {
	if s.read {
		return nil, errRecoveryFault
	}
	return s.Store.GetMessage(ctx, id)
}
func (s recoveryFaultStore) AppendEvent(ctx context.Context, ev *domain.Event) error {
	if s.audit {
		return errRecoveryFault
	}
	return s.Store.AppendEvent(ctx, ev)
}
func (s recoveryFaultStore) PutSettingIf(ctx context.Context, key string, data []byte, expected int64, actor string, guards []storage.SettingRevision) (int64, error) {
	if s.save {
		return 0, errRecoveryFault
	}
	return s.Store.PutSettingIf(ctx, key, data, expected, actor, guards)
}
func (s recoveryFaultStore) ReleaseLease(ctx context.Context, name, holder string) error {
	if s.release {
		if _, ok := ctx.Deadline(); !ok {
			return errors.New("cleanup has no timeout")
		}
		return errors.Join(s.Store.ReleaseLease(ctx, name, holder), errRecoveryFault)
	}
	return s.Store.ReleaseLease(ctx, name, holder)
}

func TestRecoveryPropagatesStorageFailuresAndRequiresCASAndLease(t *testing.T) {
	for _, kind := range []string{"read", "audit", "save", "release", "stale", "lease"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t, "fs", "github")
			v := unknownReceipt(t, f)
			if err := f.store.RecordMessage(t.Context(), dispatchEvidence(v)); err != nil {
				t.Fatal(err)
			}
			f.manager.Store = recoveryFaultStore{Store: f.store, read: kind == "read", audit: kind == "audit", save: kind == "save", release: kind == "release"}
			expected := v.Revision
			if kind == "stale" {
				expected++
			}
			if kind == "lease" {
				ok, err := f.store.AcquireLease(t.Context(), "review-route/"+v.ID, "active-dispatch", time.Minute)
				if !ok || err != nil {
					t.Fatal(ok, err)
				}
			}
			_, err := f.manager.Reconcile(t.Context(), v.ID, expected, "operator")
			if kind == "stale" || kind == "lease" {
				if !errors.Is(err, storage.ErrConflict) {
					t.Fatal(err)
				}
			} else if !errors.Is(err, errRecoveryFault) {
				t.Fatal("storage error hidden", err)
			}
			stored, getErr := f.manager.GetReceipt(t.Context(), v.ID)
			if getErr != nil {
				t.Fatal(getErr)
			}
			if kind != "release" && (stored.State != v.State || stored.Revision != v.Revision) {
				t.Fatal("failed recovery changed receipt", stored)
			}
			if f.calls.Load() != 1 {
				t.Fatal("recovery dispatched")
			}
		})
	}
}

func TestRecoveryInputValidationIsLocal(t *testing.T) {
	validID := strings.Repeat("a", 64)
	for _, input := range []struct {
		id       string
		revision int64
		actor    string
	}{
		{"invalid", 1, "operator"}, {validID, 0, "operator"}, {validID, -1, "operator"},
		{validID, 1, ""}, {validID, 1, "operator\n"},
	} {
		m := Manager{}
		if _, err := m.Reconcile(t.Context(), input.id, input.revision, input.actor); !errors.Is(err, ErrInvalid) {
			t.Fatal(err)
		}
		if _, err := m.RetryFailed(t.Context(), input.id, input.revision, input.actor); !errors.Is(err, ErrInvalid) {
			t.Fatal(err)
		}
	}
}

func failedReceipt(t *testing.T, f *fixture) Receipt {
	t.Helper()
	r := f.put(t)
	f.event(t, 1)
	if err := f.manager.scanRoute(t.Context(), r); err != nil {
		t.Fatal(err)
	}
	v := f.receipts(t)[0]
	v.State = "failed"
	var err error
	v, err = f.manager.saveReceipt(t.Context(), v, v.Revision)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

type retryAuthorityRaceStore struct {
	storage.Store
	change func()
}

func (s retryAuthorityRaceStore) AppendEvent(ctx context.Context, ev *domain.Event) error {
	if err := s.Store.AppendEvent(ctx, ev); err != nil {
		return err
	}
	if ev.Type == RecoveryRequested {
		s.change()
	}
	return nil
}

func TestRetryGuardsAuthorityChangesAfterValidationAndPreservesFailedStateOnStorageErrors(t *testing.T) {
	for _, kind := range []string{"route race", "connection race", "audit failure", "save failure", "lease held"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t, "sqlite", "github")
			v := failedReceipt(t, f)
			switch kind {
			case "route race", "connection race":
				f.manager.Store = retryAuthorityRaceStore{Store: f.store, change: func() {
					if kind == "connection race" {
						if _, err := f.manager.SCM.Save(t.Context(), scm.Input{Name: f.input.Connection, Kind: f.input.Provider, Repository: f.input.Repository}, "admin", true); err != nil {
							t.Fatal(err)
						}
					} else {
						local := f.manager
						local.Store = f.store
						r, err := local.Get(t.Context(), f.input.Name)
						if err != nil {
							t.Fatal(err)
						}
						r.Enabled = false
						if _, err = local.Put(t.Context(), r.Input, r.Revision, "admin"); err != nil {
							t.Fatal(err)
						}
					}
				}}
			case "audit failure", "save failure":
				f.manager.Store = recoveryFaultStore{Store: f.store, audit: kind == "audit failure", save: kind == "save failure"}
			case "lease held":
				ok, err := f.store.AcquireLease(t.Context(), "review-route/"+v.ID, "active-worker", time.Minute)
				if !ok || err != nil {
					t.Fatal(ok, err)
				}
			}
			_, err := f.manager.RetryFailed(t.Context(), v.ID, v.Revision, "admin")
			if err == nil {
				t.Fatal("unsafe retry succeeded")
			}
			if kind == "audit failure" || kind == "save failure" {
				if !errors.Is(err, errRecoveryFault) {
					t.Fatal(err)
				}
			} else if !errors.Is(err, storage.ErrConflict) {
				t.Fatal(err)
			}
			stored, err := f.manager.GetReceipt(t.Context(), v.ID)
			if err != nil || stored.State != "failed" || stored.Revision != v.Revision || f.calls.Load() != 0 {
				t.Fatal(stored, err)
			}
		})
	}
}

func TestProcessReportsLeaseCleanupFailureWithoutReplayingDispatch(t *testing.T) {
	f := newFixture(t, "fs", "github")
	r := f.put(t)
	f.event(t, 1)
	if err := f.manager.scanRoute(t.Context(), r); err != nil {
		t.Fatal(err)
	}
	v := f.receipts(t)[0]
	f.manager.Store = recoveryFaultStore{Store: f.store, release: true}
	if err := f.manager.process(t.Context(), v.ID); !errors.Is(err, errRecoveryFault) {
		t.Fatal("cleanup error hidden", err)
	}
	got, err := f.manager.GetReceipt(t.Context(), v.ID)
	if err != nil || got.State != "published" || f.calls.Load() != 1 {
		t.Fatal(got, err)
	}
	f.manager.Store = f.store
	if err = f.manager.process(t.Context(), v.ID); err != nil || f.calls.Load() != 1 {
		t.Fatal(err, f.calls.Load())
	}
}
