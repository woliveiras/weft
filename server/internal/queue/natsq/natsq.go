// Package natsq is the NATS JetStream broker - the platform default.
//
// # Mapping
//
//	subject "<graph>.<channel>"  -> stream WEFT_<token(subject)>, subject "weft.<graph>.<channel>"
//	group                        -> durable pull consumer <token(group)> (explicit ack,
//	                                AckWait = lease, MaxDeliver, DeliverPolicy=new)
//	dead letters                 -> stream WEFT_DLQ_<token(graph)>,
//	                                subject "weft.<graph>.__dlq.<token(subject)>.<token(group)>"
//
// # Why one stream per channel (not one per graph)
//
// Retention, limits, replication and placement are stream properties. One
// stream per channel lets a high-volume channel get its own limits, lets a
// channel be deleted without touching its neighbours, and keeps a slow
// consumer's backlog from counting against an unrelated channel's limits.
// The cost is more streams (one per channel, typically tens to hundreds per
// cluster), which JetStream handles comfortably. Graph-level dead-letter
// streams keep poison messages of a whole graph in one place for triage.
//
// # Semantics
//
// Retention is LimitsPolicy with MaxAge = SubjectConfig.Retention, so acked
// messages remain available for audit/replay until they age out, while
// each group's consumer tracks its own position. New groups use
// DeliverPolicy=new, matching the contract that a group receives messages
// published after it was declared. Publishes carry Nats-Msg-Id = message id,
// so JetStream's duplicate window (2 min) makes producer retries idempotent.
//
// Nack uses NakWithDelay; Extend uses InProgress (resets AckWait). The
// consumer BackOff list is deliberately not used: it replaces AckWait for
// redeliveries, i.e. it would shorten the lease of long-running turns; the
// engine passes explicit delays to Nack instead.
//
// Dead letters: Term (and a Nack on the last allowed attempt) republishes the
// message to the graph DLQ with Weft-Reason/Group/Subject headers and
// terms it. Messages that exhaust MaxDeliver by lease expiry (the consumer
// crashed every time) are caught through the MAX_DELIVERIES advisory and
// copied to the DLQ as well; DLQ publishes are de-duplicated by message id
// and group, so several platform instances receiving the advisory produce
// one DLQ entry.
//
// ErrNoGroups is returned when the subject has no stream (never declared).
// A declared subject whose groups were all deleted still accepts publishes
// (they are retained until MaxAge) - JetStream cannot cheaply tell.
package natsq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/queue"
	"github.com/woliveiras/weft/server/internal/queue/internal/qutil"
)

const subjectPrefix = "weft."

// Options configure the adapter.
type Options struct {
	URL       string
	Name      string // connection name shown in NATS monitoring
	Replicas  int    // stream replicas (1 for a single server, 3 for a cluster)
	CredsFile string
	// Advisories enables the MAX_DELIVERIES advisory listener (default on).
	DisableAdvisories bool
}

type Broker struct {
	nc   *nats.Conn
	js   jetstream.JetStream
	opts Options

	mu       sync.Mutex
	cfgs     map[string]queue.SubjectConfig // by subject, for dead-letter decisions
	advisory *nats.Subscription
	closed   bool
}

// New connects to NATS and returns a JetStream broker.
func New(ctx context.Context, o Options) (*Broker, error) {
	if o.URL == "" {
		o.URL = nats.DefaultURL
	}
	if o.Name == "" {
		o.Name = "weft"
	}
	if o.Replicas <= 0 {
		o.Replicas = 1
	}
	nopts := []nats.Option{
		nats.Name(o.Name),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(time.Second),
		nats.RetryOnFailedConnect(true),
	}
	if o.CredsFile != "" {
		nopts = append(nopts, nats.UserCredentials(o.CredsFile))
	}
	nc, err := nats.Connect(o.URL, nopts...)
	if err != nil {
		return nil, fmt.Errorf("natsq: connect %s: %w", o.URL, err)
	}
	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("natsq: jetstream: %w", err)
	}
	b := &Broker{nc: nc, js: js, opts: o, cfgs: map[string]queue.SubjectConfig{}}
	if !o.DisableAdvisories {
		sub, err := nc.Subscribe("$JS.EVENT.ADVISORY.CONSUMER.MAX_DELIVERIES.*.*", b.onMaxDeliveries)
		if err == nil {
			b.advisory = sub
		}
	}
	return b, nil
}

