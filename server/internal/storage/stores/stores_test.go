package stores_test

import (
	"context"
	"testing"

	"github.com/woliveiras/weft/server/internal/storage/stores"
)

func TestOpen(t *testing.T) {
	for _, typ := range []string{"", "sqlite", "fs"} {
		s, err := stores.Open(context.Background(), stores.Config{Type: typ, Dir: t.TempDir()})
		if err != nil {
			t.Fatalf("%q: %v", typ, err)
		}
		if err := s.Ping(context.Background()); err != nil {
			t.Fatalf("%q ping: %v", typ, err)
		}
		s.Close()
	}
	if _, err := stores.Open(context.Background(), stores.Config{Type: "postgres"}); err == nil {
		t.Fatal("postgres without DSN must fail")
	}
	if _, err := stores.Open(context.Background(), stores.Config{Type: "mongo"}); err == nil {
		t.Fatal("unknown type must fail")
	}
}
