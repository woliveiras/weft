// Package conformance is the behavioural specification of storage.Store.
// Every adapter runs it; an adapter is correct when it passes. Tests that
// need fresh state call newStore again, so the factory must return an empty,
// migrated store each time.
package conformance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
)

// Run executes the whole suite.
func Run(t *testing.T, newStore func(t *testing.T) storage.Store) {
	t.Helper()
	tests := []struct {
		name string
		fn   func(t *testing.T, s storage.Store)
	}{
		{"Graphs", testGraphs},
		{"GraphConflict", testGraphConflict},
		{"GraphRecords", testGraphRecords},
		{"GraphObservedState", testGraphObservedState},
		{"Events", testEvents},
		{"Messages", testMessages},
		{"MarkProcessed", testMarkProcessed},
		{"Executions", testExecutions},
		{"Approvals", testApprovals},
		{"ApprovalRace", testApprovalRace},
		{"OperatorMessages", testOperatorMessages},
		{"OperatorMessageOutbox", testOperatorMessageOutbox},
		{"ReplicaStatus", testReplicaStatus},
		{"KV", testKV},
		{"Leases", testLeases},
		{"LeaseRace", testLeaseRace},
		{"Settings", testSettings},
		{"SettingsRace", testSettingsRace},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newStore(t)
			t.Cleanup(func() { s.Close() })
			ctx := context.Background()
			if err := s.Migrate(ctx); err != nil {
				t.Fatalf("Migrate: %v", err)
			}
			if err := s.Migrate(ctx); err != nil {
				t.Fatalf("Migrate (second run must be idempotent): %v", err)
			}
			if err := s.Ping(ctx); err != nil {
				t.Fatalf("Ping: %v", err)
			}
			tc.fn(t, s)
		})
	}
}

var ctx = context.Background()

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func graph(id, desc string) *domain.Graph {
	return &domain.Graph{
		APIVersion: domain.APIVersion,
		Kind:       "Graph",
		Metadata:   domain.GraphMetadata{ID: id, Name: id, Description: desc},
		Spec: domain.GraphSpec{
			Nodes:    []domain.Node{{ID: "a", Prompt: "do " + desc}, {ID: "b"}},
			Channels: []domain.Channel{{ID: "c", Type: domain.ChannelQueue, From: "a", To: []string{"b"}}},
		},
	}
}

func testGraphs(t *testing.T, s storage.Store) {
	if _, err := s.GetGraph(ctx, "missing", 0); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("GetGraph missing: want ErrNotFound, got %v", err)
	}
	g := graph("g1", "one")
	v, err := s.SaveGraph(ctx, g, 0)
	must(t, err)
	if v != 1 || g.Metadata.Version != 1 {
		t.Fatalf("first save: version %d / metadata %d, want 1", v, g.Metadata.Version)
	}
	created := g.Metadata.CreatedAt
	if created.IsZero() {
		t.Fatal("CreatedAt not set")
	}
	g2 := graph("g1", "two")
	v, err = s.SaveGraph(ctx, g2, 1)
	must(t, err)
	if v != 2 {
		t.Fatalf("second save version %d, want 2", v)
	}
	// unconditional save
	v, err = s.SaveGraph(ctx, graph("g1", "three"), 0)
	must(t, err)
	if v != 3 {
		t.Fatalf("third save version %d, want 3", v)
	}

	latest, err := s.GetGraph(ctx, "g1", 0)
	must(t, err)
	if latest.Metadata.Description != "three" || latest.Metadata.Version != 3 {
		t.Fatalf("latest = %q v%d", latest.Metadata.Description, latest.Metadata.Version)
	}
	if !latest.Metadata.CreatedAt.Equal(created) {
		t.Fatalf("CreatedAt changed across versions: %v != %v", latest.Metadata.CreatedAt, created)
	}
	if len(latest.Spec.Nodes) != 2 || latest.Spec.Channels[0].To[0] != "b" {
		t.Fatalf("spec not round-tripped: %+v", latest.Spec)
	}
	v1, err := s.GetGraph(ctx, "g1", 1)
	must(t, err)
	if v1.Metadata.Description != "one" || v1.Metadata.Version != 1 {
		t.Fatalf("v1 = %q v%d", v1.Metadata.Description, v1.Metadata.Version)
	}
	if _, err := s.GetGraph(ctx, "g1", 9); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("missing version: want ErrNotFound, got %v", err)
	}

	vers, err := s.ListGraphVersions(ctx, "g1")
	must(t, err)
	if len(vers) != 3 || vers[0].Version != 1 || vers[2].Version != 3 {
		t.Fatalf("versions = %+v", vers)
	}
	if _, err := s.ListGraphVersions(ctx, "nope"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("ListGraphVersions missing: want ErrNotFound, got %v", err)
	}

	_, err = s.SaveGraph(ctx, graph("g0", "zero"), 0)
	must(t, err)
	all, err := s.ListGraphs(ctx)
	must(t, err)
	if len(all) != 2 {
		t.Fatalf("ListGraphs len %d, want 2", len(all))
	}
	ids := []string{all[0].Metadata.ID, all[1].Metadata.ID}
	sort.Strings(ids)
	if ids[0] != "g0" || ids[1] != "g1" {
		t.Fatalf("ListGraphs ids %v", ids)
	}
	for _, gg := range all {
		if gg.Metadata.ID == "g1" && gg.Metadata.Version != 3 {
			t.Fatalf("ListGraphs must return latest version, got v%d", gg.Metadata.Version)
		}
	}

	if _, err := s.SaveGraph(ctx, &domain.Graph{}, 0); err == nil {
		t.Fatal("SaveGraph without id must fail")
	}

	must(t, s.PutGraphRecord(ctx, &domain.GraphRecord{ID: "g1", DesiredState: domain.GraphRunning}))
	must(t, s.DeleteGraph(ctx, "g1"))
	if _, err := s.GetGraph(ctx, "g1", 0); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("after delete: want ErrNotFound, got %v", err)
	}
	if _, err := s.GetGraphRecord(ctx, "g1"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("record after delete: want ErrNotFound, got %v", err)
	}
	if err := s.DeleteGraph(ctx, "g1"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("second delete: want ErrNotFound, got %v", err)
	}
	// a deleted id can be created again from version 1
	v, err = s.SaveGraph(ctx, graph("g1", "again"), 0)
	must(t, err)
	if v != 1 {
		t.Fatalf("recreated graph version %d, want 1", v)
	}
}

