// Package redisq is a Redis Streams broker (Redis >= 7.0).
//
// # Mapping
//
//	subject  -> stream  weft:{<subject>}          (XADD, field "m" = message JSON)
//	group    -> XGROUP  <group> created at "$"         (receives messages published after Declare)
//	config   -> hash    weft:{<subject>}:cfg       (maxDeliver, ackWaitMs, retentionMs)
//	groups   -> set     weft:{<subject>}:groups    (publish refuses when empty)
//	dead     -> stream  weft:{<subject>}:dead:<group>
//
// All keys of a subject share the {<subject>} hash tag, so the adapter works
// on Redis Cluster and every multi-key operation stays on one shard.
//
// # Leases, retries and delays
//
// A delivery is an entry in the group's pending entries list (PEL). Fetch
// first runs XAUTOCLAIM with min-idle = AckWait, which hands expired leases
// to the caller and increments their delivery counter, then XREADGROUP ">"
// for new entries. Extend is XCLAIM ... JUSTID (resets idle, keeps the
// counter). Nack(delay) is XCLAIM ... IDLE (AckWait - delay) JUSTID: it makes
// the entry look as if its lease will expire after `delay`, so the next
// XAUTOCLAIM picks it up. This keeps retries entirely inside Redis (no
// side ZSET, no re-publish, the entry id and counter are preserved), at the
// price that a Nack delay is capped at AckWait - with the default 10m lease
// and the engine's sub-minute back-off that cap is never reached.
//
// Retention trims the stream with XADD MINID ~ (now - retention). Trimming
// can remove an entry still pending for a slow group; Redis drops such
// entries from the PEL during XAUTOCLAIM. Retention must therefore exceed
// the longest expected processing lag (default 7 days).
//
// Stats use XINFO GROUPS: Pending = lag (entries never delivered to the
// group), InFlight = PEL size (leased, including delayed retries),
// Dead = XLEN of the group's dead stream.
package redisq

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/queue"
	"github.com/woliveiras/weft/server/internal/queue/internal/qutil"
)

// ErrLeaseLost is returned when acking an entry no longer in the PEL.
var ErrLeaseLost = errors.New("redisq: delivery lease lost")

type Broker struct {
	rdb    redis.UniversalClient
	once   sync.Once
	closed chan struct{}
}

// New connects using a redis:// or rediss:// URL.
func New(ctx context.Context, url string) (*Broker, error) {
	if url == "" {
		url = "redis://localhost:6379/0"
	}
	opt, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("redisq: %w", err)
	}
	return NewWithClient(redis.NewClient(opt)), nil
}

// NewWithClient wraps an existing client (cluster, sentinel, ...).
func NewWithClient(c redis.UniversalClient) *Broker {
	return &Broker{rdb: c, closed: make(chan struct{})}
}

func (b *Broker) Name() string { return "redis" }

func streamKey(s string) string  { return "weft:{" + s + "}" }
func cfgKey(s string) string     { return streamKey(s) + ":cfg" }
func groupsKey(s string) string  { return streamKey(s) + ":groups" }
func deadKey(s, g string) string { return streamKey(s) + ":dead:" + g }

var publishScript = redis.NewScript(`
if redis.call('SCARD', KEYS[2]) == 0 then
  return redis.error_reply('NOGROUPS')
end
local ret = tonumber(redis.call('HGET', KEYS[3], 'retentionMs') or '0')
if ret and ret > 0 then
  local minid = tonumber(ARGV[1]) - ret
  if minid < 0 then minid = 0 end
  return redis.call('XADD', KEYS[1], 'MINID', '~', tostring(minid), '*', 'm', ARGV[2])
end
return redis.call('XADD', KEYS[1], '*', 'm', ARGV[2])
`)

func (b *Broker) Declare(ctx context.Context, cfg queue.SubjectConfig) error {
	if err := qutil.ValidName("subject", cfg.Subject); err != nil {
		return err
	}
	cfg = cfg.WithDefaults()
	if err := b.rdb.HSet(ctx, cfgKey(cfg.Subject),
		"maxDeliver", cfg.MaxDeliver,
		"ackWaitMs", cfg.AckWait.Milliseconds(),
		"retentionMs", cfg.Retention.Milliseconds()).Err(); err != nil {
		return fmt.Errorf("redisq: declare: %w", err)
	}
	for _, g := range cfg.Groups {
		if err := qutil.ValidName("group", g); err != nil {
			return err
		}
		err := b.rdb.XGroupCreateMkStream(ctx, streamKey(cfg.Subject), g, "$").Err()
		if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
			return fmt.Errorf("redisq: create group %s: %w", g, err)
		}
		if err := b.rdb.SAdd(ctx, groupsKey(cfg.Subject), g).Err(); err != nil {
			return err
		}
	}
	return nil
}

