package fsq_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/queue"
	"github.com/woliveiras/weft/server/internal/queue/conformance"
	"github.com/woliveiras/weft/server/internal/queue/fsq"
)

func TestConformance(t *testing.T) {
	conformance.Run(t, func(t *testing.T) queue.Broker {
		b, err := fsq.New(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		return b
	})
}

// Two broker instances on one directory stand in for two processes: the
// rename claim must hand each message to exactly one of them.
func TestTwoInstancesCompete(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	b1, _ := fsq.New(dir)
	b2, _ := fsq.New(dir)
	defer b1.Close()
	defer b2.Close()
	if err := b1.Declare(ctx, queue.SubjectConfig{Subject: "g.c", Groups: []string{"w"}}); err != nil {
		t.Fatal(err)
	}
	const n = 200
	for i := 0; i < n; i++ {
		m, _ := domain.NewMessage("t", "p", "", map[string]int{"i": i})
		if err := b2.Publish(ctx, "g.c", m); err != nil {
			t.Fatal(err)
		}
	}
	var mu sync.Mutex
	seen := map[string]int{}
	var wg sync.WaitGroup
	for _, b := range []*fsq.Broker{b1, b2, b1, b2} {
		s, err := b.Subscribe(ctx, "g.c", "w", "c")
		if err != nil {
			t.Fatal(err)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				ds, err := s.Fetch(ctx, 7, 200*time.Millisecond)
				if err != nil {
					t.Error(err)
					return
				}
				if len(ds) == 0 {
					return
				}
				for _, d := range ds {
					mu.Lock()
					seen[d.Message().ID]++
					mu.Unlock()
					if err := d.Ack(ctx); err != nil {
						t.Error(err)
					}
				}
			}
		}()
	}
	wg.Wait()
	if len(seen) != n {
		t.Fatalf("expected %d, got %d", n, len(seen))
	}
	for id, c := range seen {
		if c != 1 {
			t.Fatalf("%s delivered %d times", id, c)
		}
	}
}