func (b *Broker) Name() string { return "nats" }

func streamName(subject string) string  { return "WEFT_" + qutil.Token(subject) }
func dlqStream(graph string) string     { return "WEFT_DLQ_" + qutil.Token(graph) }
func natsSubject(subject string) string { return subjectPrefix + subject }
func consumerName(group string) string  { return qutil.Token(group) }
func dlqSubject(subject, group string) string {
	return subjectPrefix + qutil.Graph(subject) + ".__dlq." + qutil.Token(subject) + "." + qutil.Token(group)
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
	if _, err := b.js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:        streamName(cfg.Subject),
		Description: "weft channel " + cfg.Subject,
		Subjects:    []string{natsSubject(cfg.Subject)},
		Retention:   jetstream.LimitsPolicy,
		MaxAge:      cfg.Retention,
		Storage:     jetstream.FileStorage,
		Replicas:    b.opts.Replicas,
		Duplicates:  2 * time.Minute,
	}); err != nil {
		return fmt.Errorf("natsq: stream for %s: %w", cfg.Subject, err)
	}
	graph := qutil.Graph(cfg.Subject)
	if _, err := b.js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:        dlqStream(graph),
		Description: "weft dead letters for graph " + graph,
		Subjects:    []string{subjectPrefix + graph + ".__dlq.>"},
		Retention:   jetstream.LimitsPolicy,
		MaxAge:      30 * 24 * time.Hour,
		Storage:     jetstream.FileStorage,
		Replicas:    b.opts.Replicas,
		Duplicates:  10 * time.Minute,
	}); err != nil {
		return fmt.Errorf("natsq: dlq stream for %s: %w", graph, err)
	}
	for _, g := range cfg.Groups {
		if _, err := b.js.CreateOrUpdateConsumer(ctx, streamName(cfg.Subject), jetstream.ConsumerConfig{
			Durable:       consumerName(g),
			Description:   g,
			FilterSubject: natsSubject(cfg.Subject),
			AckPolicy:     jetstream.AckExplicitPolicy,
			AckWait:       cfg.AckWait,
			MaxDeliver:    cfg.MaxDeliver,
			DeliverPolicy: jetstream.DeliverNewPolicy,
			MaxAckPending: 10000,
		}); err != nil {
			return fmt.Errorf("natsq: consumer %s on %s: %w", g, cfg.Subject, err)
		}
	}
	b.mu.Lock()
	b.cfgs[cfg.Subject] = cfg
	b.mu.Unlock()
	return nil
}

func (b *Broker) DeleteGroup(ctx context.Context, subject, group string) error {
	err := b.js.DeleteConsumer(ctx, streamName(subject), consumerName(group))
	if err != nil && !errors.Is(err, jetstream.ErrConsumerNotFound) && !errors.Is(err, jetstream.ErrStreamNotFound) {
		return err
	}
	return nil
}

func (b *Broker) Publish(ctx context.Context, subject string, msg *domain.Message) error {
	data, err := qutil.Encode(msg)
	if err != nil {
		return err
	}
	m := nats.NewMsg(natsSubject(subject))
	m.Data = data
	m.Header.Set("Weft-Type", msg.Type)
	_, err = b.js.PublishMsg(ctx, m, jetstream.WithMsgID(msg.ID))
	if err != nil {
		if errors.Is(err, jetstream.ErrNoStreamResponse) || errors.Is(err, nats.ErrNoResponders) {
			return queue.ErrNoGroups
		}
		return fmt.Errorf("natsq: publish %s: %w", subject, err)
	}
	return nil
}

