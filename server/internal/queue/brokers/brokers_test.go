package brokers_test

import (
	"context"
	"testing"

	"github.com/woliveiras/weft/server/internal/queue/brokers"
)

func TestOpenLocal(t *testing.T) {
	for _, c := range []brokers.Config{{Type: "memory"}, {Type: "fs", Dir: t.TempDir()}} {
		b, err := brokers.Open(context.Background(), c)
		if err != nil {
			t.Fatalf("%s: %v", c.Type, err)
		}
		if err := b.Ping(context.Background()); err != nil {
			t.Fatalf("%s ping: %v", c.Type, err)
		}
		b.Close()
	}
	if _, err := brokers.Open(context.Background(), brokers.Config{Type: "carrier-pigeon"}); err == nil {
		t.Fatal("expected error for unknown type")
	}
}
