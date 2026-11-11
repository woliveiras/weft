// Package kafkaq is an Apache Kafka broker (also Redpanda and other
// Kafka-protocol servers), built on franz-go.
//
// # Mapping
//
//	subject  -> topic "weft.<subject>" (Partitions partitions, retention.ms = Retention)
//	group    -> Kafka consumer group "weft.<subject>.<group>", subscribed to the subject
//	            topic and to its own retry topic
//	retries  -> topic "weft-retry.<subject>.<group>"   (consumed by that group only)
//	dead     -> topic "weft-dlq.<subject>.<group>"
//
// Records are keyed by the message correlation id, so all messages of one
// workflow land on one partition and keep their relative order.
//
// # Trade-offs (read before choosing Kafka)
//
// Kafka is a log, not a queue: a consumer group owns partitions, not
// messages. The adapter layers queue semantics on top, and the seams show:
//
//   - Competing consumers are bounded by partitions. A group never has more
//     active consumers than its topic has partitions (default 12), so set
//     Partitions >= the largest replica count of any node reading the channel.
//   - Acks commit offsets, and an offset can only be committed once every
//     earlier record of the partition is settled. The adapter tracks settled
//     offsets per partition and commits the contiguous frontier
//     synchronously. A slow message holds back the commit (not the delivery)
//     of later ones; after a crash or rebalance those later ones are
//     delivered again. Consumers de-duplicate by message id.
//   - There is no per-message lease. The adapter enforces AckWait locally: a
//     watchdog treats an expired delivery like Nack(0).
//   - Nack cannot put a record "back". It produces a copy with an incremented
//     attempt header (and a not-before header for delays) to the group's
//     retry topic and settles the original. A delayed retry is held by the
//     consumer that polled it until due, which also holds back that retry
//     partition's commit frontier.
//   - Redelivery caused by a crash or rebalance re-reads the original record,
//     so the attempt counter only counts explicit nacks and lease expiries.
//   - New groups start at the end of the topic (Declare commits the current
//     end offsets for a group that has none), matching the contract that a
//     group receives messages published after it is declared.
//   - Group configuration (AckWait, MaxDeliver) is held by the process that
//     declared it; subscribe-only processes use Options defaults.
//   - Stats.Pending is consumer lag minus local in-flight deliveries;
//     InFlight is local to this process; Dead is the retained size of the DLQ.
//
// ErrNoGroups is returned when the subject topic does not exist (never
// declared); Kafka itself cannot tell whether any group will read it.
package kafkaq

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/queue"
	"github.com/woliveiras/weft/server/internal/queue/internal/qutil"
)

const (
	hdrPrior     = "weft-prior"      // deliveries before this copy was produced
	hdrNotBefore = "weft-not-before" // unix millis
	hdrReason    = "weft-reason"
	hdrGroup     = "weft-group"
)

// ErrLeaseLost is returned when acting on a settled delivery.
var ErrLeaseLost = errors.New("kafkaq: delivery lease lost")

type Options struct {
	Brokers           []string
	Partitions        int32 // default 12
	ReplicationFactor int16 // default -1 (broker default)
	AckWait           time.Duration
	MaxDeliver        int
	ClientID          string
}

type Broker struct {
	opts Options
	cl   *kgo.Client
	adm  *kadm.Client

	mu       sync.Mutex
	topics   map[string]bool
	cfgs     map[string]queue.SubjectConfig
	inflight map[string]int64 // by kafka group
	closed   bool
}

func New(ctx context.Context, o Options) (*Broker, error) {
	if len(o.Brokers) == 0 {
		o.Brokers = []string{"localhost:9092"}
	}
	if o.Partitions <= 0 {
		o.Partitions = 12
	}
	if o.ReplicationFactor == 0 {
		o.ReplicationFactor = -1
	}
	if o.AckWait <= 0 {
		o.AckWait = 10 * time.Minute
	}
	if o.MaxDeliver <= 0 {
		o.MaxDeliver = 5
	}
	if o.ClientID == "" {
		o.ClientID = "weft"
	}
	cl, err := kgo.NewClient(
		kgo.SeedBrokers(o.Brokers...),
		kgo.ClientID(o.ClientID),
		kgo.RequiredAcks(kgo.AllISRAcks()),
		kgo.ProducerLinger(0),
	)
	if err != nil {
		return nil, fmt.Errorf("kafkaq: %w", err)
	}
	b := &Broker{opts: o, cl: cl, adm: kadm.NewClient(cl), topics: map[string]bool{}, cfgs: map[string]queue.SubjectConfig{}, inflight: map[string]int64{}}
	return b, nil
}