func (b *Broker) Subscribe(ctx context.Context, subject, group, consumer string) (queue.Subscription, error) {
	c, err := b.js.Consumer(ctx, streamName(subject), consumerName(group))
	if err != nil {
		return nil, fmt.Errorf("natsq: group %s on %s: %w", group, subject, err)
	}
	maxDeliver := 5
	if info := c.CachedInfo(); info != nil && info.Config.MaxDeliver > 0 {
		maxDeliver = info.Config.MaxDeliver
	}
	return &sub{b: b, c: c, subject: subject, group: group, maxDeliver: maxDeliver, done: make(chan struct{})}, nil
}

func (b *Broker) Stats(ctx context.Context, subject, group string) (queue.Stats, error) {
	c, err := b.js.Consumer(ctx, streamName(subject), consumerName(group))
	if err != nil {
		if errors.Is(err, jetstream.ErrConsumerNotFound) || errors.Is(err, jetstream.ErrStreamNotFound) {
			return queue.Stats{}, nil
		}
		return queue.Stats{}, err
	}
	info, err := c.Info(ctx)
	if err != nil {
		return queue.Stats{}, err
	}
	st := queue.Stats{Pending: int64(info.NumPending), InFlight: int64(info.NumAckPending)}
	if s, err := b.js.Stream(ctx, dlqStream(qutil.Graph(subject))); err == nil {
		subj := dlqSubject(subject, group)
		if si, err := s.Info(ctx, jetstream.WithSubjectFilter(subj)); err == nil {
			st.Dead = int64(si.State.Subjects[subj])
		}
	}
	return st, nil
}

func (b *Broker) Ping(ctx context.Context) error {
	if b.nc.Status() != nats.CONNECTED {
		return fmt.Errorf("natsq: connection %s", b.nc.Status())
	}
	_, err := b.js.AccountInfo(ctx)
	return err
}

func (b *Broker) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil
	}
	b.closed = true
	if b.advisory != nil {
		_ = b.advisory.Unsubscribe()
	}
	b.nc.Close()
	return nil
}

// deadLetter copies a message to the graph DLQ. Idempotent per (message, group).
func (b *Broker) deadLetter(ctx context.Context, subject, group string, data []byte, msgID, reason string) error {
	m := nats.NewMsg(dlqSubject(subject, group))
	m.Data = data
	m.Header.Set("Weft-Reason", reason)
	m.Header.Set("Weft-Group", group)
	m.Header.Set("Weft-Subject", subject)
	_, err := b.js.PublishMsg(ctx, m, jetstream.WithMsgID("dlq:"+group+":"+msgID))
	return err
}