func testGraphConflict(t *testing.T, s storage.Store) {
	_, err := s.SaveGraph(ctx, graph("g", "1"), 0)
	must(t, err)
	_, err = s.SaveGraph(ctx, graph("g", "2"), 1)
	must(t, err)
	g := graph("g", "stale")
	if _, err := s.SaveGraph(ctx, g, 1); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("stale save: want ErrConflict, got %v", err)
	}
	if g.Metadata.Version != 0 {
		t.Fatalf("failed save must not leave a version on the document, got %d", g.Metadata.Version)
	}
	if _, err := s.SaveGraph(ctx, graph("new", "x"), 4); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("expected version on missing graph: want ErrConflict, got %v", err)
	}
	latest, err := s.GetGraph(ctx, "g", 0)
	must(t, err)
	if latest.Metadata.Description != "2" {
		t.Fatalf("conflicting save leaked: %q", latest.Metadata.Description)
	}

	// Concurrent editors expecting the same version: exactly one wins.
	const n = 8
	var wins, conflicts int32
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := s.SaveGraph(ctx, graph("g", fmt.Sprintf("racer-%d", i)), 2)
			switch {
			case err == nil:
				atomic.AddInt32(&wins, 1)
			case errors.Is(err, storage.ErrConflict):
				atomic.AddInt32(&conflicts, 1)
			default:
				t.Errorf("racer %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
	if wins != 1 || conflicts != n-1 {
		t.Fatalf("concurrent saves: %d wins, %d conflicts; want 1 and %d", wins, conflicts, n-1)
	}
}

func testGraphRecords(t *testing.T, s storage.Store) {
	if _, err := s.GetGraphRecord(ctx, "x"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	r := &domain.GraphRecord{ID: "x", Version: 2, DesiredState: domain.GraphRunning, State: domain.GraphStopped, RunID: "run1", StartedAt: &now}
	must(t, s.PutGraphRecord(ctx, r))
	r.State = domain.GraphRunning
	must(t, s.PutGraphRecord(ctx, r))
	must(t, s.PutGraphRecord(ctx, &domain.GraphRecord{ID: "a", DesiredState: domain.GraphStopped}))
	got, err := s.GetGraphRecord(ctx, "x")
	must(t, err)
	if got.State != domain.GraphRunning || got.RunID != "run1" || got.Version != 2 || got.StartedAt == nil || !got.StartedAt.Equal(now) {
		t.Fatalf("record = %+v", got)
	}
	all, err := s.ListGraphRecords(ctx)
	must(t, err)
	if len(all) != 2 || all[0].ID != "a" || all[1].ID != "x" {
		t.Fatalf("ListGraphRecords = %+v", all)
	}
}

func testEvents(t *testing.T, s storage.Store) {
	var last int64
	types := []domain.EventType{domain.EvGraphStarted, domain.EvExecStarted, domain.EvExecCompleted}
	for i := 0; i < 30; i++ {
		ev := domain.NewEvent(types[i%3], []string{"g1", "g2"}[i%2], map[string]int{"i": i})
		if i%5 == 0 {
			ev.WithNode("n1", 2)
		}
		ev.WithExecution("e").WithMessage("m").WithTrace("t")
		ev.Level = "info"
		must(t, s.AppendEvent(ctx, ev))
		if ev.Seq <= last {
			t.Fatalf("seq not increasing: %d after %d", ev.Seq, last)
		}
		last = ev.Seq
	}
	if err := s.AppendEvent(ctx, &domain.Event{GraphID: "g"}); err == nil {
		t.Fatal("event without type must fail")
	}
	noID := &domain.Event{Type: domain.EvLog}
	must(t, s.AppendEvent(ctx, noID))
	if noID.ID == "" || noID.Time.IsZero() || noID.Seq <= last {
		t.Fatalf("AppendEvent must fill id/time/seq: %+v", noID)
	}

	asc, err := s.ListEvents(ctx, storage.EventFilter{Ascending: true, Limit: 1000})
	must(t, err)
	if len(asc) != 31 {
		t.Fatalf("all events = %d, want 31", len(asc))
	}
	for i := 1; i < len(asc); i++ {
		if asc[i].Seq <= asc[i-1].Seq {
			t.Fatal("ascending order broken")
		}
	}
	first := asc[0]
	var data map[string]int
	must(t, json.Unmarshal(first.Data, &data))
	if data["i"] != 0 || first.NodeID != "n1" || first.Replica != 2 || first.ExecutionID != "e" ||
		first.MessageID != "m" || first.TraceID != "t" || first.Level != "info" || first.Actor != "system" || first.Time.IsZero() {
		t.Fatalf("event not round-tripped: %+v data=%s", first, first.Data)
	}

	desc, err := s.ListEvents(ctx, storage.EventFilter{Limit: 5})
	must(t, err)
	if len(desc) != 5 || desc[0].Seq != asc[len(asc)-1].Seq || desc[0].Seq < desc[1].Seq {
		t.Fatalf("default order must be newest first with limit, got %d events", len(desc))
	}

	older, err := s.ListEvents(ctx, storage.EventFilter{BeforeSeq: desc[4].Seq, Limit: 3})
	must(t, err)
	if len(older) != 3 || older[0].Seq != asc[25].Seq || older[2].Seq != asc[23].Seq {
		t.Fatal("exclusive before cursor broken", older)
	}
	bounded, err := s.ListEvents(ctx, storage.EventFilter{BeforeSeq: asc[10].Seq, AfterSeq: asc[7].Seq, Ascending: true})
	must(t, err)
	if len(bounded) != 2 || bounded[0].Seq != asc[8].Seq {
		t.Fatal("combined sequence bounds broken", bounded)
	}
	g1, err := s.ListEvents(ctx, storage.EventFilter{GraphID: "g1", Ascending: true})
	must(t, err)
	if len(g1) != 15 {
		t.Fatalf("graph filter = %d, want 15", len(g1))
	}
	node, err := s.ListEvents(ctx, storage.EventFilter{NodeID: "n1", Ascending: true})
	must(t, err)
	if len(node) != 6 {
		t.Fatalf("node filter = %d, want 6", len(node))
	}
	typed, err := s.ListEvents(ctx, storage.EventFilter{Types: []domain.EventType{domain.EvExecStarted, domain.EvExecCompleted}, Ascending: true})
	must(t, err)
	if len(typed) != 20 {
		t.Fatalf("type filter = %d, want 20", len(typed))
	}
	after, err := s.ListEvents(ctx, storage.EventFilter{AfterSeq: asc[24].Seq, Ascending: true})
	must(t, err)
	if len(after) != 6 || after[0].Seq != asc[25].Seq {
		t.Fatalf("after filter = %d", len(after))
	}
	page, err := s.ListEvents(ctx, storage.EventFilter{AfterSeq: asc[4].Seq, Ascending: true, Limit: 3})
	must(t, err)
	if len(page) != 3 || page[0].Seq != asc[5].Seq || page[2].Seq != asc[7].Seq {
		t.Fatalf("paging after+limit broken")
	}
	combo, err := s.ListEvents(ctx, storage.EventFilter{GraphID: "g2", Types: []domain.EventType{domain.EvGraphStarted}, Ascending: true})
	must(t, err)
	for _, e := range combo {
		if e.GraphID != "g2" || e.Type != domain.EvGraphStarted {
			t.Fatalf("combined filter returned %+v", e)
		}
	}
	if len(combo) != 5 {
		t.Fatalf("combined filter = %d, want 5", len(combo))
	}
}

func msgRecord(t *testing.T, id, graph, channel, group, src, tgt, corr string, status domain.MessageStatus, at time.Time) *domain.MessageRecord {
	m, err := domain.NewMessage("task.ready", src, tgt, map[string]any{"title": id})
	must(t, err)
	m.ID = id
	m.SetMeta(domain.MetaCorrelationID, corr)
	return &domain.MessageRecord{Message: *m, GraphID: graph, Channel: channel, Group: group, Status: status, CreatedAt: at, UpdatedAt: at}
}

func testMessages(t *testing.T, s storage.Store) {
	base := time.Now().UTC().Truncate(time.Millisecond).Add(-time.Hour)
	for i := 0; i < 10; i++ {
		ch := []string{"tasks", "reviews"}[i%2]
		r := msgRecord(t, fmt.Sprintf("m%02d", i), "g", ch, "", "planner", "dev", []string{"c1", "c2"}[i/5], domain.MsgPublished, base.Add(time.Duration(i)*time.Minute))
		must(t, s.RecordMessage(ctx, r))
	}
	must(t, s.RecordMessage(ctx, msgRecord(t, "other", "g2", "tasks", "", "x", "y", "c9", domain.MsgPublished, base)))

	// upsert the same (id, channel, group): status changes, created_at stays
	up := msgRecord(t, "m00", "g", "tasks", "", "planner", "dev", "c1", domain.MsgAcked, base.Add(time.Hour))
	up.Attempt, up.Consumer, up.Error = 2, "dev#0", "boom"
	up.CreatedAt = base.Add(5 * time.Hour)
	must(t, s.RecordMessage(ctx, up))
	// a second group row for the same message id
	must(t, s.RecordMessage(ctx, msgRecord(t, "m00", "g", "tasks", "tasks.qa", "planner", "dev", "c1", domain.MsgDelivered, base)))

	recs, err := s.GetMessage(ctx, "m00")
	must(t, err)
	if len(recs) != 2 {
		t.Fatalf("GetMessage rows = %d, want 2", len(recs))
	}
	var acked *domain.MessageRecord
	for _, r := range recs {
		if r.Group == "" {
			acked = r
		}
	}
	if acked == nil || acked.Status != domain.MsgAcked || acked.Attempt != 2 || acked.Consumer != "dev#0" || acked.Error != "boom" {
		t.Fatalf("upsert not applied: %+v", acked)
	}
	if !acked.CreatedAt.Equal(base) {
		t.Fatalf("upsert must keep created_at: %v != %v", acked.CreatedAt, base)
	}
	if acked.Message.Type != "task.ready" || acked.Message.Source != "planner" || acked.Message.PayloadMap()["title"] != "m00" {
		t.Fatalf("envelope not round-tripped: %+v", acked.Message)
	}
	if _, err := s.GetMessage(ctx, "nope"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("GetMessage missing: want ErrNotFound, got %v", err)
	}

	all, err := s.ListMessages(ctx, storage.MessageFilter{GraphID: "g"})
	must(t, err)
	if len(all) != 11 {
		t.Fatalf("graph messages = %d, want 11", len(all))
	}
	for i := 1; i < len(all); i++ {
		if all[i].CreatedAt.After(all[i-1].CreatedAt) {
			t.Fatal("ListMessages must be newest first")
		}
	}
	if all[0].Message.ID != "m09" {
		t.Fatalf("newest = %s, want m09", all[0].Message.ID)
	}
	tasks, err := s.ListMessages(ctx, storage.MessageFilter{GraphID: "g", Channel: "tasks"})
	must(t, err)
	if len(tasks) != 6 {
		t.Fatalf("channel filter = %d, want 6", len(tasks))
	}
	byNode, err := s.ListMessages(ctx, storage.MessageFilter{NodeID: "dev"})
	must(t, err)
	if len(byNode) != 11 {
		t.Fatalf("node (target) filter = %d, want 11", len(byNode))
	}
	bySrc, err := s.ListMessages(ctx, storage.MessageFilter{NodeID: "x"})
	must(t, err)
	if len(bySrc) != 1 {
		t.Fatalf("node (source) filter = %d, want 1", len(bySrc))
	}
	corr, err := s.ListMessages(ctx, storage.MessageFilter{CorrelationID: "c2"})
	must(t, err)
	if len(corr) != 5 {
		t.Fatalf("correlation filter = %d, want 5", len(corr))
	}
	st, err := s.ListMessages(ctx, storage.MessageFilter{GraphID: "g", Status: domain.MsgAcked})
	must(t, err)
	if len(st) != 1 || st[0].Message.ID != "m00" {
		t.Fatalf("status filter = %d", len(st))
	}
	before, err := s.ListMessages(ctx, storage.MessageFilter{GraphID: "g", Before: base.Add(3 * time.Minute)})
	must(t, err)
	if len(before) != 4 { // m00 (two groups), m01, m02
		t.Fatalf("before filter = %d, want 4", len(before))
	}
	lim, err := s.ListMessages(ctx, storage.MessageFilter{GraphID: "g", Limit: 3})
	must(t, err)
	if len(lim) != 3 || lim[0].Message.ID != "m09" {
		t.Fatalf("limit = %d", len(lim))
	}
	if err := s.RecordMessage(ctx, &domain.MessageRecord{Channel: "x"}); err == nil {
		t.Fatal("RecordMessage without id must fail")
	}
}

func testMarkProcessed(t *testing.T, s storage.Store) {
	if seen, err := s.IsProcessed(ctx, "grp", "m1"); err != nil || seen {
		t.Fatalf("IsProcessed before marking: %v %v", seen, err)
	}
	ok, err := s.MarkProcessed(ctx, "grp", "m1")
	must(t, err)
	if !ok {
		t.Fatal("first MarkProcessed must return true")
	}
	if seen, err := s.IsProcessed(ctx, "grp", "m1"); err != nil || !seen {
		t.Fatalf("IsProcessed after marking: %v %v", seen, err)
	}
	if seen, _ := s.IsProcessed(ctx, "other", "m1"); seen {
		t.Fatal("processed is per group")
	}
	ok, err = s.MarkProcessed(ctx, "grp", "m1")
	must(t, err)
	if ok {
		t.Fatal("duplicate MarkProcessed must return false")
	}
	ok, err = s.MarkProcessed(ctx, "other", "m1")
	must(t, err)
	if !ok {
		t.Fatal("same message in another group is not a duplicate")
	}
	var wins int32
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := s.MarkProcessed(ctx, "race", "m")
			if err != nil {
				t.Error(err)
			}
			if ok {
				atomic.AddInt32(&wins, 1)
			}
		}()
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("concurrent MarkProcessed winners = %d, want 1", wins)
	}
}

