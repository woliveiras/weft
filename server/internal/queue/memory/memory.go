// Package memory is an in-process broker with the full delivery semantics
// of the platform (groups, leases, redelivery, delayed nacks, dead letters).
//
// It is the reference implementation the conformance suite is written
// against, the transport for unit tests, and a legitimate choice for a
// single-binary demo. It is NOT durable: everything is lost when the process
// exits, which is why it is never the default.
package memory

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/queue"
	"github.com/woliveiras/weft/server/internal/queue/internal/qutil"
)

// ErrLeaseLost is returned when a delivery is acted on after its lease
// expired and the message was handed to someone else.
var ErrLeaseLost = errors.New("memory: delivery lease lost")

type Broker struct {
	mu       sync.Mutex
	subjects map[string]*subject
	changed  chan struct{} // closed and replaced on every state change
	closed   bool
	seq      uint64
	token    uint64
}

type subject struct {
	cfg    queue.SubjectConfig
	groups map[string]*group
}

type group struct {
	ready    []*entry // sorted by seq
	inflight map[uint64]*entry
	dead     int64
}

type entry struct {
	seq        uint64
	msg        *domain.Message
	attempts   int // deliveries so far
	notBefore  time.Time
	leaseUntil time.Time
	token      uint64
}

func New() *Broker {
	return &Broker{subjects: map[string]*subject{}, changed: make(chan struct{})}
}

func (b *Broker) Name() string { return "memory" }

// notify wakes every waiting Fetch. Caller holds b.mu.
func (b *Broker) notify() {
	close(b.changed)
	b.changed = make(chan struct{})
}

func (b *Broker) Declare(ctx context.Context, cfg queue.SubjectConfig) error {
	if err := qutil.ValidName("subject", cfg.Subject); err != nil {
		return err
	}
	for _, g := range cfg.Groups {
		if err := qutil.ValidName("group", g); err != nil {
			return err
		}
	}
	cfg = cfg.WithDefaults()
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return queue.ErrClosed
	}
	s := b.subjects[cfg.Subject]
	if s == nil {
		s = &subject{groups: map[string]*group{}}
		b.subjects[cfg.Subject] = s
	}
	s.cfg = queue.SubjectConfig{Subject: cfg.Subject, MaxDeliver: cfg.MaxDeliver, AckWait: cfg.AckWait, Retention: cfg.Retention}
	for _, g := range cfg.Groups {
		if s.groups[g] == nil {
			s.groups[g] = &group{inflight: map[uint64]*entry{}}
		}
	}
	return nil
}

func (b *Broker) DeleteGroup(ctx context.Context, subj, grp string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if s := b.subjects[subj]; s != nil {
		delete(s.groups, grp)
	}
	b.notify()
	return nil
}

func (b *Broker) Publish(ctx context.Context, subj string, msg *domain.Message) error {
	if err := msg.Validate(); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return queue.ErrClosed
	}
	s := b.subjects[subj]
	if s == nil || len(s.groups) == 0 {
		return queue.ErrNoGroups
	}
	b.seq++
	for _, g := range s.groups {
		g.ready = append(g.ready, &entry{seq: b.seq, msg: msg.Clone()})
	}
	b.notify()
	return nil
}

func (b *Broker) Subscribe(ctx context.Context, subj, grp, consumer string) (queue.Subscription, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil, queue.ErrClosed
	}
	s := b.subjects[subj]
	if s == nil || s.groups[grp] == nil {
		return nil, errors.New("memory: group " + grp + " not declared on " + subj)
	}
	return &sub{b: b, subject: subj, group: grp, done: make(chan struct{})}, nil
}

func (b *Broker) Stats(ctx context.Context, subj, grp string) (queue.Stats, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.subjects[subj]
	if s == nil || s.groups[grp] == nil {
		return queue.Stats{}, nil
	}
	g := s.groups[grp]
	b.reapLocked(s, g, time.Now())
	return queue.Stats{Pending: int64(len(g.ready)), InFlight: int64(len(g.inflight)), Dead: g.dead}, nil
}

func (b *Broker) Ping(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return queue.ErrClosed
	}
	return nil
}

func (b *Broker) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.closed {
		b.closed = true
		b.notify()
	}
	return nil
}

// reapLocked returns expired leases to the ready list. Caller holds b.mu.
func (b *Broker) reapLocked(s *subject, g *group, now time.Time) {
	reaped := false
	for tok, e := range g.inflight {
		if now.After(e.leaseUntil) {
			delete(g.inflight, tok)
			e.notBefore = time.Time{}
			insertSorted(g, e)
			reaped = true
		}
	}
	if reaped {
		b.notify()
	}
}

