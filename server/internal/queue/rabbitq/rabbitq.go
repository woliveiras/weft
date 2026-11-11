// Package rabbitq is a RabbitMQ (AMQP 0-9-1) broker. Requires RabbitMQ >= 3.8
// (quorum queues).
//
// # Mapping
//
//	all subjects   -> durable topic exchange "weft"
//	subject        -> routing key "<subject>"
//	group          -> durable quorum queue "weft.<subject>.<group>" bound with key = subject
//	delayed retry  -> classic queue "weft.<subject>.<group>:retry"
//	                  (per-message TTL, dead-letters back into the group queue)
//	dead letters   -> durable queue "weft.<subject>.<group>:dead"
//
// Publishing is persistent, mandatory and confirmed: Publish returns only
// after the broker has accepted the message into every bound queue, and an
// unroutable message (no group declared) comes back as queue.ErrNoGroups.
//
// # Pull, not push
//
// Fetch uses basic.get. A push consumer with prefetch would make RabbitMQ
// hand messages to a replica that is busy with a long turn and hold them
// there, invisible to idle replicas; pulling keeps the "an agent takes work
// only when it is ready" contract exactly. The cost is a round trip per
// message and polling with back-off (50ms -> 500ms) when the queue is empty,
// which is negligible next to an LLM turn.
//
// # Leases, retries and attempts
//
// AMQP has no per-message lease: an unacked message is returned only when its
// channel dies. The adapter therefore enforces AckWait itself - a watchdog
// per subscription treats an expired delivery like Nack(0). Extend renews the
// local lease.
//
// Nack(delay) republishes a copy (with an incremented attempt header) to the
// retry queue with expiration = delay - the queue dead-letters it back into
// the group queue when it expires - and then acks the original. With delay 0
// the copy goes straight back to the group queue. Republishing rather than
// basic.nack(requeue) is what makes the attempt count exact and the delay
// possible. Two caveats: RabbitMQ expires per-message TTLs only at the head
// of a queue, so a long delay queued ahead of a short one delays both
// (head-of-line blocking; the engine's back-off is monotonic per message,
// which keeps this mild); and a crash between the republish and the ack
// yields a duplicate, which consumers de-duplicate by message id.
//
// Messages returned because a channel or connection died are redelivered by
// the broker; quorum queues count those in x-delivery-count, which is added
// to the attempt.
//
// Group configuration (AckWait, MaxDeliver) is held by the process that
// declared it; processes that only subscribe use Options defaults. The
// engine declares every subject on every instance at startup, so in practice
// all instances agree.
//
// Stats: Pending = ready messages in the group and retry queues (passive
// declare); InFlight = deliveries held by this process (AMQP does not expose
// unacked counts without the management plugin); Dead = dead queue depth.
package rabbitq

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/queue"
	"github.com/woliveiras/weft/server/internal/queue/internal/qutil"
)

const (
	exchange      = "weft"
	hdrPrior      = "x-weft-prior"     // deliveries before this copy was published
	hdrReason     = "x-weft-reason"    // dead-letter reason
	hdrDeliveries = "x-delivery-count" // set by quorum queues on broker-side redelivery
)

// ErrLeaseLost is returned when acting on a delivery that was already settled
// (or expired and requeued by the watchdog).
var ErrLeaseLost = errors.New("rabbitq: delivery lease lost")

type Options struct {
	URL        string
	AckWait    time.Duration // default for groups declared by another process
	MaxDeliver int
}

type Broker struct {
	opts Options

	mu       sync.Mutex
	conn     *amqp.Connection
	pubCh    *amqp.Channel
	returns  chan amqp.Return
	cfgs     map[string]queue.SubjectConfig // by subject
	inflight map[string]int64               // by queue name
	closed   bool
	pubMu    sync.Mutex
}