func testExecutions(t *testing.T, s storage.Store) {
	defer testExecutionParents(t, s)
	if _, err := s.GetExecution(ctx, "x"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	base := time.Now().UTC().Truncate(time.Millisecond)
	for i := 0; i < 6; i++ {
		e := &domain.Execution{
			ID: fmt.Sprintf("e%d", i), GraphID: "g", NodeID: []string{"a", "b"}[i%2], Replica: i % 3,
			Iteration: i, Status: domain.ExecRunning, InputIDs: []string{"m1"}, StartedAt: base.Add(time.Duration(i) * time.Second),
		}
		must(t, s.PutExecution(ctx, e))
	}
	must(t, s.PutExecution(ctx, &domain.Execution{ID: "other", GraphID: "g2", NodeID: "a", Status: domain.ExecRunning, StartedAt: base}))
	done := base.Add(time.Minute)
	e0 := &domain.Execution{ID: "e0", GraphID: "g", NodeID: "a", Status: domain.ExecSucceeded, StartedAt: base,
		Usage: domain.Usage{InputTokens: 10, OutputTokens: 5, CacheReadTokens: 4, CacheWriteTokens: 2}, CompletedAt: &done, OutputIDs: []string{"o1"}, Summary: "ok"}
	must(t, s.PutExecution(ctx, e0))
	got, err := s.GetExecution(ctx, "e0")
	must(t, err)
	if got.Status != domain.ExecSucceeded || got.Usage != (domain.Usage{InputTokens: 10, OutputTokens: 5, CacheReadTokens: 4, CacheWriteTokens: 2}) || got.CompletedAt == nil || !got.CompletedAt.Equal(done) || got.Summary != "ok" {
		t.Fatalf("execution = %+v", got)
	}
	list, err := s.ListExecutions(ctx, storage.ExecutionFilter{GraphID: "g"})
	must(t, err)
	if len(list) != 6 || list[0].ID != "e5" {
		t.Fatalf("list = %d, first %s (want 6, e5 newest first)", len(list), list[0].ID)
	}
	byNode, err := s.ListExecutions(ctx, storage.ExecutionFilter{GraphID: "g", NodeID: "b"})
	must(t, err)
	if len(byNode) != 3 {
		t.Fatalf("node filter = %d", len(byNode))
	}
	byStatus, err := s.ListExecutions(ctx, storage.ExecutionFilter{Status: domain.ExecRunning})
	must(t, err)
	if len(byStatus) != 6 { // e1..e5 + other
		t.Fatalf("status filter = %d, want 6", len(byStatus))
	}
	lim, err := s.ListExecutions(ctx, storage.ExecutionFilter{GraphID: "g", Limit: 2})
	must(t, err)
	if len(lim) != 2 {
		t.Fatalf("limit = %d", len(lim))
	}
}

func testExecutionParents(t *testing.T, s storage.Store) {
	for _, id := range []string{"parent-one", "parent-two"} {
		must(t, s.PutExecution(ctx, &domain.Execution{ID: id, GraphID: "tree", ParentExecutionID: "root", StartedAt: time.Now()}))
	}
	list, err := s.ListExecutions(ctx, storage.ExecutionFilter{ParentExecutionID: "root"})
	must(t, err)
	if len(list) != 2 {
		t.Fatalf("parent filter: %+v", list)
	}
	list[0].ParentExecutionID = "other-root"
	must(t, s.PutExecution(ctx, list[0]))
	list, err = s.ListExecutions(ctx, storage.ExecutionFilter{ParentExecutionID: "root"})
	must(t, err)
	if len(list) != 1 {
		t.Fatal("parent index was not updated")
	}
}
func testApprovals(t *testing.T, s storage.Store) {
	if _, err := s.GetApproval(ctx, "x"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	base := time.Now().UTC().Truncate(time.Millisecond)
	for i := 0; i < 4; i++ {
		must(t, s.PutApproval(ctx, &domain.Approval{
			ID: fmt.Sprintf("a%d", i), GraphID: []string{"g", "g2"}[i%2], NodeID: "n", Kind: domain.ApprovalTool,
			Status: domain.ApprovalPending, Title: "run rm", Detail: json.RawMessage(`{"tool":"shell.exec"}`),
			CreatedAt: base.Add(time.Duration(i) * time.Second),
		}))
	}
	a, err := s.DecideApproval(ctx, "a0", domain.ApprovalApproved, "alice", "lgtm", "yes")
	must(t, err)
	if a.Status != domain.ApprovalApproved || a.DecidedBy != "alice" || a.Comment != "lgtm" || a.Answer != "yes" || a.DecidedAt == nil {
		t.Fatalf("decided = %+v", a)
	}
	got, err := s.GetApproval(ctx, "a0")
	must(t, err)
	if got.Status != domain.ApprovalApproved || got.DecidedBy != "alice" {
		t.Fatalf("persisted = %+v", got)
	}
	var detail map[string]string
	must(t, json.Unmarshal(got.Detail, &detail))
	if detail["tool"] != "shell.exec" {
		t.Fatalf("detail = %s", got.Detail)
	}
	if _, err := s.DecideApproval(ctx, "a0", domain.ApprovalRejected, "bob", "", ""); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("deciding twice: want ErrConflict, got %v", err)
	}
	if _, err := s.DecideApproval(ctx, "zzz", domain.ApprovalRejected, "bob", "", ""); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("deciding missing: want ErrNotFound, got %v", err)
	}
	pending, err := s.ListApprovals(ctx, storage.ApprovalFilter{Status: domain.ApprovalPending})
	must(t, err)
	if len(pending) != 3 {
		t.Fatalf("pending = %d, want 3", len(pending))
	}
	g, err := s.ListApprovals(ctx, storage.ApprovalFilter{GraphID: "g"})
	must(t, err)
	if len(g) != 2 || g[0].ID != "a2" {
		t.Fatalf("graph filter = %d (want 2, newest a2 first)", len(g))
	}
	gp, err := s.ListApprovals(ctx, storage.ApprovalFilter{GraphID: "g", Status: domain.ApprovalPending})
	must(t, err)
	if len(gp) != 1 || gp[0].ID != "a2" {
		t.Fatalf("graph+status filter = %+v", gp)
	}
}

