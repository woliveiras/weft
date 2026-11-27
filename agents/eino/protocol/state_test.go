package protocol

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStatePath(t *testing.T) {
	for _, key := range []string{"", ".", "..", string(bytes.Repeat([]byte("a"), 513))} {
		if _, err := statePath(key); err == nil {
			t.Fatal("invalid key accepted")
		}
	}
	p, err := statePath("plan:a/b%20")
	if err != nil || p != "/state/plan:a%2Fb%2520" {
		t.Fatal(p, err)
	}
}

func TestStateRoundTripAndMissingKey(t *testing.T) {
	var value []byte
	router := http.NewServeMux()
	router.HandleFunc("/internal/v1/executions/one/state/{key...}", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("key") != "plan:a/b%20" || r.Header.Get("Authorization") != "Bearer fixture" {
			t.Error("wrong state scope")
		}
		switch r.Method {
		case "GET":
			if value == nil {
				w.WriteHeader(404)
				return
			}
			_, _ = w.Write(value)
		case "PUT":
			var err error
			value, err = io.ReadAll(r.Body)
			if err != nil {
				t.Error(err)
			}
			w.WriteHeader(204)
		default:
			t.Error(r.Method)
		}
	})
	s := httptest.NewServer(router)
	defer s.Close()
	client := Client{base: s.URL + "/internal/v1/executions/one", token: "fixture"}
	ctx := context.Background()
	if _, ok, err := client.StateGet(ctx, "plan:a/b%20"); err != nil || ok {
		t.Fatal(ok, err)
	}
	data := []byte{0, 1, 255}
	if err := client.StatePut(ctx, "plan:a/b%20", data); err != nil {
		t.Fatal(err)
	}
	got, ok, err := client.StateGet(ctx, "plan:a/b%20")
	if err != nil || !ok || !bytes.Equal(got, data) {
		t.Fatal(got, ok, err)
	}
}
