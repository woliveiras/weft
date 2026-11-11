package observability

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var InstructionLoads = promauto.NewCounterVec(prometheus.CounterOpts{Namespace: ns, Name: "instruction_loads_total", Help: "Instruction context preparation by bounded outcome (loaded, failed)."}, []string{"outcome"})
var InstructionLoadSeconds = promauto.NewHistogram(prometheus.HistogramOpts{Namespace: ns, Name: "instruction_load_duration_seconds", Help: "Instruction resolution and durable snapshot latency."})
