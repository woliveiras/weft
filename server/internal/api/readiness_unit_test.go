package api

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/queue"
	"github.com/woliveiras/weft/server/internal/storage"
)

type pingStore struct {
	storage.Store
	err error
}

func (p pingStore) Ping(context.Context) error { return p.err }

type pingBroker struct {
	queue.Broker
	err error
}

func (p pingBroker) Ping(context.Context) error { return p.err }
func TestReadinessDoesNotSerializeRawDependencyErrors(t *testing.T) {
	private := errors.New("synthetic-token=private-value upstream=http://private-host")
	for _, tc := range []struct {
		store, broker error
		code          int
	}{{nil, nil, 200}, {private, nil, 503}, {nil, private, 503}, {private, private, 503}} {
		server := Server{Deps: Deps{Store: pingStore{err: tc.store}, Broker: pingBroker{err: tc.broker}, Config: config.Default()}}
		recorder := httptest.NewRecorder()
		if err := server.readyz(recorder, httptest.NewRequest("GET", "/readyz", nil)); err != nil {
			t.Fatal(err)
		}
		if recorder.Code != tc.code {
			t.Fatal(recorder.Code)
		}
		for _, secret := range []string{"synthetic-token", "private-value", "private-host"} {
			if strings.Contains(recorder.Body.String(), secret) {
				t.Fatalf("disclosed dependency diagnostic: %s", recorder.Body.String())
			}
		}
	}
}