func (b *Broker) DeleteGroup(ctx context.Context, subject, group string) error {
	err := b.rdb.XGroupDestroy(ctx, streamKey(subject), group).Err()
	if err != nil && !strings.Contains(err.Error(), "no such key") {
		return err
	}
	b.rdb.SRem(ctx, groupsKey(subject), group)
	b.rdb.Del(ctx, deadKey(subject, group))
	return nil
}

func (b *Broker) Publish(ctx context.Context, subject string, msg *domain.Message) error {
	data, err := qutil.Encode(msg)
	if err != nil {
		return err
	}
	err = publishScript.Run(ctx, b.rdb,
		[]string{streamKey(subject), groupsKey(subject), cfgKey(subject)},
		time.Now().UnixMilli(), data).Err()
	if err != nil {
		if strings.Contains(err.Error(), "NOGROUPS") {
			return queue.ErrNoGroups
		}
		return fmt.Errorf("redisq: publish: %w", err)
	}
	return nil
}

type subCfg struct {
	ackWait    time.Duration
	maxDeliver int
}

func (b *Broker) loadCfg(ctx context.Context, subject string) (subCfg, error) {
	c := subCfg{ackWait: 10 * time.Minute, maxDeliver: 5}
	vals, err := b.rdb.HGetAll(ctx, cfgKey(subject)).Result()
	if err != nil {
		return c, err
	}
	if v, err := strconv.ParseInt(vals["ackWaitMs"], 10, 64); err == nil && v > 0 {
		c.ackWait = time.Duration(v) * time.Millisecond
	}
	if v, err := strconv.Atoi(vals["maxDeliver"]); err == nil && v > 0 {
		c.maxDeliver = v
	}
	return c, nil
}

func (b *Broker) Subscribe(ctx context.Context, subject, group, consumer string) (queue.Subscription, error) {
	ok, err := b.rdb.SIsMember(ctx, groupsKey(subject), group).Result()
	if err != nil {
		return nil, fmt.Errorf("redisq: subscribe: %w", err)
	}
	if !ok {
		return nil, fmt.Errorf("redisq: group %s not declared on %s", group, subject)
	}
	cfg, err := b.loadCfg(ctx, subject)
	if err != nil {
		return nil, err
	}
	if consumer == "" {
		consumer = "c-" + domain.NewID("")
	}
	return &sub{b: b, subject: subject, group: group, consumer: consumer, cfg: cfg, done: make(chan struct{})}, nil
}

func (b *Broker) Stats(ctx context.Context, subject, group string) (queue.Stats, error) {
	groups, err := b.rdb.XInfoGroups(ctx, streamKey(subject)).Result()
	if err != nil {
		if strings.Contains(err.Error(), "no such key") {
			return queue.Stats{}, nil
		}
		return queue.Stats{}, err
	}
	var st queue.Stats
	for _, g := range groups {
		if g.Name == group {
			st.Pending = g.Lag
			st.InFlight = g.Pending
		}
	}
	if st.Pending < 0 {
		st.Pending = 0
	}
	st.Dead, _ = b.rdb.XLen(ctx, deadKey(subject, group)).Result()
	return st, nil
}

func (b *Broker) Ping(ctx context.Context) error { return b.rdb.Ping(ctx).Err() }

func (b *Broker) Close() error {
	var err error
	b.once.Do(func() {
		close(b.closed)
		err = b.rdb.Close()
	})
	return err
}

type sub struct {
	b        *Broker
	subject  string
	group    string
	consumer string
	cfg      subCfg
	once     sync.Once
	done     chan struct{}
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
		case <-s.b.closed:
			return nil, queue.ErrClosed
		default:
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		out, err := s.reclaim(ctx, max)
		if err != nil {
			return nil, err
		}
		if len(out) > 0 {
			return out, nil
		}
		remaining := time.Until(deadline)
		block := time.Duration(-1) // no BLOCK argument: return immediately
		if remaining >= time.Millisecond {
			// Wake at least every second to pick up expired leases and
			// delayed retries, which XREADGROUP ">" does not see.
			block = min(remaining, time.Second)
		}
		res, err := s.b.rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    s.group,
			Consumer: s.consumer,
			Streams:  []string{streamKey(s.subject), ">"},
			Count:    int64(max),
			Block:    block,
		}).Result()
		if err != nil && !errors.Is(err, redis.Nil) {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, fmt.Errorf("redisq: read: %w", err)
		}
		for _, st := range res {
			for _, m := range st.Messages {
				if d := s.toDelivery(ctx, m, 1); d != nil {
					out = append(out, d)
				}
			}
		}
		if len(out) > 0 || time.Until(deadline) <= 0 {
			return out, nil
		}
	}
}