func New(ctx context.Context, o Options) (*Broker, error) {
	if o.URL == "" {
		o.URL = "amqp://guest:guest@localhost:5672/"
	}
	if o.AckWait <= 0 {
		o.AckWait = 10 * time.Minute
	}
	if o.MaxDeliver <= 0 {
		o.MaxDeliver = 5
	}
	b := &Broker{opts: o, cfgs: map[string]queue.SubjectConfig{}, inflight: map[string]int64{}}
	if _, err := b.connection(); err != nil {
		return nil, err
	}
	ch, err := b.channel()
	if err != nil {
		return nil, err
	}
	defer ch.Close()
	if err := ch.ExchangeDeclare(exchange, "topic", true, false, false, false, nil); err != nil {
		return nil, fmt.Errorf("rabbitq: declare exchange: %w", err)
	}
	return b, nil
}

func (b *Broker) Name() string { return "rabbitmq" }

func groupQueue(subject, group string) string { return "weft." + subject + "." + group }
func retryQueue(subject, group string) string { return groupQueue(subject, group) + ":retry" }
func deadQueue(subject, group string) string  { return groupQueue(subject, group) + ":dead" }

// connection returns a live connection, redialling if it was lost.
func (b *Broker) connection() (*amqp.Connection, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil, queue.ErrClosed
	}
	if b.conn != nil && !b.conn.IsClosed() {
		return b.conn, nil
	}
	conn, err := amqp.DialConfig(b.opts.URL, amqp.Config{
		Heartbeat:  10 * time.Second,
		Properties: amqp.Table{"connection_name": "weft"},
	})
	if err != nil {
		return nil, fmt.Errorf("rabbitq: dial: %w", err)
	}
	b.conn = conn
	b.pubCh = nil
	return conn, nil
}

func (b *Broker) channel() (*amqp.Channel, error) {
	conn, err := b.connection()
	if err != nil {
		return nil, err
	}
	return conn.Channel()
}

// publishChannel returns the confirm-mode channel used for all publishes.
// Caller holds b.pubMu.
func (b *Broker) publishChannel() (*amqp.Channel, error) {
	b.mu.Lock()
	ch := b.pubCh
	b.mu.Unlock()
	if ch != nil && !ch.IsClosed() {
		return ch, nil
	}
	ch, err := b.channel()
	if err != nil {
		return nil, err
	}
	if err := ch.Confirm(false); err != nil {
		ch.Close()
		return nil, fmt.Errorf("rabbitq: confirm mode: %w", err)
	}
	returns := ch.NotifyReturn(make(chan amqp.Return, 64))
	b.mu.Lock()
	b.pubCh, b.returns = ch, returns
	b.mu.Unlock()
	return ch, nil
}

// publish sends one confirmed, persistent message. When mandatory is set an
// unroutable message yields queue.ErrNoGroups.
func (b *Broker) publish(ctx context.Context, exch, key string, mandatory bool, p amqp.Publishing) error {
	b.pubMu.Lock()
	defer b.pubMu.Unlock()
	ch, err := b.publishChannel()
	if err != nil {
		return err
	}
	p.DeliveryMode = amqp.Persistent
	dc, err := ch.PublishWithDeferredConfirmWithContext(ctx, exch, key, mandatory, false, p)
	if err != nil {
		return fmt.Errorf("rabbitq: publish: %w", err)
	}
	ok, err := dc.WaitContext(ctx)
	if err != nil {
		return fmt.Errorf("rabbitq: confirm: %w", err)
	}
	// A basic.return precedes the basic.ack of the same message on the wire,
	// and publishes are serialised, so any return for this message is
	// already buffered.
	returned := false
	for {
		select {
		case r := <-b.returns:
			if r.MessageId == p.MessageId {
				returned = true
			}
			continue
		default:
		}
		break
	}
	if returned {
		return queue.ErrNoGroups
	}
	if !ok {
		return errors.New("rabbitq: publish nacked by broker")
	}
	return nil
}

