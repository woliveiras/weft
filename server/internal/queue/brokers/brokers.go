// Package brokers opens a queue.Broker from configuration. It lives apart
// from package queue because every adapter imports queue.
//
//	type      url example                              notes
//	memory    -                                        in-process, not durable
//	fs        - (Dir: /var/lib/weft/queues)         Dir required
//	nats      nats://nats:4222                         default; Options: replicas, creds, name
//	redis     redis://redis:6379/0                     Redis >= 7
//	rabbitmq  amqp://guest:guest@rabbitmq:5672/        Options: ackWait, maxDeliver
//	kafka     kafka://k1:9092,k2:9092 (or host list)   Options: partitions, replicationFactor, ackWait, maxDeliver
package brokers

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/queue"
	"github.com/woliveiras/weft/server/internal/queue/fsq"
	"github.com/woliveiras/weft/server/internal/queue/kafkaq"
	"github.com/woliveiras/weft/server/internal/queue/memory"
	"github.com/woliveiras/weft/server/internal/queue/natsq"
	"github.com/woliveiras/weft/server/internal/queue/rabbitq"
	"github.com/woliveiras/weft/server/internal/queue/redisq"
)

// Config selects and configures a broker.
type Config struct {
	Type    string            `json:"type" yaml:"type"`
	URL     string            `json:"url,omitempty" yaml:"url,omitempty"`
	Dir     string            `json:"dir,omitempty" yaml:"dir,omitempty"`
	Options map[string]string `json:"options,omitempty" yaml:"options,omitempty"`
}

// Types lists supported broker types.
func Types() []string { return []string{"nats", "redis", "rabbitmq", "kafka", "fs", "memory"} }

// Open connects to the configured broker.
func Open(ctx context.Context, cfg Config) (queue.Broker, error) {
	opt := func(k string) string { return cfg.Options[k] }
	dur := func(k string) time.Duration {
		d, _ := time.ParseDuration(opt(k))
		return d
	}
	num := func(k string) int {
		n, _ := strconv.Atoi(opt(k))
		return n
	}
	switch strings.ToLower(cfg.Type) {
	case "", "nats":
		return natsq.New(ctx, natsq.Options{
			URL: cfg.URL, Name: opt("name"), Replicas: num("replicas"), CredsFile: opt("creds"),
			DisableAdvisories: opt("advisories") == "false",
		})
	case "memory", "mem":
		return memory.New(), nil
	case "fs", "file", "filesystem":
		dir := cfg.Dir
		if dir == "" {
			dir = strings.TrimPrefix(cfg.URL, "file://")
		}
		return fsq.New(dir)
	case "redis":
		return redisq.New(ctx, cfg.URL)
	case "rabbitmq", "amqp":
		return rabbitq.New(ctx, rabbitq.Options{URL: cfg.URL, AckWait: dur("ackWait"), MaxDeliver: num("maxDeliver")})
	case "kafka":
		return kafkaq.New(ctx, kafkaq.Options{
			Brokers:           kafkaq.ParseBrokers(cfg.URL),
			Partitions:        int32(num("partitions")),
			ReplicationFactor: int16(num("replicationFactor")),
			AckWait:           dur("ackWait"),
			MaxDeliver:        num("maxDeliver"),
			ClientID:          opt("clientId"),
		})
	default:
		return nil, fmt.Errorf("queue: unknown broker type %q (supported: %s)", cfg.Type, strings.Join(Types(), ", "))
	}
}