// onMaxDeliveries handles the JetStream advisory emitted when a message hits
// MaxDeliver through lease expiry, copying it to the DLQ.
func (b *Broker) onMaxDeliveries(m *nats.Msg) {
	var adv struct {
		Stream    string `json:"stream"`
		Consumer  string `json:"consumer"`
		StreamSeq uint64 `json:"stream_seq"`
	}
	if json.Unmarshal(m.Data, &adv) != nil || !strings.HasPrefix(adv.Stream, "WEFT_") || strings.HasPrefix(adv.Stream, "WEFT_DLQ_") {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s, err := b.js.Stream(ctx, adv.Stream)
	if err != nil {
		return
	}
	raw, err := s.GetMsg(ctx, adv.StreamSeq)
	if err != nil {
		return
	}
	c, err := s.Consumer(ctx, adv.Consumer)
	if err != nil {
		return
	}
	group := c.CachedInfo().Config.Description
	subject := strings.TrimPrefix(raw.Subject, subjectPrefix)
	var env struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(raw.Data, &env)
	_ = b.deadLetter(ctx, subject, group, raw.Data, env.ID, "max deliveries exceeded")
}

type sub struct {
	b          *Broker
	c          jetstream.Consumer
	subject    string
	group      string
	maxDeliver int
	once       sync.Once
	done       chan struct{}
}

func (s *sub) Fetch(ctx context.Context, max int, wait time.Duration) ([]queue.Delivery, error) {
	if max <= 0 {
		max = 1
	}
	select {
	case <-s.done:
		return nil, queue.ErrClosed
	default:
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var batch jetstream.MessageBatch
	var err error
	if wait <= 0 {
		batch, err = s.c.FetchNoWait(max)
	} else {
		fctx, cancel := context.WithTimeout(ctx, wait)
		defer cancel()
		// Close() cancels in-progress fetches.
		go func() {
			select {
			case <-s.done:
				cancel()
			case <-fctx.Done():
			}
		}()
		batch, err = s.c.Fetch(max, jetstream.FetchContext(fctx))
	}
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("natsq: fetch: %w", err)
	}
	var out []queue.Delivery
	for m := range batch.Messages() {
		md, err := m.Metadata()
		attempt := 1
		if err == nil {
			attempt = int(md.NumDelivered)
		}
		dm, derr := qutil.Decode(m.Data(), attempt)
		if derr != nil {
			_ = s.b.deadLetter(context.Background(), s.subject, s.group, m.Data(), "seq-"+seqOf(md), derr.Error())
			_ = m.TermWithReason(derr.Error())
			continue
		}
		out = append(out, &delivery{s: s, m: m, msg: dm, attempt: attempt})
	}
	if berr := batch.Error(); berr != nil && !errors.Is(berr, context.DeadlineExceeded) && !errors.Is(berr, nats.ErrTimeout) && !errors.Is(berr, jetstream.ErrNoMessages) {
		if ctx.Err() != nil {
			// Return what we have leased: dropping it would only delay it by AckWait.
			for _, d := range out {
				_ = d.Nack(context.Background(), 0)
			}
			return nil, ctx.Err()
		}
		select {
		case <-s.done:
			return out, nil
		default:
		}
		if len(out) == 0 && !errors.Is(berr, context.Canceled) {
			return nil, fmt.Errorf("natsq: fetch: %w", berr)
		}
	}
	if ctx.Err() != nil && len(out) == 0 {
		return nil, ctx.Err()
	}
	return out, nil
}

func seqOf(md *jetstream.MsgMetadata) string {
	if md == nil {
		return "unknown"
	}
	return strconv.FormatUint(md.Sequence.Stream, 10)
}

func (s *sub) Close() error {
	s.once.Do(func() { close(s.done) })
	return nil
}

type delivery struct {
	s       *sub
	m       jetstream.Msg
	msg     *domain.Message
	attempt int
}

func (d *delivery) Message() *domain.Message { return d.msg }
func (d *delivery) Attempt() int             { return d.attempt }

func (d *delivery) Ack(ctx context.Context) error { return d.m.DoubleAck(ctx) }

func (d *delivery) Nack(ctx context.Context, delay time.Duration) error {
	if d.attempt >= d.s.maxDeliver {
		return d.Term(ctx, "max deliveries exceeded")
	}
	if delay <= 0 {
		return d.m.Nak()
	}
	return d.m.NakWithDelay(delay)
}

func (d *delivery) Term(ctx context.Context, reason string) error {
	if reason == "" {
		reason = "terminated"
	}
	if err := d.s.b.deadLetter(ctx, d.s.subject, d.s.group, d.m.Data(), d.msg.ID, reason); err != nil {
		return fmt.Errorf("natsq: dead-letter: %w", err)
	}
	return d.m.TermWithReason(reason)
}

func (d *delivery) Extend(ctx context.Context) error { return d.m.InProgress() }

var _ queue.Broker = (*Broker)(nil)