func (b *Broker) Name() string { return "kafka" }

func mainTopic(subject string) string         { return "weft." + subject }
func retryTopic(subject, group string) string { return "weft-retry." + subject + "." + group }
func dlqTopic(subject, group string) string   { return "weft-dlq." + subject + "." + group }
func kgroup(subject, group string) string     { return "weft." + subject + "." + group }

func (b *Broker) createTopics(ctx context.Context, partitions int32, configs map[string]*string, topics ...string) error {
	for _, t := range topics {
		if len(t) > 249 {
			return fmt.Errorf("kafkaq: topic name too long: %s", t)
		}
	}
	res, err := b.adm.CreateTopics(ctx, partitions, b.opts.ReplicationFactor, configs, topics...)
	if err != nil {
		return fmt.Errorf("kafkaq: create topics: %w", err)
	}
	for _, r := range res {
		if r.Err != nil && !errors.Is(r.Err, kerr.TopicAlreadyExists) {
			return fmt.Errorf("kafkaq: create topic %s: %w (%s)", r.Topic, r.Err, r.ErrMessage)
		}
	}
	return nil
}

func (b *Broker) Declare(ctx context.Context, cfg queue.SubjectConfig) error {
	if err := qutil.ValidName("subject", cfg.Subject); err != nil {
		return err
	}
	cfg = cfg.WithDefaults()
	ret := strconv.FormatInt(cfg.Retention.Milliseconds(), 10)
	topic := mainTopic(cfg.Subject)
	if err := b.createTopics(ctx, b.opts.Partitions, map[string]*string{"retention.ms": &ret}, topic); err != nil {
		return err
	}
	for _, g := range cfg.Groups {
		if err := qutil.ValidName("group", g); err != nil {
			return err
		}
		if err := b.createTopics(ctx, b.opts.Partitions, map[string]*string{"retention.ms": &ret}, retryTopic(cfg.Subject, g)); err != nil {
			return err
		}
		if err := b.createTopics(ctx, 1, nil, dlqTopic(cfg.Subject, g)); err != nil {
			return err
		}
		if err := b.initGroup(ctx, kgroup(cfg.Subject, g), topic); err != nil {
			return err
		}
	}
	b.mu.Lock()
	b.topics[topic] = true
	b.cfgs[cfg.Subject] = cfg
	b.mu.Unlock()
	return nil
}