// reclaim takes over entries whose lease expired (or whose Nack delay passed).
func (s *sub) reclaim(ctx context.Context, max int) ([]queue.Delivery, error) {
	msgs, _, err := s.b.rdb.XAutoClaim(ctx, &redis.XAutoClaimArgs{
		Stream:   streamKey(s.subject),
		Group:    s.group,
		Consumer: s.consumer,
		MinIdle:  s.cfg.ackWait,
		Start:    "0-0",
		Count:    int64(max),
	}).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("redisq: autoclaim: %w", err)
	}
	var out []queue.Delivery
	for _, m := range msgs {
		attempt := 2
		if p, err := s.b.rdb.XPendingExt(ctx, &redis.XPendingExtArgs{
			Stream: streamKey(s.subject), Group: s.group, Start: m.ID, End: m.ID, Count: 1,
		}).Result(); err == nil && len(p) == 1 {
			attempt = int(p[0].RetryCount)
		}
		if attempt > s.cfg.maxDeliver {
			s.deadLetter(ctx, m.ID, fmt.Sprint(m.Values["m"]), "max deliveries exceeded")
			continue
		}
		if d := s.toDelivery(ctx, m, attempt); d != nil {
			out = append(out, d)
		}
	}
	return out, nil
}

func (s *sub) toDelivery(ctx context.Context, m redis.XMessage, attempt int) queue.Delivery {
	raw, _ := m.Values["m"].(string)
	dm, err := qutil.Decode([]byte(raw), attempt)
	if err != nil {
		s.deadLetter(ctx, m.ID, raw, err.Error())
		return nil
	}
	return &delivery{s: s, id: m.ID, raw: raw, msg: dm, attempt: attempt}
}

func (s *sub) deadLetter(ctx context.Context, id, raw, reason string) error {
	_, err := s.b.rdb.TxPipelined(ctx, func(p redis.Pipeliner) error {
		p.XAdd(ctx, &redis.XAddArgs{Stream: deadKey(s.subject, s.group), Values: map[string]any{
			"m": raw, "reason": reason, "group": s.group, "id": id,
		}})
		p.XAck(ctx, streamKey(s.subject), s.group, id)
		return nil
	})
	return err
}

func (s *sub) Close() error {
	s.once.Do(func() { close(s.done) })
	return nil
}

type delivery struct {
	s       *sub
	id      string
	raw     string
	msg     *domain.Message
	attempt int
}

func (d *delivery) Message() *domain.Message { return d.msg }
func (d *delivery) Attempt() int             { return d.attempt }

func (d *delivery) Ack(ctx context.Context) error {
	n, err := d.s.b.rdb.XAck(ctx, streamKey(d.s.subject), d.s.group, d.id).Result()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrLeaseLost
	}
	return nil
}

func (d *delivery) Nack(ctx context.Context, delay time.Duration) error {
	if d.attempt >= d.s.cfg.maxDeliver {
		return d.Term(ctx, "max deliveries exceeded")
	}
	idle := d.s.cfg.ackWait - delay
	if idle < 0 {
		idle = 0
	}
	return d.s.b.rdb.Do(ctx, "XCLAIM", streamKey(d.s.subject), d.s.group, d.s.consumer, 0, d.id,
		"IDLE", idle.Milliseconds(), "JUSTID").Err()
}

func (d *delivery) Term(ctx context.Context, reason string) error {
	return d.s.deadLetter(ctx, d.id, d.raw, reason)
}

func (d *delivery) Extend(ctx context.Context) error {
	ids, err := d.s.b.rdb.XClaimJustID(ctx, &redis.XClaimArgs{
		Stream: streamKey(d.s.subject), Group: d.s.group, Consumer: d.s.consumer, MinIdle: 0, Messages: []string{d.id},
	}).Result()
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return ErrLeaseLost
	}
	return nil
}

var _ queue.Broker = (*Broker)(nil)
