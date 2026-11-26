package observability

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var ChatOperationSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Namespace: ns, Name: "chat_operation_duration_seconds",
	Help: "Chat model calls and background reconciliation passes by bounded operation and outcome.",
}, []string{"operation", "outcome"})

var ChatWatchDelay = promauto.NewHistogram(prometheus.HistogramOpts{
	Namespace: ns, Name: "chat_watch_schedule_delay_seconds",
	Help: "Time between a scheduled watch reconciliation tick and its actual start.",
})