func testApprovalRace(t *testing.T, s storage.Store) {
	must(t, s.PutApproval(ctx, &domain.Approval{ID: "r", GraphID: "g", Kind: domain.ApprovalMessage, Status: domain.ApprovalPending, Title: "t"}))
	const n = 10
	var wins, conflicts int32
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			status := domain.ApprovalApproved
			if i%2 == 1 {
				status = domain.ApprovalRejected
			}
			_, err := s.DecideApproval(ctx, "r", status, fmt.Sprintf("user%d", i), "", "")
			switch {
			case err == nil:
				atomic.AddInt32(&wins, 1)
			case errors.Is(err, storage.ErrConflict):
				atomic.AddInt32(&conflicts, 1)
			default:
				t.Errorf("decider %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
	if wins != 1 || conflicts != n-1 {
		t.Fatalf("concurrent decisions: %d wins, %d conflicts", wins, conflicts)
	}
}

func testOperatorMessages(t *testing.T, s storage.Store) {
	base := time.Now().UTC().Truncate(time.Millisecond)
	put := func(id string, replica int, at time.Duration) {
		must(t, s.PutOperatorMessage(ctx, &domain.OperatorMessage{
			ID: id, GraphID: "g", NodeID: "dev", Replica: replica, Text: "hi " + id, Author: "op", CreatedAt: base.Add(at),
		}))
	}
	put("any1", -1, 1*time.Second)
	put("r0", 0, 2*time.Second)
	put("r1", 1, 3*time.Second)
	put("any2", -1, 4*time.Second)
	must(t, s.PutOperatorMessage(ctx, &domain.OperatorMessage{ID: "elsewhere", GraphID: "g", NodeID: "qa", Replica: -1, Text: "x", CreatedAt: base}))

	got1, err := s.ConsumeOperatorMessages(ctx, "g", "dev", 1)
	must(t, err)
	ids := func(ms []*domain.OperatorMessage) []string {
		out := []string{}
		for _, m := range ms {
			out = append(out, m.ID)
			if m.Consumed == nil {
				t.Fatalf("consumed message %s has no Consumed time", m.ID)
			}
		}
		return out
	}
	if fmt.Sprint(ids(got1)) != "[any1 r1 any2]" {
		t.Fatalf("replica 1 consumed %v, want [any1 r1 any2] in order", ids(got1))
	}
	got0, err := s.ConsumeOperatorMessages(ctx, "g", "dev", 0)
	must(t, err)
	if fmt.Sprint(ids(got0)) != "[r0]" {
		t.Fatalf("replica 0 consumed %v, want [r0]", ids(got0))
	}
	again, err := s.ConsumeOperatorMessages(ctx, "g", "dev", 0)
	must(t, err)
	if len(again) != 0 {
		t.Fatalf("messages consumed twice: %v", ids(again))
	}

	// Many replicas racing for one any-replica message: exactly one gets it.
	put("race", -1, 5*time.Second)
	var winners int32
	var wg sync.WaitGroup
	for r := 0; r < 8; r++ {
		wg.Add(1)
		go func(r int) {
			defer wg.Done()
			ms, err := s.ConsumeOperatorMessages(ctx, "g", "dev", r)
			if err != nil {
				t.Error(err)
				return
			}
			atomic.AddInt32(&winners, int32(len(ms)))
		}(r)
	}
	wg.Wait()
	if winners != 1 {
		t.Fatalf("any-replica message delivered %d times, want 1", winners)
	}

	hist, err := s.ListOperatorMessages(ctx, "g", "dev", 0)
	must(t, err)
	if len(hist) != 5 || hist[0].ID != "any1" || hist[4].ID != "race" {
		t.Fatalf("history = %v (want 5, chronological)", ids(hist))
	}
	last2, err := s.ListOperatorMessages(ctx, "g", "dev", 2)
	must(t, err)
	if len(last2) != 2 || last2[0].ID != "any2" || last2[1].ID != "race" {
		t.Fatalf("limited history must be the latest messages in chronological order: %v", ids(last2))
	}
	allNodes, err := s.ListOperatorMessages(ctx, "g", "", 0)
	must(t, err)
	if len(allNodes) != 6 {
		t.Fatalf("all-node history = %d, want 6", len(allNodes))
	}
}

func testReplicaStatus(t *testing.T, s storage.Store) {
	put := func(node string, r int, st domain.ReplicaState) {
		must(t, s.PutReplicaStatus(ctx, &domain.ReplicaStatus{GraphID: "g", NodeID: node, Replica: r, State: st, Iteration: r}))
	}
	put("b", 1, domain.ReplicaIdle)
	put("b", 0, domain.ReplicaRunning)
	put("a", 0, domain.ReplicaStarting)
	put("a", 0, domain.ReplicaRunning) // update
	must(t, s.PutReplicaStatus(ctx, &domain.ReplicaStatus{GraphID: "g2", NodeID: "a", State: domain.ReplicaIdle}))
	list, err := s.ListReplicaStatus(ctx, "g")
	must(t, err)
	if len(list) != 3 {
		t.Fatalf("statuses = %d, want 3", len(list))
	}
	if list[0].NodeID != "a" || list[0].State != domain.ReplicaRunning || list[1].NodeID != "b" || list[1].Replica != 0 || list[2].Replica != 1 {
		t.Fatalf("order/state wrong: %+v %+v %+v", list[0], list[1], list[2])
	}
	if list[0].UpdatedAt.IsZero() {
		t.Fatal("UpdatedAt not set")
	}
	must(t, s.DeleteReplicaStatus(ctx, "g", "b", 1))
	list, err = s.ListReplicaStatus(ctx, "g")
	must(t, err)
	if len(list) != 2 {
		t.Fatalf("after delete = %d, want 2", len(list))
	}
	must(t, s.DeleteReplicaStatus(ctx, "g", "zzz", 9)) // deleting a missing status is not an error
}

func testKV(t *testing.T, s storage.Store) {
	if _, err := s.KVGet(ctx, "g/n", "k"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	must(t, s.KVPut(ctx, "g/n", "notes/1", []byte("one")))
	must(t, s.KVPut(ctx, "g/n", "notes/2", []byte{0, 1, 2, 255}))
	must(t, s.KVPut(ctx, "g/n", "Notes/3", []byte("case")))
	must(t, s.KVPut(ctx, "g/n", "other", []byte("x")))
	must(t, s.KVPut(ctx, "g/n#1", "notes/1", []byte("private")))
	must(t, s.KVPut(ctx, "g/n", "empty", []byte{}))
	must(t, s.KVPut(ctx, "g/n", "notes/1", []byte("uno"))) // overwrite

	v, err := s.KVGet(ctx, "g/n", "notes/1")
	must(t, err)
	if string(v) != "uno" {
		t.Fatalf("KVGet = %q", v)
	}
	v, err = s.KVGet(ctx, "g/n", "notes/2")
	must(t, err)
	if fmt.Sprint(v) != "[0 1 2 255]" {
		t.Fatalf("binary value = %v", v)
	}
	v, err = s.KVGet(ctx, "g/n", "empty")
	must(t, err)
	if v == nil || len(v) != 0 {
		t.Fatalf("empty value must be a non-nil empty slice, got %v", v)
	}
	m, err := s.KVList(ctx, "g/n", "notes/")
	must(t, err)
	if len(m) != 2 || string(m["notes/1"]) != "uno" {
		t.Fatalf("prefix list (case-sensitive) = %v", m)
	}
	all, err := s.KVList(ctx, "g/n", "")
	must(t, err)
	if len(all) != 5 {
		t.Fatalf("full list = %d, want 5", len(all))
	}
	pct, err := s.KVList(ctx, "g/n", "%")
	must(t, err)
	if len(pct) != 0 {
		t.Fatalf("prefix must be literal, '%%' matched %d keys", len(pct))
	}
	must(t, s.KVDelete(ctx, "g/n", "notes/1"))
	if _, err := s.KVGet(ctx, "g/n", "notes/1"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("after delete: want ErrNotFound, got %v", err)
	}
	v, err = s.KVGet(ctx, "g/n#1", "notes/1")
	must(t, err)
	if string(v) != "private" {
		t.Fatal("namespaces must be isolated")
	}
	must(t, s.KVDelete(ctx, "g/n", "never-existed"))
}

func testLeases(t *testing.T, s storage.Store) {
	ok, err := s.AcquireLease(ctx, "g/n#0", "i1", time.Hour)
	must(t, err)
	if !ok {
		t.Fatal("acquire free lease failed")
	}
	ok, err = s.AcquireLease(ctx, "g/n#0", "i1", time.Hour)
	must(t, err)
	if !ok {
		t.Fatal("renew by holder failed")
	}
	ok, err = s.AcquireLease(ctx, "g/n#0", "i2", time.Hour)
	must(t, err)
	if ok {
		t.Fatal("another holder acquired a live lease")
	}
	must(t, s.ReleaseLease(ctx, "g/n#0", "i2")) // not the holder: no effect
	ok, err = s.AcquireLease(ctx, "g/n#0", "i2", time.Hour)
	must(t, err)
	if ok {
		t.Fatal("release by non-holder must not free the lease")
	}
	must(t, s.ReleaseLease(ctx, "g/n#0", "i1"))
	ok, err = s.AcquireLease(ctx, "g/n#0", "i2", time.Hour)
	must(t, err)
	if !ok {
		t.Fatal("acquire after release failed")
	}

	// expiry
	ok, err = s.AcquireLease(ctx, "g/n#1", "i1", 150*time.Millisecond)
	must(t, err)
	if !ok {
		t.Fatal("acquire short lease failed")
	}
	ok, err = s.AcquireLease(ctx, "g/n#1", "i2", time.Hour)
	must(t, err)
	if ok {
		t.Fatal("acquired before expiry")
	}
	time.Sleep(300 * time.Millisecond)
	ok, err = s.AcquireLease(ctx, "g/n#1", "i2", time.Hour)
	must(t, err)
	if !ok {
		t.Fatal("expired lease not taken over")
	}

	_, err = s.AcquireLease(ctx, "h/x#0", "i3", time.Hour)
	must(t, err)
	ls, err := s.ListLeases(ctx, "g/")
	must(t, err)
	if len(ls) != 2 {
		t.Fatalf("ListLeases(g/) = %d, want 2", len(ls))
	}
	for _, l := range ls {
		if l.Holder != "i2" || l.ExpiresAt.Before(time.Now()) {
			t.Fatalf("lease = %+v", l)
		}
	}
	all, err := s.ListLeases(ctx, "")
	must(t, err)
	if len(all) != 3 {
		t.Fatalf("ListLeases() = %d, want 3", len(all))
	}
}

func testLeaseRace(t *testing.T, s storage.Store) {
	const n = 12
	var wins int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			ok, err := s.AcquireLease(ctx, "race", fmt.Sprintf("holder-%d", i), time.Minute)
			if err != nil {
				t.Error(err)
				return
			}
			if ok {
				atomic.AddInt32(&wins, 1)
			}
		}(i)
	}
	close(start)
	wg.Wait()
	if wins != 1 {
		t.Fatalf("concurrent AcquireLease winners = %d, want 1", wins)
	}
}

func testSettings(t *testing.T, s storage.Store) {
	if _, err := s.GetSetting(ctx, "llm/providers"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("missing setting: want ErrNotFound, got %v", err)
	}
	if err := s.DeleteSetting(ctx, "llm/providers", 1); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("delete missing: want ErrNotFound, got %v", err)
	}
	if _, err := s.PutSetting(ctx, "llm/providers", []byte("x"), 3, "alice"); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("update of a missing key must conflict, got %v", err)
	}
	rev, err := s.PutSetting(ctx, "llm/providers", []byte{0, 1, 255}, 0, "alice")
	must(t, err)
	if rev != 1 {
		t.Fatalf("first revision = %d, want 1", rev)
	}
	if _, err := s.PutSetting(ctx, "llm/providers", []byte("again"), 0, "bob"); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("create of an existing key must conflict, got %v", err)
	}
	st, err := s.GetSetting(ctx, "llm/providers")
	must(t, err)
	if fmt.Sprint(st.Value) != "[0 1 255]" || st.Revision != 1 || st.UpdatedBy != "alice" || st.UpdatedAt.IsZero() {
		t.Fatalf("GetSetting = %+v", st)
	}
	st.Value[0] = 9 // callers must not be able to mutate stored state
	rev, err = s.PutSetting(ctx, "llm/providers", []byte("two"), 1, "bob")
	must(t, err)
	if rev != 2 {
		t.Fatalf("second revision = %d", rev)
	}
	if _, err := s.PutSetting(ctx, "llm/providers", []byte("stale"), 1, "carol"); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("stale revision must conflict, got %v", err)
	}
	st, err = s.GetSetting(ctx, "llm/providers")
	must(t, err)
	if string(st.Value) != "two" || st.Revision != 2 || st.UpdatedBy != "bob" {
		t.Fatalf("after update = %+v", st)
	}
	_, err = s.PutSetting(ctx, "mcp/connections/b", []byte("b"), 0, "")
	must(t, err)
	_, err = s.PutSetting(ctx, "mcp/connections/a", []byte{}, 0, "")
	must(t, err)
	_, err = s.PutSetting(ctx, "MCP/other", []byte("case"), 0, "")
	must(t, err)
	list, err := s.ListSettings(ctx, "mcp/")
	must(t, err)
	if len(list) != 2 || list[0].Key != "mcp/connections/a" || list[1].Key != "mcp/connections/b" {
		t.Fatalf("prefix list (ordered, case-sensitive) = %+v", list)
	}
	if list[0].Value == nil || len(list[0].Value) != 0 {
		t.Fatalf("empty value must be a non-nil empty slice, got %v", list[0].Value)
	}
	if pct, err := s.ListSettings(ctx, "%"); err != nil || len(pct) != 0 {
		t.Fatalf("prefix must be literal: %v %v", pct, err)
	}
	all, err := s.ListSettings(ctx, "")
	must(t, err)
	if len(all) != 4 {
		t.Fatalf("full list = %d, want 4", len(all))
	}
	if err := s.DeleteSetting(ctx, "llm/providers", 1); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("delete with stale revision: want ErrConflict, got %v", err)
	}
	must(t, s.DeleteSetting(ctx, "llm/providers", 2))
	if _, err := s.GetSetting(ctx, "llm/providers"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("after delete: want ErrNotFound, got %v", err)
	}
	// A deleted key can be created again from revision 0.
	rev, err = s.PutSetting(ctx, "llm/providers", []byte("fresh"), 0, "")
	must(t, err)
	if rev != 1 {
		t.Fatalf("recreated revision = %d", rev)
	}
}