// initGroup commits the current end offsets of the subject topic for a group
// that has no commits yet, so it starts with messages published from now on.
func (b *Broker) initGroup(ctx context.Context, group, topic string) error {
	offs, err := b.adm.FetchOffsets(ctx, group)
	if err == nil {
		if ps, ok := offs[topic]; ok && len(ps) > 0 {
			return nil
		}
	}
	// Newly created topics can take a moment to report offsets for every
	// partition; retry briefly.
	var last error
	for i := 0; i < 20; i++ {
		ends, err := b.adm.ListEndOffsets(ctx, topic)
		if err == nil && ends.Error() == nil {
			resp, err := b.adm.CommitOffsets(ctx, group, ends.Offsets())
			if err == nil && resp.Error() == nil {
				return nil
			}
			if err == nil {
				err = resp.Error()
			}
			last = err
		} else if err != nil {
			last = err
		} else {
			last = ends.Error()
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	return fmt.Errorf("kafkaq: initialise group %s: %w", group, last)
}

func (b *Broker) DeleteGroup(ctx context.Context, subject, group string) error {
	if _, err := b.adm.DeleteGroups(ctx, kgroup(subject, group)); err != nil {
		return err
	}
	_, err := b.adm.DeleteTopics(ctx, retryTopic(subject, group), dlqTopic(subject, group))
	return err
}

func (b *Broker) topicExists(ctx context.Context, topic string) (bool, error) {
	b.mu.Lock()
	ok := b.topics[topic]
	b.mu.Unlock()
	if ok {
		return true, nil
	}
	td, err := b.adm.ListTopics(ctx, topic)
	if err != nil {
		return false, err
	}
	if d, ok := td[topic]; ok && d.Err == nil {
		b.mu.Lock()
		b.topics[topic] = true
		b.mu.Unlock()
		return true, nil
	}
	return false, nil
}

func keyOf(m *domain.Message) []byte {
	if c := m.MetaString(domain.MetaCorrelationID); c != "" {
		return []byte(c)
	}
	return []byte(m.ID)
}

func (b *Broker) Publish(ctx context.Context, subject string, msg *domain.Message) error {
	data, err := qutil.Encode(msg)
	if err != nil {
		return err
	}
	topic := mainTopic(subject)
	ok, err := b.topicExists(ctx, topic)
	if err != nil {
		return fmt.Errorf("kafkaq: publish: %w", err)
	}
	if !ok {
		return queue.ErrNoGroups
	}
	rec := &kgo.Record{Topic: topic, Key: keyOf(msg), Value: data, Headers: []kgo.RecordHeader{{Key: hdrPrior, Value: []byte("0")}}}
	if err := b.cl.ProduceSync(ctx, rec).FirstErr(); err != nil {
		return fmt.Errorf("kafkaq: publish: %w", err)
	}
	return nil
}

func (b *Broker) groupCfg(subject string) (time.Duration, int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if c, ok := b.cfgs[subject]; ok {
		return c.AckWait, c.MaxDeliver
	}
	return b.opts.AckWait, b.opts.MaxDeliver
}

func (b *Broker) addInflight(g string, n int64) {
	b.mu.Lock()
	b.inflight[g] += n
	b.mu.Unlock()
}

func (b *Broker) Subscribe(ctx context.Context, subject, group, consumer string) (queue.Subscription, error) {
	ok, err := b.topicExists(ctx, mainTopic(subject))
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("kafkaq: subject %s not declared", subject)
	}
	ackWait, maxDeliver := b.groupCfg(subject)
	s := &sub{
		b: b, subject: subject, group: group, kgroup: kgroup(subject, group),
		ackWait: ackWait, maxDeliver: maxDeliver,
		parts: map[tp]*partState{}, out: map[*delivery]bool{},
		done: make(chan struct{}),
	}
	cl, err := kgo.NewClient(
		kgo.SeedBrokers(b.opts.Brokers...),
		kgo.ClientID(b.opts.ClientID+"-"+consumer),
		kgo.ConsumerGroup(s.kgroup),
		kgo.ConsumeTopics(mainTopic(subject), retryTopic(subject, group)),
		kgo.DisableAutoCommit(),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
		kgo.FetchMaxWait(500*time.Millisecond),
		kgo.OnPartitionsRevoked(s.onRevoked),
		kgo.OnPartitionsLost(s.onRevoked),
	)
	if err != nil {
		return nil, fmt.Errorf("kafkaq: consumer: %w", err)
	}
	s.cl = cl
	s.wg.Add(1)
	go s.watchdog()
	return s, nil
}

func (b *Broker) Stats(ctx context.Context, subject, group string) (queue.Stats, error) {
	g := kgroup(subject, group)
	var st queue.Stats
	lags, err := b.adm.Lag(ctx, g)
	if err != nil {
		return st, err
	}
	var total int64
	if l, ok := lags[g]; ok {
		for topic, ps := range l.Lag {
			if topic != mainTopic(subject) && topic != retryTopic(subject, group) {
				continue
			}
			for _, p := range ps {
				if p.Lag > 0 {
					total += p.Lag
				}
			}
		}
	}
	b.mu.Lock()
	st.InFlight = b.inflight[g]
	b.mu.Unlock()
	st.Pending = max(total-st.InFlight, 0)
	dlq := dlqTopic(subject, group)
	if ends, err := b.adm.ListEndOffsets(ctx, dlq); err == nil {
		starts, _ := b.adm.ListStartOffsets(ctx, dlq)
		ends.Each(func(o kadm.ListedOffset) {
			start := int64(0)
			if s, ok := starts.Lookup(o.Topic, o.Partition); ok && s.Offset > 0 {
				start = s.Offset
			}
			if o.Offset > start {
				st.Dead += o.Offset - start
			}
		})
	}
	return st, nil
}

func (b *Broker) Ping(ctx context.Context) error { return b.cl.Ping(ctx) }

func (b *Broker) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.closed {
		b.closed = true
		b.cl.Close()
	}
	return nil
}

