// Package queue defines the broker abstraction every transport implements.
//
// # Model
//
// A Subject is a durable, append-only stream of messages (one per channel:
// "<graph>.<channel>"). A Group is a named cursor over a subject: messages
// published to the subject are delivered to every group, and within a group
// each message goes to exactly one consumer. All channel semantics are
// expressed with that one primitive:
//
//	queue      group = "<channel>"                    every consumer competes
//	topic      group = "<channel>.<node>"             copy per node, replicas compete
//	fanout     group = "<channel>.<node>"             same delivery, static targets
//	broadcast  group = "<channel>.<node>.<replica>"   copy per replica
//
// Groups must be Declared before messages are published, because brokers
// such as RabbitMQ and the filesystem adapter only deliver to groups that
// exist at publish time. The engine declares every group when a graph is
// deployed or changed.
//
// # Delivery guarantees
//
// At-least-once. A fetched delivery is invisible to other consumers of the
// group until it is Acked, Nacked, or its AckWait lease expires, after which
// it is redelivered with Attempt incremented. After MaxDeliver attempts the
// adapter dead-letters it (Term). Consumers deduplicate by Message.ID, which
// turns at-least-once delivery into effectively-once processing.
//
// Fetch is pull-based on purpose: an agent takes a turn only when it is
// ready, which gives natural back-pressure and maps to event-driven work
// without a push buffer that could hold messages hostage in a busy replica.
package queue

import (
	"context"
	"errors"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
)

// ErrClosed is returned by operations on a closed broker or subscriber.
var ErrClosed = errors.New("queue: closed")

// ErrNoGroups is returned by Publish when the subject has no declared group
// (the message would be lost on brokers that only deliver to existing groups).
var ErrNoGroups = errors.New("queue: subject has no declared groups")

// SubjectConfig describes a subject and the groups that consume it.
type SubjectConfig struct {
	Subject    string        // "<graph>.<channel>", [A-Za-z0-9_.-]
	Groups     []string      // consumer groups that must receive every message
	MaxDeliver int           // attempts before dead-lettering (default 5)
	AckWait    time.Duration // lease on an un-acked delivery (default 10m)
	Retention  time.Duration // how long acked history is kept where the broker supports it (default 7d)
}

func (c SubjectConfig) WithDefaults() SubjectConfig {
	if c.MaxDeliver <= 0 {
		c.MaxDeliver = 5
	}
	if c.AckWait <= 0 {
		c.AckWait = 10 * time.Minute
	}
	if c.Retention <= 0 {
		c.Retention = 7 * 24 * time.Hour
	}
	return c
}

// Broker is the transport abstraction. Implementations must be safe for
// concurrent use by many goroutines and many processes (several platform
// instances share one broker).
type Broker interface {
	// Name returns the adapter name ("nats", "redis", "rabbitmq", "kafka", "fs", "memory").
	Name() string
	// Declare creates the subject and its groups, idempotently. Calling it again
	// with more groups adds them; groups not listed are left alone (removal is
	// explicit via DeleteGroup so a redeploy never drops work in flight).
	Declare(ctx context.Context, cfg SubjectConfig) error
	// DeleteGroup removes a group and any messages pending for it.
	DeleteGroup(ctx context.Context, subject, group string) error
	// Publish appends a message to a subject; every declared group receives it.
	// Publish must be durable when it returns nil.
	Publish(ctx context.Context, subject string, msg *domain.Message) error
	// Subscribe opens a consumer in a group. consumer is a stable name for
	// diagnostics ("<node>#<replica>@<instance>").
	Subscribe(ctx context.Context, subject, group, consumer string) (Subscription, error)
	// Stats reports backlog for a group.
	Stats(ctx context.Context, subject, group string) (Stats, error)
	// Ping verifies connectivity (readiness probe).
	Ping(ctx context.Context) error
	Close() error
}

// Stats is the backlog of a group.
type Stats struct {
	Pending  int64 `json:"pending"`  // published, not yet delivered
	InFlight int64 `json:"inFlight"` // delivered, not yet acked
	Dead     int64 `json:"dead"`     // dead-lettered (when the adapter can tell)
}

// Subscription is a pull consumer bound to one group.
type Subscription interface {
	// Fetch returns up to max deliveries, waiting at most wait for the first
	// one. It returns an empty slice (not an error) on timeout. A cancelled
	// ctx returns ctx.Err().
	Fetch(ctx context.Context, max int, wait time.Duration) ([]Delivery, error)
	Close() error
}

// Delivery is one leased message.
type Delivery interface {
	Message() *domain.Message
	// Attempt is 1 on first delivery.
	Attempt() int
	// Ack completes the message for this group.
	Ack(ctx context.Context) error
	// Nack returns it to the group for redelivery after delay.
	Nack(ctx context.Context, delay time.Duration) error
	// Term dead-letters it: never redelivered, recorded as dead.
	Term(ctx context.Context, reason string) error
	// Extend renews the lease (long turns call this periodically).
	Extend(ctx context.Context) error
}

// Group names for each channel type. Kept here so every adapter, the engine
// and the API agree on them.
func QueueGroup(channel string) string                { return channel }
func NodeGroup(channel, node string) string           { return channel + "." + node }
func ReplicaGroup(channel, node string, r int) string { return channel + "." + node + "." + itoa(r) }
func Subject(graph, channel string) string            { return graph + "." + channel }
func DeadLetterSubject(graph string) string           { return graph + ".__dlq" }

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b [20]byte
	p := len(b)
	for i > 0 {
		p--
		b[p] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		p--
		b[p] = '-'
	}
	return string(b[p:])
}