func (b *Broker) Declare(ctx context.Context, cfg queue.SubjectConfig) error {
	if err := qutil.ValidName("subject", cfg.Subject); err != nil {
		return err
	}
	cfg = cfg.WithDefaults()
	ch, err := b.channel()
	if err != nil {
		return err
	}
	defer ch.Close()
	for _, g := range cfg.Groups {
		if err := qutil.ValidName("group", g); err != nil {
			return err
		}
		q := groupQueue(cfg.Subject, g)
		if _, err := ch.QueueDeclare(q, true, false, false, false, amqp.Table{"x-queue-type": "quorum"}); err != nil {
			return fmt.Errorf("rabbitq: declare %s: %w", q, err)
		}
		if err := ch.QueueBind(q, cfg.Subject, exchange, false, nil); err != nil {
			return fmt.Errorf("rabbitq: bind %s: %w", q, err)
		}
		if _, err := ch.QueueDeclare(retryQueue(cfg.Subject, g), true, false, false, false, amqp.Table{
			"x-dead-letter-exchange":    "",
			"x-dead-letter-routing-key": q,
		}); err != nil {
			return fmt.Errorf("rabbitq: declare retry queue: %w", err)
		}
		if _, err := ch.QueueDeclare(deadQueue(cfg.Subject, g), true, false, false, false, nil); err != nil {
			return fmt.Errorf("rabbitq: declare dead queue: %w", err)
		}
	}
	b.mu.Lock()
	b.cfgs[cfg.Subject] = cfg
	b.mu.Unlock()
	return nil
}

func (b *Broker) DeleteGroup(ctx context.Context, subject, group string) error {
	for _, q := range []string{groupQueue(subject, group), retryQueue(subject, group), deadQueue(subject, group)} {
		ch, err := b.channel()
		if err != nil {
			return err
		}
		_, err = ch.QueueDelete(q, false, false, false)
		ch.Close()
		var aerr *amqp.Error
		if err != nil && !(errors.As(err, &aerr) && aerr.Code == amqp.NotFound) {
			return err
		}
	}
	return nil
}

func (b *Broker) Publish(ctx context.Context, subject string, msg *domain.Message) error {
	data, err := qutil.Encode(msg)
	if err != nil {
		return err
	}
	return b.publish(ctx, exchange, subject, true, amqp.Publishing{
		ContentType: "application/json",
		MessageId:   msg.ID,
		Type:        msg.Type,
		Timestamp:   time.Now(),
		Body:        data,
		Headers:     amqp.Table{hdrPrior: int64(0)},
	})
}

func (b *Broker) groupCfg(subject string) (time.Duration, int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if c, ok := b.cfgs[subject]; ok {
		return c.AckWait, c.MaxDeliver
	}
	return b.opts.AckWait, b.opts.MaxDeliver
}

func (b *Broker) Subscribe(ctx context.Context, subject, group, consumer string) (queue.Subscription, error) {
	ch, err := b.channel()
	if err != nil {
		return nil, err
	}
	if _, err := ch.QueueDeclarePassive(groupQueue(subject, group), true, false, false, false, amqp.Table{"x-queue-type": "quorum"}); err != nil {
		return nil, fmt.Errorf("rabbitq: group %s not declared on %s: %w", group, subject, err)
	}
	ackWait, maxDeliver := b.groupCfg(subject)
	s := &sub{
		b: b, ch: ch, subject: subject, group: group, queue: groupQueue(subject, group),
		ackWait: ackWait, maxDeliver: maxDeliver,
		out: map[uint64]*delivery{}, done: make(chan struct{}),
	}
	s.wg.Add(1)
	go s.watchdog()
	return s, nil
}

func (b *Broker) depth(name string) int64 {
	ch, err := b.channel()
	if err != nil {
		return 0
	}
	defer ch.Close()
	q, err := ch.QueueDeclarePassive(name, true, false, false, false, nil)
	if err != nil {
		return 0
	}
	return int64(q.Messages)
}