// ---- subscription -------------------------------------------------------------

type tp struct {
	topic     string
	partition int32
}

// partState tracks which offsets of one partition are unsettled, so commits
// only ever advance over a contiguous settled prefix.
type partState struct {
	outstanding map[int64]bool
	maxSettled  int64
	committed   int64
	epoch       int32
}

func (p *partState) frontier() int64 {
	if len(p.outstanding) > 0 {
		low := int64(-1)
		for o := range p.outstanding {
			if low < 0 || o < low {
				low = o
			}
		}
		return low
	}
	return p.maxSettled + 1
}

type sub struct {
	b          *Broker
	cl         *kgo.Client
	subject    string
	group      string
	kgroup     string
	ackWait    time.Duration
	maxDeliver int

	mu      sync.Mutex
	parts   map[tp]*partState
	delayed []*delivery        // polled retries not yet due
	out     map[*delivery]bool // delivered, unsettled
	gen     int                // bumped on revoke; deliveries from older generations are void

	pollMu sync.Mutex
	once   sync.Once
	done   chan struct{}
	wg     sync.WaitGroup
}

func (s *sub) onRevoked(ctx context.Context, cl *kgo.Client, revoked map[string][]int32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for topic, ps := range revoked {
		for _, p := range ps {
			delete(s.parts, tp{topic, p})
		}
	}
	keep := s.delayed[:0]
	for _, d := range s.delayed {
		if _, ok := s.parts[d.tp]; ok {
			keep = append(keep, d)
		}
	}
	s.delayed = keep
	for d := range s.out {
		if _, ok := s.parts[d.tp]; !ok {
			d.void = true
			delete(s.out, d)
			s.b.addInflight(s.kgroup, -1)
		}
	}
}

func header(r *kgo.Record, k string) string {
	for _, h := range r.Headers {
		if h.Key == k {
			return string(h.Value)
		}
	}
	return ""
}

