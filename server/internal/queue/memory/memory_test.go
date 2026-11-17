package memory_test

import (
	"testing"

	"github.com/woliveiras/weft/server/internal/queue"
	"github.com/woliveiras/weft/server/internal/queue/conformance"
	"github.com/woliveiras/weft/server/internal/queue/memory"
)

func TestConformance(t *testing.T) {
	conformance.Run(t, func(t *testing.T) queue.Broker { return memory.New() })
}
