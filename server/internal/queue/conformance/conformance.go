// Package conformance is the behavioural contract every queue.Broker must
// satisfy. Adapters run it from their own tests:
//
//	conformance.Run(t, func(t *testing.T) queue.Broker { return newBroker(t) })
//
// Network adapters may relax a few assertions through Options, each of
// which corresponds to a documented limitation of that transport.
package conformance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/queue"
)

// Options relax assertions for transports that cannot meet them.
type Options struct {
	// AckWait used by the lease-expiry test (default 1s).
	AckWait time.Duration
	// NoDeadStats: the adapter cannot count dead-lettered messages.
	NoDeadStats bool
	// NoInFlightStats: the adapter cannot report in-flight counts.
	NoInFlightStats bool
	// NoGroupsErrOnlyUndeclaredSubject: ErrNoGroups is only detected for
	// subjects that were never declared (not for subjects with zero groups).
	SkipNoGroups bool
	// Settle is how long to wait for broker-side state (consumer group
	// rebalances, offset commits) before asserting. Default 0.
	Settle time.Duration
	// FetchWait is the wait used when a message is expected (default 5s).
	FetchWait time.Duration
}

// Run executes the suite with default options.
func Run(t *testing.T, newBroker func(t *testing.T) queue.Broker) {
	RunWith(t, newBroker, Options{})
}

// RunWith executes the suite with relaxed options.
func RunWith(t *testing.T, newBroker func(t *testing.T) queue.Broker, o Options) {
	if o.AckWait == 0 {
		o.AckWait = time.Second
	}
	if o.FetchWait == 0 {
		o.FetchWait = 5 * time.Second
	}
	tests := []struct {
		name string
		fn   func(*testing.T, queue.Broker, Options)
	}{
		{"RoundTrip", testRoundTrip},
		{"CopyPerGroup", testCopyPerGroup},
		{"CompetingConsumers", testCompeting},
		{"AckRemoves", testAckRemoves},
		{"NackRedelivers", testNack},
		{"NackDelay", testNackDelay},
		{"LeaseExpiry", testLeaseExpiry},
		{"Term", testTerm},
		{"MaxDeliver", testMaxDeliver},
		{"FetchTimeout", testFetchTimeout},
		{"Stats", testStats},
		{"DeclareIdempotent", testDeclareIdempotent},
		{"NoGroups", testNoGroups},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := newBroker(t)
			t.Cleanup(func() { _ = b.Close() })
			tc.fn(t, b, o)
		})
	}
}

func ctx(t *testing.T) context.Context {
	c, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	return c
}

func uniq(prefix string) string {
	return fmt.Sprintf("%s%d%04d.ch", prefix, time.Now().UnixNano()%1e9, rand.Intn(10000))
}