func (s *sub) Fetch(ctx context.Context, max int, wait time.Duration) ([]queue.Delivery, error) {
	if max <= 0 {
		max = 1
	}
	deadline := time.Now().Add(wait)
	for {
		select {
		case <-s.done:
			return nil, queue.ErrClosed
		default:
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		out, next := s.takeDue(max)
		if len(out) > 0 {
			return out, nil
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, nil
		}
		pollFor := remaining
		if !next.IsZero() && time.Until(next) < pollFor {
			pollFor = time.Until(next)
			if pollFor < 10*time.Millisecond {
				pollFor = 10 * time.Millisecond
			}
		}
		if err := s.poll(ctx, max, pollFor); err != nil {
			return nil, err
		}
	}
}

// takeDue hands out delayed records whose time has come.
func (s *sub) takeDue(max int) ([]queue.Delivery, time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	var out []queue.Delivery
	var next time.Time
	keep := s.delayed[:0]
	for _, d := range s.delayed {
		if len(out) < max && !now.Before(d.notBefore) {
			d.leaseUntil = now.Add(s.ackWait)
			s.out[d] = true
			s.b.addInflight(s.kgroup, 1)
			out = append(out, d)
			continue
		}
		if next.IsZero() || d.notBefore.Before(next) {
			next = d.notBefore
		}
		keep = append(keep, d)
	}
	s.delayed = keep
	return out, next
}

func (s *sub) poll(ctx context.Context, max int, wait time.Duration) error {
	s.pollMu.Lock()
	defer s.pollMu.Unlock()
	pctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	go func() {
		select {
		case <-s.done:
			cancel()
		case <-pctx.Done():
		}
	}()
	fs := s.cl.PollRecords(pctx, max)
	if fs.IsClientClosed() {
		return queue.ErrClosed
	}
	if err := ctx.Err(); err != nil {
		// Leave records unsettled; they are redelivered after a rebalance.
		return err
	}
	var ferr error
	fs.EachError(func(t string, p int32, err error) {
		if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) && ferr == nil {
			ferr = fmt.Errorf("kafkaq: fetch %s/%d: %w", t, p, err)
		}
	})
	s.mu.Lock()
	gen := s.gen
	var dead []*delivery
	fs.EachRecord(func(r *kgo.Record) {
		key := tp{r.Topic, r.Partition}
		ps := s.parts[key]
		if ps == nil {
			ps = &partState{outstanding: map[int64]bool{}, maxSettled: r.Offset - 1, committed: r.Offset}
			s.parts[key] = ps
		}
		ps.outstanding[r.Offset] = true
		ps.epoch = r.LeaderEpoch
		prior, _ := strconv.Atoi(header(r, hdrPrior))
		d := &delivery{s: s, rec: r, tp: key, gen: gen, attempt: prior + 1}
		if nb, err := strconv.ParseInt(header(r, hdrNotBefore), 10, 64); err == nil {
			d.notBefore = time.UnixMilli(nb)
		}
		if d.attempt > s.maxDeliver {
			dead = append(dead, d)
			return
		}
		m, err := qutil.Decode(r.Value, d.attempt)
		if err != nil {
			d.reason = err.Error()
			dead = append(dead, d)
			return
		}
		d.msg = m
		s.delayed = append(s.delayed, d) // due ones are handed out by takeDue
	})
	s.mu.Unlock()
	for _, d := range dead {
		reason := d.reason
		if reason == "" {
			reason = "max deliveries exceeded"
		}
		_ = d.deadLetter(ctx, reason)
	}
	return ferr
}

// settle marks a record done and commits the partition frontier.
func (s *sub) settle(ctx context.Context, d *delivery) error {
	s.mu.Lock()
	if d.void {
		s.mu.Unlock()
		return nil // partition was revoked; the new owner will re-read it
	}
	if s.out[d] {
		delete(s.out, d)
		s.b.addInflight(s.kgroup, -1)
	}
	ps := s.parts[d.tp]
	if ps == nil {
		s.mu.Unlock()
		return nil
	}
	delete(ps.outstanding, d.rec.Offset)
	if d.rec.Offset > ps.maxSettled {
		ps.maxSettled = d.rec.Offset
	}
	f := ps.frontier()
	if f <= ps.committed {
		s.mu.Unlock()
		return nil
	}
	commit := map[string]map[int32]kgo.EpochOffset{d.tp.topic: {d.tp.partition: {Epoch: ps.epoch, Offset: f}}}
	s.mu.Unlock()
	var cerr error
	done := make(chan struct{})
	s.cl.CommitOffsetsSync(ctx, commit, func(_ *kgo.Client, _ *kmsgReq, resp *kmsgResp, err error) {
		cerr = err
		if err == nil {
			cerr = commitErr(resp)
		}
		close(done)
	})
	<-done
	if cerr != nil {
		return fmt.Errorf("kafkaq: commit: %w", cerr)
	}
	s.mu.Lock()
	if ps := s.parts[d.tp]; ps != nil && f > ps.committed {
		ps.committed = f
	}
	s.mu.Unlock()
	return nil
}

func (s *sub) watchdog() {
	defer s.wg.Done()
	t := time.NewTicker(250 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-t.C:
		}
		now := time.Now()
		var expired []*delivery
		s.mu.Lock()
		for d := range s.out {
			if now.After(d.leaseUntil) {
				expired = append(expired, d)
			}
		}
		s.mu.Unlock()
		for _, d := range expired {
			_ = d.Nack(context.Background(), 0)
		}
	}
}