func testSettingsRace(t *testing.T, s storage.Store) {
	_, err := s.PutSetting(ctx, "race", []byte("0"), 0, "")
	must(t, err)
	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := s.PutSetting(ctx, "race", []byte(fmt.Sprint(i)), 1, fmt.Sprint(i))
			if err == nil {
				wins.Add(1)
			} else if !errors.Is(err, storage.ErrConflict) {
				t.Errorf("writer %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("%d writers won the same revision, want exactly 1", wins.Load())
	}
	st, err := s.GetSetting(ctx, "race")
	must(t, err)
	if st.Revision != 2 {
		t.Fatalf("revision after race = %d", st.Revision)
	}
}

func testOperatorMessageOutbox(t *testing.T, s storage.Store) {
	ctx := context.Background()
	m := &domain.OperatorMessage{ID: "outbox", GraphID: "g", NodeID: "n", Replica: 0, Text: "decision"}
	if err := s.PutOperatorMessageOnce(ctx, nil); err == nil {
		t.Fatal("nil message accepted")
	}
	must(t, s.PutOperatorMessageOnce(ctx, m))
	must(t, s.PutOperatorMessageOnce(ctx, m))
	got, err := s.ConsumeOperatorMessages(ctx, "g", "n", 0)
	must(t, err)
	if len(got) != 1 {
		t.Fatal(got)
	}
	must(t, s.PutOperatorMessageOnce(ctx, m))
	got, err = s.ConsumeOperatorMessages(ctx, "g", "n", 0)
	must(t, err)
	if len(got) != 0 {
		t.Fatal("consumed continuation resurrected")
	}
}

func testGraphObservedState(t *testing.T, s storage.Store) {
	original := &domain.GraphRecord{ID: "observed", Version: 1, DesiredState: domain.GraphRunning, State: domain.GraphStopped, RunID: "run1"}
	must(t, s.PutGraphRecord(ctx, original))
	snapshot, err := s.GetGraphRecord(ctx, original.ID)
	must(t, err)
	must(t, s.UpdateGraphState(ctx, snapshot, domain.GraphRunning))
	got, err := s.GetGraphRecord(ctx, original.ID)
	must(t, err)
	if got.State != domain.GraphRunning || got.DesiredState != domain.GraphRunning || got.RunID != "run1" {
		t.Fatal(got)
	}
	if !errors.Is(s.UpdateGraphState(ctx, snapshot, domain.GraphFailed), storage.ErrConflict) {
		t.Fatal("stale observation accepted")
	}
	for _, desired := range []domain.GraphState{domain.GraphStopped, domain.GraphPaused, domain.GraphRunning} {
		before, err := s.GetGraphRecord(ctx, original.ID)
		must(t, err)
		command := *before
		command.DesiredState = desired
		command.RunID += "-new"
		command.Version++
		command.UpdatedAt = time.Now().UTC()
		must(t, s.PutGraphRecord(ctx, &command))
		if !errors.Is(s.UpdateGraphState(ctx, before, domain.GraphStopped), storage.ErrConflict) {
			t.Fatal("operator command overwritten")
		}
		got, err = s.GetGraphRecord(ctx, original.ID)
		must(t, err)
		if got.DesiredState != desired || got.RunID != command.RunID || got.Version != command.Version {
			t.Fatal(got)
		}
	}
	if s.UpdateGraphState(ctx, nil, domain.GraphStopped) == nil {
		t.Fatal("nil snapshot accepted")
	}
}