func msg(t *testing.T, i int) *domain.Message {
	m, err := domain.NewMessage("test.item", "producer", "consumer", map[string]any{"i": i})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func declare(t *testing.T, b queue.Broker, subj string, o Options, groups ...string) {
	err := b.Declare(ctx(t), queue.SubjectConfig{Subject: subj, Groups: groups, AckWait: o.AckWait, MaxDeliver: 5})
	if err != nil {
		t.Fatalf("declare: %v", err)
	}
	if o.Settle > 0 {
		time.Sleep(o.Settle)
	}
}

func subscribe(t *testing.T, b queue.Broker, subj, grp, name string) queue.Subscription {
	s, err := b.Subscribe(ctx(t), subj, grp, name)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func publish(t *testing.T, b queue.Broker, subj string, m *domain.Message) {
	if err := b.Publish(ctx(t), subj, m); err != nil {
		t.Fatalf("publish: %v", err)
	}
}

// fetchN fetches until n deliveries arrive or wait elapses.
func fetchN(t *testing.T, s queue.Subscription, n int, wait time.Duration) []queue.Delivery {
	var out []queue.Delivery
	deadline := time.Now().Add(wait)
	for len(out) < n && time.Now().Before(deadline) {
		ds, err := s.Fetch(ctx(t), n-len(out), time.Until(deadline))
		if err != nil {
			t.Fatalf("fetch: %v", err)
		}
		out = append(out, ds...)
	}
	return out
}

func ackAll(t *testing.T, ds []queue.Delivery) {
	for _, d := range ds {
		if err := d.Ack(ctx(t)); err != nil {
			t.Fatalf("ack: %v", err)
		}
	}
}

func expectEmpty(t *testing.T, s queue.Subscription, wait time.Duration) {
	ds, err := s.Fetch(ctx(t), 10, wait)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(ds) != 0 {
		t.Fatalf("expected no deliveries, got %d (first id %s attempt %d)", len(ds), ds[0].Message().ID, ds[0].Attempt())
	}
}

func testRoundTrip(t *testing.T, b queue.Broker, o Options) {
	subj := uniq("rt")
	declare(t, b, subj, o, "g1")
	s := subscribe(t, b, subj, "g1", "c1")
	in := msg(t, 1)
	in.Metadata["custom"] = "value"
	in.Metadata["nested"] = map[string]any{"a": []any{1.0, "x"}}
	publish(t, b, subj, in)
	ds := fetchN(t, s, 1, o.FetchWait)
	if len(ds) != 1 {
		t.Fatalf("expected 1 delivery, got %d", len(ds))
	}
	out := ds[0].Message()
	if out.ID != in.ID || out.Type != in.Type || out.Source != in.Source || out.Target != in.Target {
		t.Fatalf("envelope mismatch: %+v vs %+v", out, in)
	}
	if !jsonEqual(out.Payload, in.Payload) {
		t.Fatalf("payload mismatch: %s vs %s", out.Payload, in.Payload)
	}
	for k, v := range in.Metadata {
		a, _ := json.Marshal(v)
		bb, _ := json.Marshal(out.Metadata[k])
		if !jsonEqual(a, bb) {
			t.Fatalf("metadata %q mismatch: %s vs %s", k, bb, a)
		}
	}
	if ds[0].Attempt() != 1 || out.MetaInt(domain.MetaAttempt) != 1 {
		t.Fatalf("first attempt should be 1, got %d / meta %v", ds[0].Attempt(), out.Metadata[domain.MetaAttempt])
	}
	ackAll(t, ds)
}

func jsonEqual(a, b []byte) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	xa, _ := json.Marshal(x)
	ya, _ := json.Marshal(y)
	return string(xa) == string(ya)
}

func testCopyPerGroup(t *testing.T, b queue.Broker, o Options) {
	subj := uniq("cpg")
	declare(t, b, subj, o, "g1", "g2")
	s1 := subscribe(t, b, subj, "g1", "c1")
	s2 := subscribe(t, b, subj, "g2", "c2")
	for i := 0; i < 3; i++ {
		publish(t, b, subj, msg(t, i))
	}
	for name, s := range map[string]queue.Subscription{"g1": s1, "g2": s2} {
		ds := fetchN(t, s, 3, o.FetchWait)
		if len(ds) != 3 {
			t.Fatalf("group %s: expected 3, got %d", name, len(ds))
		}
		ackAll(t, ds)
	}
}

func testCompeting(t *testing.T, b queue.Broker, o Options) {
	subj := uniq("cmp")
	declare(t, b, subj, o, "work")
	const n = 20
	subs := []queue.Subscription{subscribe(t, b, subj, "work", "c1"), subscribe(t, b, subj, "work", "c2")}
	if o.Settle > 0 {
		// Let group membership settle so both consumers own partitions.
		for _, s := range subs {
			_, _ = s.Fetch(ctx(t), 1, 100*time.Millisecond)
		}
		time.Sleep(o.Settle)
	}
	for i := 0; i < n; i++ {
		publish(t, b, subj, msg(t, i))
	}
	var mu sync.Mutex
	seen := map[string]int{}
	total := 0
	var wg sync.WaitGroup
	deadline := time.Now().Add(o.FetchWait * 3)
	for _, s := range subs {
		wg.Add(1)
		go func(s queue.Subscription) {
			defer wg.Done()
			for time.Now().Before(deadline) {
				mu.Lock()
				done := total >= n
				mu.Unlock()
				if done {
					return
				}
				ds, err := s.Fetch(context.Background(), 3, 300*time.Millisecond)
				if err != nil {
					t.Errorf("fetch: %v", err)
					return
				}
				for _, d := range ds {
					mu.Lock()
					seen[d.Message().ID]++
					total++
					mu.Unlock()
					if err := d.Ack(context.Background()); err != nil {
						t.Errorf("ack: %v", err)
					}
				}
			}
		}(s)
	}
	wg.Wait()
	if len(seen) != n {
		t.Fatalf("expected %d distinct messages, got %d (total deliveries %d)", n, len(seen), total)
	}
	for id, c := range seen {
		if c != 1 {
			t.Fatalf("message %s delivered %d times", id, c)
		}
	}
}

func testAckRemoves(t *testing.T, b queue.Broker, o Options) {
	subj := uniq("ack")
	declare(t, b, subj, o, "g")
	s := subscribe(t, b, subj, "g", "c")
	publish(t, b, subj, msg(t, 1))
	ds := fetchN(t, s, 1, o.FetchWait)
	if len(ds) != 1 {
		t.Fatalf("expected 1, got %d", len(ds))
	}
	ackAll(t, ds)
	expectEmpty(t, s, o.AckWait+time.Second)
}

func testNack(t *testing.T, b queue.Broker, o Options) {
	subj := uniq("nack")
	declare(t, b, subj, o, "g")
	s := subscribe(t, b, subj, "g", "c")
	in := msg(t, 1)
	publish(t, b, subj, in)
	ds := fetchN(t, s, 1, o.FetchWait)
	if len(ds) != 1 {
		t.Fatalf("expected 1, got %d", len(ds))
	}
	if err := ds[0].Nack(ctx(t), 0); err != nil {
		t.Fatalf("nack: %v", err)
	}
	ds = fetchN(t, s, 1, o.FetchWait)
	if len(ds) != 1 || ds[0].Message().ID != in.ID {
		t.Fatalf("expected redelivery of %s, got %d", in.ID, len(ds))
	}
	if ds[0].Attempt() != 2 {
		t.Fatalf("expected attempt 2, got %d", ds[0].Attempt())
	}
	ackAll(t, ds)
}

func testNackDelay(t *testing.T, b queue.Broker, o Options) {
	subj := uniq("nackd")
	declare(t, b, subj, o, "g")
	s := subscribe(t, b, subj, "g", "c")
	publish(t, b, subj, msg(t, 1))
	ds := fetchN(t, s, 1, o.FetchWait)
	if len(ds) != 1 {
		t.Fatalf("expected 1, got %d", len(ds))
	}
	start := time.Now()
	if err := ds[0].Nack(ctx(t), 1500*time.Millisecond); err != nil {
		t.Fatalf("nack: %v", err)
	}
	expectEmpty(t, s, 700*time.Millisecond)
	ds = fetchN(t, s, 1, o.FetchWait)
	if len(ds) != 1 {
		t.Fatalf("expected delayed redelivery")
	}
	if el := time.Since(start); el < time.Second {
		t.Fatalf("redelivered too early: %v", el)
	}
	ackAll(t, ds)
}

func testLeaseExpiry(t *testing.T, b queue.Broker, o Options) {
	subj := uniq("lease")
	declare(t, b, subj, o, "g")
	s1 := subscribe(t, b, subj, "g", "c1")
	in := msg(t, 1)
	publish(t, b, subj, in)
	ds := fetchN(t, s1, 1, o.FetchWait)
	if len(ds) != 1 {
		t.Fatalf("expected 1, got %d", len(ds))
	}
	// Do not ack: the lease must expire and the message come back.
	ds2 := fetchN(t, s1, 1, o.AckWait*4+o.FetchWait)
	if len(ds2) != 1 || ds2[0].Message().ID != in.ID {
		t.Fatalf("expected redelivery after lease expiry, got %d", len(ds2))
	}
	if ds2[0].Attempt() < 2 {
		t.Fatalf("expected attempt >= 2, got %d", ds2[0].Attempt())
	}
	ackAll(t, ds2)
}

func testTerm(t *testing.T, b queue.Broker, o Options) {
	subj := uniq("term")
	declare(t, b, subj, o, "g")
	s := subscribe(t, b, subj, "g", "c")
	publish(t, b, subj, msg(t, 1))
	ds := fetchN(t, s, 1, o.FetchWait)
	if len(ds) != 1 {
		t.Fatalf("expected 1, got %d", len(ds))
	}
	if err := ds[0].Term(ctx(t), "poison"); err != nil {
		t.Fatalf("term: %v", err)
	}
	expectEmpty(t, s, o.AckWait+time.Second)
	if !o.NoDeadStats {
		waitStats(t, b, subj, "g", func(st queue.Stats) bool { return st.Dead >= 1 }, "dead >= 1")
	}
}

func testMaxDeliver(t *testing.T, b queue.Broker, o Options) {
	subj := uniq("maxd")
	if err := b.Declare(ctx(t), queue.SubjectConfig{Subject: subj, Groups: []string{"g"}, AckWait: o.AckWait, MaxDeliver: 2}); err != nil {
		t.Fatal(err)
	}
	if o.Settle > 0 {
		time.Sleep(o.Settle)
	}
	s := subscribe(t, b, subj, "g", "c")
	publish(t, b, subj, msg(t, 1))
	for i := 1; i <= 2; i++ {
		ds := fetchN(t, s, 1, o.FetchWait)
		if len(ds) != 1 {
			t.Fatalf("delivery %d: expected 1, got %d", i, len(ds))
		}
		if err := ds[0].Nack(ctx(t), 0); err != nil {
			t.Fatalf("nack: %v", err)
		}
	}
	expectEmpty(t, s, o.AckWait+time.Second)
	if !o.NoDeadStats {
		waitStats(t, b, subj, "g", func(st queue.Stats) bool { return st.Dead >= 1 }, "dead >= 1")
	}
}

func testFetchTimeout(t *testing.T, b queue.Broker, o Options) {
	subj := uniq("fto")
	declare(t, b, subj, o, "g")
	s := subscribe(t, b, subj, "g", "c")
	start := time.Now()
	ds, err := s.Fetch(ctx(t), 5, 300*time.Millisecond)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(ds) != 0 {
		t.Fatalf("expected empty, got %d", len(ds))
	}
	if el := time.Since(start); el > 3*time.Second {
		t.Fatalf("fetch with 300ms wait took %v", el)
	}
	// A cancelled context returns its error.
	c, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Fetch(c, 1, time.Second); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func waitStats(t *testing.T, b queue.Broker, subj, grp string, ok func(queue.Stats) bool, what string) queue.Stats {
	var st queue.Stats
	deadline := time.Now().Add(10 * time.Second)
	for {
		var err error
		st, err = b.Stats(ctx(t), subj, grp)
		if err != nil {
			t.Fatalf("stats: %v", err)
		}
		if ok(st) {
			return st
		}
		if time.Now().After(deadline) {
			t.Fatalf("stats never satisfied %s: %+v", what, st)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func testStats(t *testing.T, b queue.Broker, o Options) {
	subj := uniq("stats")
	declare(t, b, subj, o, "g")
	s := subscribe(t, b, subj, "g", "c")
	for i := 0; i < 3; i++ {
		publish(t, b, subj, msg(t, i))
	}
	waitStats(t, b, subj, "g", func(st queue.Stats) bool { return st.Pending+st.InFlight == 3 }, "pending+inflight == 3")
	ds := fetchN(t, s, 1, o.FetchWait)
	if len(ds) != 1 {
		t.Fatalf("expected 1, got %d", len(ds))
	}
	if !o.NoInFlightStats {
		waitStats(t, b, subj, "g", func(st queue.Stats) bool { return st.InFlight == 1 && st.Pending == 2 }, "inflight 1, pending 2")
	}
	ackAll(t, ds)
	waitStats(t, b, subj, "g", func(st queue.Stats) bool { return st.Pending+st.InFlight == 2 }, "pending+inflight == 2 after ack")
}

func testDeclareIdempotent(t *testing.T, b queue.Broker, o Options) {
	subj := uniq("decl")
	declare(t, b, subj, o, "g1")
	declare(t, b, subj, o, "g1")
	declare(t, b, subj, o, "g1", "g2") // adds a group
	s1 := subscribe(t, b, subj, "g1", "c1")
	s2 := subscribe(t, b, subj, "g2", "c2")
	publish(t, b, subj, msg(t, 1))
	for _, s := range []queue.Subscription{s1, s2} {
		ds := fetchN(t, s, 1, o.FetchWait)
		if len(ds) != 1 {
			t.Fatalf("expected 1 in each group, got %d", len(ds))
		}
		ackAll(t, ds)
	}
}

func testNoGroups(t *testing.T, b queue.Broker, o Options) {
	if o.SkipNoGroups {
		t.Skip("adapter cannot detect missing groups")
	}
	err := b.Publish(ctx(t), uniq("nogrp"), msg(t, 1))
	if !errors.Is(err, queue.ErrNoGroups) {
		t.Fatalf("expected ErrNoGroups, got %v", err)
	}
}
