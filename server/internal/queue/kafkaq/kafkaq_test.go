package kafkaq_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/queue"
	"github.com/woliveiras/weft/server/internal/queue/conformance"
	"github.com/woliveiras/weft/server/internal/queue/kafkaq"
)

// Set WEFT_TEST_KAFKA_BROKERS (e.g. localhost:9092) to run.
func TestConformance(t *testing.T) {
	brokers := os.Getenv("WEFT_TEST_KAFKA_BROKERS")
	if brokers == "" {
		t.Skip("WEFT_TEST_KAFKA_BROKERS not set")
	}
	conformance.RunWith(t, func(t *testing.T) queue.Broker {
		b, err := kafkaq.New(context.Background(), kafkaq.Options{Brokers: kafkaq.ParseBrokers(brokers), Partitions: 4})
		if err != nil {
			t.Fatal(err)
		}
		return b
	}, conformance.Options{
		AckWait:         2 * time.Second,
		Settle:          4 * time.Second, // consumer-group joins and rebalances
		FetchWait:       20 * time.Second,
		NoInFlightStats: false,
	})
}