func (s *sub) Close() error {
	s.once.Do(func() {
		close(s.done)
		s.wg.Wait()
		s.pollMu.Lock()
		s.cl.Close()
		s.pollMu.Unlock()
		s.mu.Lock()
		s.b.addInflight(s.kgroup, -int64(len(s.out)))
		s.out = map[*delivery]bool{}
		s.mu.Unlock()
	})
	return nil
}

type delivery struct {
	s          *sub
	rec        *kgo.Record
	tp         tp
	gen        int
	msg        *domain.Message
	attempt    int
	notBefore  time.Time
	leaseUntil time.Time
	reason     string

	mu      sync.Mutex
	settled bool
	void    bool // guarded by s.mu
}

func (d *delivery) Message() *domain.Message { return d.msg }
func (d *delivery) Attempt() int             { return d.attempt }

func (d *delivery) once(fn func() error) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.settled {
		return ErrLeaseLost
	}
	if err := fn(); err != nil {
		return err
	}
	d.settled = true
	return nil
}

func (d *delivery) Ack(ctx context.Context) error {
	return d.once(func() error { return d.s.settle(ctx, d) })
}

func (d *delivery) produce(ctx context.Context, topic string, hdrs ...kgo.RecordHeader) error {
	rec := &kgo.Record{Topic: topic, Key: d.rec.Key, Value: d.rec.Value, Headers: hdrs}
	return d.s.b.cl.ProduceSync(ctx, rec).FirstErr()
}

func (d *delivery) Nack(ctx context.Context, delay time.Duration) error {
	if d.attempt >= d.s.maxDeliver {
		return d.Term(ctx, "max deliveries exceeded")
	}
	return d.once(func() error {
		hdrs := []kgo.RecordHeader{{Key: hdrPrior, Value: []byte(strconv.Itoa(d.attempt))}}
		if delay > 0 {
			hdrs = append(hdrs, kgo.RecordHeader{Key: hdrNotBefore, Value: []byte(strconv.FormatInt(time.Now().Add(delay).UnixMilli(), 10))})
		}
		if err := d.produce(ctx, retryTopic(d.s.subject, d.s.group), hdrs...); err != nil {
			return fmt.Errorf("kafkaq: nack: %w", err)
		}
		return d.s.settle(ctx, d)
	})
}

func (d *delivery) Term(ctx context.Context, reason string) error { return d.deadLetter(ctx, reason) }

func (d *delivery) deadLetter(ctx context.Context, reason string) error {
	return d.once(func() error {
		err := d.produce(ctx, dlqTopic(d.s.subject, d.s.group),
			kgo.RecordHeader{Key: hdrPrior, Value: []byte(strconv.Itoa(d.attempt))},
			kgo.RecordHeader{Key: hdrReason, Value: []byte(reason)},
			kgo.RecordHeader{Key: hdrGroup, Value: []byte(d.s.group)})
		if err != nil {
			return fmt.Errorf("kafkaq: dead-letter: %w", err)
		}
		return d.s.settle(ctx, d)
	})
}

func (d *delivery) Extend(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.settled {
		return ErrLeaseLost
	}
	d.s.mu.Lock()
	d.leaseUntil = time.Now().Add(d.s.ackWait)
	d.s.mu.Unlock()
	return nil
}

// ParseBrokers splits a "host:port,host:port" list (optionally kafka:// prefixed).
func ParseBrokers(url string) []string {
	url = strings.TrimPrefix(url, "kafka://")
	var out []string
	for _, p := range strings.Split(url, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

type (
	kmsgReq  = kmsg.OffsetCommitRequest
	kmsgResp = kmsg.OffsetCommitResponse
)

func commitErr(resp *kmsgResp) error {
	if resp == nil {
		return nil
	}
	for _, t := range resp.Topics {
		for _, p := range t.Partitions {
			if err := kerr.ErrorForCode(p.ErrorCode); err != nil {
				return err
			}
		}
	}
	return nil
}

var _ queue.Broker = (*Broker)(nil)
