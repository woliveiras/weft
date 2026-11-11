//go:build deps

// Package deps pins every third-party module the platform uses so that
// `go mod tidy` keeps them even while an adapter is being written. It is
// never compiled into a binary (build tag "deps").
package deps

import (
	_ "github.com/expr-lang/expr"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "github.com/nats-io/nats.go"
	_ "github.com/nats-io/nats.go/jetstream"
	_ "github.com/prometheus/client_golang/prometheus"
	_ "github.com/prometheus/client_golang/prometheus/promhttp"
	_ "github.com/rabbitmq/amqp091-go"
	_ "github.com/redis/go-redis/v9"
	_ "github.com/robfig/cron/v3"
	_ "github.com/twmb/franz-go/pkg/kadm"
	_ "github.com/twmb/franz-go/pkg/kgo"
	_ "go.opentelemetry.io/contrib/bridges/otelslog"
	_ "go.opentelemetry.io/otel"
	_ "go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	_ "go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	_ "go.opentelemetry.io/otel/sdk/log"
	_ "go.opentelemetry.io/otel/sdk/trace"
	_ "gopkg.in/yaml.v3"
	_ "modernc.org/sqlite"
)