func (b *Broker) Stats(ctx context.Context, subject, group string) (queue.Stats, error) {
	if _, err := b.connection(); err != nil {
		return queue.Stats{}, err
	}
	q := groupQueue(subject, group)
	b.mu.Lock()
	inflight := b.inflight[q]
	b.mu.Unlock()
	return queue.Stats{
		Pending:  b.depth(q) + b.depth(retryQueue(subject, group)),
		InFlight: inflight,
		Dead:     b.depth(deadQueue(subject, group)),
	}, nil
}

func (b *Broker) addInflight(q string, n int64) {
	b.mu.Lock()
	b.inflight[q] += n
	b.mu.Unlock()
}

func (b *Broker) Ping(ctx context.Context) error {
	_, err := b.connection()
	return err
}

func (b *Broker) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil
	}
	b.closed = true
	if b.conn != nil {
		return b.conn.Close()
	}
	return nil
}

type sub struct {
	b          *Broker
	chMu       sync.Mutex
	ch         *amqp.Channel
	subject    string
	group      string
	queue      string
	ackWait    time.Duration
	maxDeliver int

	mu   sync.Mutex
	out  map[uint64]*delivery // outstanding deliveries by delivery tag
	once sync.Once
	done chan struct{}
	wg   sync.WaitGroup
}

func (s *sub) channel() (*amqp.Channel, error) {
	s.chMu.Lock()
	defer s.chMu.Unlock()
	if s.ch != nil && !s.ch.IsClosed() {
		return s.ch, nil
	}
	// The old channel died: the broker requeued its unacked messages, so the
	// outstanding deliveries are void.
	s.mu.Lock()
	for tag, d := range s.out {
		d.settled = true
		delete(s.out, tag)
		s.b.addInflight(s.queue, -1)
	}
	s.mu.Unlock()
	ch, err := s.b.channel()
	if err != nil {
		return nil, err
	}
	s.ch = ch
	return ch, nil
}

func (s *sub) Fetch(ctx context.Context, max int, wait time.Duration) ([]queue.Delivery, error) {
	if max <= 0 {
		max = 1
	}
	deadline := time.Now().Add(wait)
	bo := qutil.Backoff{Min: 50 * time.Millisecond, Max: 500 * time.Millisecond}
	for {
		select {
		case <-s.done:
			return nil, queue.ErrClosed
		default:
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		ch, err := s.channel()
		if err != nil {
			return nil, err
		}
		var out []queue.Delivery
		for len(out) < max {
			m, ok, err := ch.Get(s.queue, false)
			if err != nil {
				if len(out) > 0 {
					return out, nil
				}
				return nil, fmt.Errorf("rabbitq: get: %w", err)
			}
			if !ok {
				break
			}
			if d := s.accept(ctx, m); d != nil {
				out = append(out, d)
			}
		}
		if len(out) > 0 {
			return out, nil
		}
		now := time.Now()
		if !now.Before(deadline) {
			return nil, nil
		}
		d := min(bo.Next(), deadline.Sub(now))
		t := time.NewTimer(d)
		select {
		case <-ctx.Done():
			t.Stop()
			return nil, ctx.Err()
		case <-s.done:
			t.Stop()
			return nil, queue.ErrClosed
		case <-t.C:
		}
	}
}

func headerInt(h amqp.Table, k string) int {
	switch v := h[k].(type) {
	case int64:
		return int(v)
	case int32:
		return int(v)
	case int:
		return v
	case int16:
		return int(v)
	case int8:
		return int(v)
	case uint8:
		return int(v)
	case string:
		i, _ := strconv.Atoi(v)
		return i
	}
	return 0
}

func (s *sub) accept(ctx context.Context, m amqp.Delivery) *delivery {
	attempt := headerInt(m.Headers, hdrPrior) + headerInt(m.Headers, hdrDeliveries) + 1
	d := &delivery{s: s, raw: m, attempt: attempt, leaseUntil: time.Now().Add(s.ackWait)}
	if attempt > s.maxDeliver {
		_ = d.deadLetter(ctx, "max deliveries exceeded")
		return nil
	}
	dm, err := qutil.Decode(m.Body, attempt)
	if err != nil {
		_ = d.deadLetter(ctx, err.Error())
		return nil
	}
	d.msg = dm
	s.mu.Lock()
	s.out[m.DeliveryTag] = d
	s.mu.Unlock()
	s.b.addInflight(s.queue, 1)
	return d
}

// watchdog enforces AckWait: an expired delivery is retried as if nacked.
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
		for _, d := range s.out {
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
		s.chMu.Lock()
		if s.ch != nil {
			_ = s.ch.Close() // the broker requeues anything still unacked
		}
		s.chMu.Unlock()
		s.mu.Lock()
		s.b.addInflight(s.queue, -int64(len(s.out)))
		s.out = map[uint64]*delivery{}
		s.mu.Unlock()
	})
	return nil
}

