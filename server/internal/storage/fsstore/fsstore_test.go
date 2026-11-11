package fsstore_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/storage/conformance"
	"github.com/woliveiras/weft/server/internal/storage/fsstore"
)

func TestConformance(t *testing.T) {
	conformance.Run(t, func(t *testing.T) storage.Store {
		s, err := fsstore.Open(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		return s
	})
}

func open(t *testing.T, dir string) *fsstore.Store {
	t.Helper()
	s, err := fsstore.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s
}

// Everything written must be there after a reopen.
func TestReopen(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := open(t, dir)
	g := &domain.Graph{APIVersion: domain.APIVersion, Kind: "Graph", Metadata: domain.GraphMetadata{ID: "Graph/One"}}
	if _, err := s.SaveGraph(ctx, g, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveGraph(ctx, g, 1); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := s.AppendEvent(ctx, domain.NewEvent(domain.EvLog, "g", nil)); err != nil {
			t.Fatal(err)
		}
	}
	m, _ := domain.NewMessage("t", "a", "b", nil)
	if err := s.RecordMessage(ctx, &domain.MessageRecord{Message: *m, GraphID: "g", Channel: "c", Status: domain.MsgPublished}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MarkProcessed(ctx, "grp", m.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.KVPut(ctx, "g/n", "A/b", []byte("v")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AcquireLease(ctx, "g/n#0", "i1", time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := s.PutOperatorMessage(ctx, &domain.OperatorMessage{ID: "o1", GraphID: "g", NodeID: "n", Replica: -1, Text: "hi"}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutReplicaStatus(ctx, &domain.ReplicaStatus{GraphID: "g", NodeID: "n", Replica: 1, State: domain.ReplicaIdle}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PutSetting(ctx, "llm/providers", []byte("cfg"), 0, "admin"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	s = open(t, dir)
	defer s.Close()
	got, err := s.GetGraph(ctx, "Graph/One", 0)
	if err != nil || got.Metadata.Version != 2 {
		t.Fatalf("graph after reopen: %v %+v", err, got)
	}
	vs, _ := s.ListGraphVersions(ctx, "Graph/One")
	if len(vs) != 2 {
		t.Fatalf("versions after reopen = %d", len(vs))
	}
	evs, _ := s.ListEvents(ctx, storage.EventFilter{Ascending: true})
	if len(evs) != 3 || evs[2].Seq != 3 {
		t.Fatalf("events after reopen = %d", len(evs))
	}
	ev := domain.NewEvent(domain.EvLog, "g", nil)
	if err := s.AppendEvent(ctx, ev); err != nil || ev.Seq != 4 {
		t.Fatalf("seq must continue after reopen: %v seq=%d", err, ev.Seq)
	}
	if ok, _ := s.MarkProcessed(ctx, "grp", m.ID); ok {
		t.Fatal("processed set lost on reopen")
	}
	if _, err := s.GetMessage(ctx, m.ID); err != nil {
		t.Fatal(err)
	}
	if v, err := s.KVGet(ctx, "g/n", "A/b"); err != nil || string(v) != "v" {
		t.Fatalf("kv after reopen: %v %q", err, v)
	}
	if ok, _ := s.AcquireLease(ctx, "g/n#0", "i2", time.Hour); ok {
		t.Fatal("lease lost on reopen")
	}
	if ms, _ := s.ConsumeOperatorMessages(ctx, "g", "n", 3); len(ms) != 1 {
		t.Fatal("operator message lost on reopen")
	}
	if rs, _ := s.ListReplicaStatus(ctx, "g"); len(rs) != 1 || rs[0].Replica != 1 {
		t.Fatal("replica status lost on reopen")
	}
	if st, err := s.GetSetting(ctx, "llm/providers"); err != nil || string(st.Value) != "cfg" || st.Revision != 1 || st.UpdatedBy != "admin" {
		t.Fatalf("setting after reopen: %v %+v", err, st)
	}
	if _, err := s.PutSetting(ctx, "llm/providers", []byte("v2"), 1, ""); err != nil {
		t.Fatalf("revision must continue after reopen: %v", err)
	}
}

// A crash mid-append leaves a partial last line; Open must drop it and keep
// everything before it.
func TestTornEventTail(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := open(t, dir)
	for i := 0; i < 2; i++ {
		if err := s.AppendEvent(ctx, domain.NewEvent(domain.EvLog, "g", nil)); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()
	seg := filepath.Join(dir, "events", "events-000001.jsonl")
	f, err := os.OpenFile(seg, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(`{"seq":3,"id":"half","ty`)
	f.Close()
	// also a leftover temp file from an interrupted atomic write
	os.WriteFile(filepath.Join(dir, "records", ".tmp-123"), []byte("junk"), 0o644)

	s = open(t, dir)
	defer s.Close()
	evs, _ := s.ListEvents(ctx, storage.EventFilter{Ascending: true})
	if len(evs) != 2 {
		t.Fatalf("events after torn tail = %d, want 2", len(evs))
	}
	ev := domain.NewEvent(domain.EvLog, "g", nil)
	if err := s.AppendEvent(ctx, ev); err != nil || ev.Seq != 3 {
		t.Fatalf("append after recovery: %v seq=%d", err, ev.Seq)
	}
	if _, err := os.Stat(filepath.Join(dir, "records", ".tmp-123")); !os.IsNotExist(err) {
		t.Fatal("temp file not cleaned")
	}
	s.Close()
	s2 := open(t, dir)
	defer s2.Close()
	evs, _ = s2.ListEvents(ctx, storage.EventFilter{Ascending: true})
	if len(evs) != 3 {
		t.Fatalf("events after second reopen = %d, want 3", len(evs))
	}
}

// Corruption that is not at the tail must not be silently dropped.
func TestCorruptEventMiddle(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := open(t, dir)
	s.AppendEvent(ctx, domain.NewEvent(domain.EvLog, "g", nil))
	s.Close()
	seg := filepath.Join(dir, "events", "events-000001.jsonl")
	b, _ := os.ReadFile(seg)
	os.WriteFile(seg, append([]byte("garbage\n"), b...), 0o644)
	if s, err := fsstore.Open(dir); err == nil {
		s.Close()
		t.Fatal("expected an error for corruption in the middle of the log")
	}
}

func TestExclusiveLock(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	if s2, err := fsstore.Open(dir); err == nil {
		s2.Close()
		t.Fatal("second Open of the same directory must fail")
	}
	s.Close()
	s3 := open(t, dir)
	s3.Close()
}
