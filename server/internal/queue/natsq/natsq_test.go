package natsq_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/queue"
	"github.com/woliveiras/weft/server/internal/queue/conformance"
	"github.com/woliveiras/weft/server/internal/queue/natsq"
)

// Set WEFT_TEST_NATS_URL (e.g. nats://localhost:4222, JetStream enabled) to run.
func TestConformance(t *testing.T) {
	url := os.Getenv("WEFT_TEST_NATS_URL")
	if url == "" {
		t.Skip("WEFT_TEST_NATS_URL not set")
	}
	conformance.RunWith(t, func(t *testing.T) queue.Broker {
		b, err := natsq.New(context.Background(), natsq.Options{URL: url})
		if err != nil {
			t.Fatal(err)
		}
		return b
	}, conformance.Options{AckWait: time.Second})
}

// A message that exhausts MaxDeliver through lease expiry (no Nack ever
// sent) must still reach the DLQ via the MAX_DELIVERIES advisory.
func TestLeaseExhaustionDeadLetters(t *testing.T) {
	url := os.Getenv("WEFT_TEST_NATS_URL")
	if url == "" {
		t.Skip("WEFT_TEST_NATS_URL not set")
	}
	ctx := context.Background()
	b, err := natsq.New(ctx, natsq.Options{URL: url})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	subj := "advg.ch" + time.Now().Format("150405.000000")
	subj = subj[:len(subj)-7] + subj[len(subj)-6:] // drop the dot from the time
	if err := b.Declare(ctx, queue.SubjectConfig{Subject: subj, Groups: []string{"g"}, AckWait: time.Second, MaxDeliver: 2}); err != nil {
		t.Fatal(err)
	}
	s, _ := b.Subscribe(ctx, subj, "g", "c")
	defer s.Close()
	m, _ := domain.NewMessage("t", "p", "", nil)
	if err := b.Publish(ctx, subj, m); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		ds, err := s.Fetch(ctx, 1, 5*time.Second)
		if err != nil || len(ds) != 1 {
			t.Fatalf("fetch %d: %v %d", i, err, len(ds))
		}
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		st, err := b.Stats(ctx, subj, "g")
		if err != nil {
			t.Fatal(err)
		}
		if st.Dead >= 1 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("not dead-lettered: %+v", st)
		}
		// The server only notices exhaustion when it would redeliver, i.e.
		// while a consumer is pulling - as the engine always is.
		if ds, _ := s.Fetch(ctx, 1, 300*time.Millisecond); len(ds) > 0 {
			t.Fatalf("delivered beyond MaxDeliver (attempt %d)", ds[0].Attempt())
		}
	}
}