func insertSorted(g *group, e *entry) {
	i := sort.Search(len(g.ready), func(i int) bool { return g.ready[i].seq >= e.seq })
	g.ready = append(g.ready, nil)
	copy(g.ready[i+1:], g.ready[i:])
	g.ready[i] = e
}

type sub struct {
	b       *Broker
	subject string
	group   string
	once    sync.Once
	done    chan struct{}
}

func (s *sub) Fetch(ctx context.Context, max int, wait time.Duration) ([]queue.Delivery, error) {
	if max <= 0 {
		max = 1
	}
	deadline := time.Now().Add(wait)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		out, changed, next, err := s.take(max)
		if err != nil || len(out) > 0 {
			return out, err
		}
		now := time.Now()
		if !now.Before(deadline) {
			return nil, nil
		}
		sleep := deadline.Sub(now)
		if !next.IsZero() && next.Sub(now) < sleep {
			sleep = next.Sub(now)
		}
		t := time.NewTimer(sleep)
		select {
		case <-ctx.Done():
			t.Stop()
			return nil, ctx.Err()
		case <-s.done:
			t.Stop()
			return nil, queue.ErrClosed
		case <-changed:
		case <-t.C:
		}
		t.Stop()
	}
}

// take claims up to max ready entries. It also returns the channel to wait
// on and the earliest time something becomes due (delayed nack or lease).
func (s *sub) take(max int) ([]queue.Delivery, <-chan struct{}, time.Time, error) {
	b := s.b
	b.mu.Lock()
	defer b.mu.Unlock()
	select {
	case <-s.done:
		return nil, nil, time.Time{}, queue.ErrClosed
	default:
	}
	if b.closed {
		return nil, nil, time.Time{}, queue.ErrClosed
	}
	subj := b.subjects[s.subject]
	if subj == nil || subj.groups[s.group] == nil {
		return nil, nil, time.Time{}, errors.New("memory: group deleted")
	}
	g := subj.groups[s.group]
	now := time.Now()
	b.reapLocked(subj, g, now)
	var out []queue.Delivery
	var next time.Time
	keep := g.ready[:0]
	for _, e := range g.ready {
		if len(out) >= max || now.Before(e.notBefore) {
			if now.Before(e.notBefore) && (next.IsZero() || e.notBefore.Before(next)) {
				next = e.notBefore
			}
			keep = append(keep, e)
			continue
		}
		e.attempts++
		if e.attempts > subj.cfg.MaxDeliver {
			g.dead++
			continue
		}
		b.token++
		e.token = b.token
		e.leaseUntil = now.Add(subj.cfg.AckWait)
		g.inflight[e.token] = e
		m := e.msg.Clone()
		m.SetMeta(domain.MetaAttempt, e.attempts)
		out = append(out, &delivery{s: s, e: e, token: e.token, attempt: e.attempts, msg: m})
	}
	g.ready = keep
	for _, e := range g.inflight {
		if next.IsZero() || e.leaseUntil.Before(next) {
			next = e.leaseUntil
		}
	}
	return out, b.changed, next, nil
}

func (s *sub) Close() error {
	s.once.Do(func() { close(s.done) })
	return nil
}

type delivery struct {
	s       *sub
	e       *entry
	token   uint64
	attempt int
	msg     *domain.Message
}

func (d *delivery) Message() *domain.Message { return d.msg }
func (d *delivery) Attempt() int             { return d.attempt }

// settle removes the entry from in-flight if this delivery still owns it.
func (d *delivery) settle(fn func(s *subject, g *group)) error {
	b := d.s.b
	b.mu.Lock()
	defer b.mu.Unlock()
	subj := b.subjects[d.s.subject]
	if subj == nil || subj.groups[d.s.group] == nil {
		return ErrLeaseLost
	}
	g := subj.groups[d.s.group]
	if cur, ok := g.inflight[d.token]; !ok || cur != d.e {
		return ErrLeaseLost
	}
	fn(subj, g)
	b.notify()
	return nil
}

func (d *delivery) Ack(ctx context.Context) error {
	return d.settle(func(s *subject, g *group) { delete(g.inflight, d.token) })
}

func (d *delivery) Nack(ctx context.Context, delay time.Duration) error {
	return d.settle(func(s *subject, g *group) {
		delete(g.inflight, d.token)
		if d.e.attempts >= s.cfg.MaxDeliver {
			g.dead++
			return
		}
		d.e.notBefore = time.Now().Add(delay)
		insertSorted(g, d.e)
	})
}

func (d *delivery) Term(ctx context.Context, reason string) error {
	return d.settle(func(s *subject, g *group) {
		delete(g.inflight, d.token)
		g.dead++
	})
}

func (d *delivery) Extend(ctx context.Context) error {
	return d.settle(func(s *subject, g *group) { d.e.leaseUntil = time.Now().Add(s.cfg.AckWait) })
}

var _ queue.Broker = (*Broker)(nil)