type delivery struct {
	s          *sub
	mu         sync.Mutex
	raw        amqp.Delivery
	msg        *domain.Message
	attempt    int
	leaseUntil time.Time
	settled    bool
}

func (d *delivery) Message() *domain.Message { return d.msg }
func (d *delivery) Attempt() int             { return d.attempt }

// settle runs fn (which must ack the original) exactly once.
func (d *delivery) settle(fn func() error) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.settled {
		return ErrLeaseLost
	}
	if err := fn(); err != nil {
		return err
	}
	d.settled = true
	d.s.mu.Lock()
	if _, ok := d.s.out[d.raw.DeliveryTag]; ok {
		delete(d.s.out, d.raw.DeliveryTag)
		d.s.b.addInflight(d.s.queue, -1)
	}
	d.s.mu.Unlock()
	return nil
}

func (d *delivery) Ack(ctx context.Context) error {
	return d.settle(func() error { return d.raw.Ack(false) })
}

func (d *delivery) copyOf(prior int, extra amqp.Table) amqp.Publishing {
	h := amqp.Table{hdrPrior: int64(prior)}
	for k, v := range extra {
		h[k] = v
	}
	return amqp.Publishing{
		ContentType: d.raw.ContentType,
		MessageId:   d.raw.MessageId,
		Type:        d.raw.Type,
		Timestamp:   d.raw.Timestamp,
		Body:        d.raw.Body,
		Headers:     h,
	}
}

func (d *delivery) Nack(ctx context.Context, delay time.Duration) error {
	if d.attempt >= d.s.maxDeliver {
		return d.Term(ctx, "max deliveries exceeded")
	}
	return d.settle(func() error {
		p := d.copyOf(d.attempt, nil)
		key := d.s.queue
		if delay > 0 {
			key = retryQueue(d.s.subject, d.s.group)
			p.Expiration = strconv.FormatInt(max(delay.Milliseconds(), 1), 10)
		}
		if err := d.s.b.publish(ctx, "", key, false, p); err != nil {
			return err
		}
		return d.raw.Ack(false)
	})
}

func (d *delivery) Term(ctx context.Context, reason string) error {
	return d.deadLetter(ctx, reason)
}

func (d *delivery) deadLetter(ctx context.Context, reason string) error {
	return d.settle(func() error {
		p := d.copyOf(d.attempt, amqp.Table{hdrReason: reason})
		if err := d.s.b.publish(ctx, "", deadQueue(d.s.subject, d.s.group), false, p); err != nil {
			return err
		}
		return d.raw.Ack(false)
	})
}

func (d *delivery) Extend(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.settled {
		return ErrLeaseLost
	}
	d.leaseUntil = time.Now().Add(d.s.ackWait)
	return nil
}

var _ queue.Broker = (*Broker)(nil)
