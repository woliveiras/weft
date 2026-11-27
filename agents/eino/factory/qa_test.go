package factory

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/woliveiras/weft/agents/eino/protocol"
)

func TestQAUsesGatewayExitStatus(t *testing.T) {
	qa, err := QA(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name       string
		code       int
		body, kind string
	}{
		{"passed", 200, `{"data":{"exitCode":0,"stdout":"ok"}}`, "qa.passed"},
		{"failed", 200, `{"data":{"exitCode":1,"stderr":"test failed"}}`, "qa.failed"},
		{"timed out", 200, `{"data":{"exitCode":0,"timedOut":true}}`, "qa.failed"},
		{"text claims success", 200, `{"content":"exitCode: 0"}`, "qa.failed"},
		{"missing status", 200, `{"content":"ok"}`, "qa.failed"},
		{"gateway error", 200, `{"content":"private fixture","isError":true}`, "qa.failed"},
		{"denied", 403, `{}`, "qa.failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			controller := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var args struct {
					Argv       []string
					TimeoutSec int
				}
				if json.NewDecoder(r.Body).Decode(&args) != nil || !slices.Equal(args.Argv, []string{"go", "test", "./..."}) || args.TimeoutSec != 600 {
					t.Error(args)
				}
				w.WriteHeader(tc.code)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer controller.Close()
			h, err := protocol.NewHandler([]string{controller.URL}, "", map[string]protocol.Agent{"qa": qa})
			if err != nil {
				t.Fatal(err)
			}
			out := sendTurn(t, h, controller.URL, "qa", "qa1", "review.approved", map[string]string{"task_id": "task1"})
			if len(out) != 1 || out[0].Type != tc.kind || stringValue(out[0].Payload["task_id"]) != "task1" {
				t.Fatal(out)
			}
			payload := out[0].Payload
			switch tc.name {
			case "failed":
				if !strings.Contains(string(payload["failures"]), "test failed") || string(payload["exit_code"]) != "1" {
					t.Fatal(payload)
				}
			case "denied":
				if stringValue(payload["reason"]) != "shell.exec not granted" {
					t.Fatal(payload)
				}
			case "gateway error":
				if strings.Contains(string(payload["failures"]), "private fixture") || len(payload["failures"]) == 0 {
					t.Fatal(payload)
				}
			}
		})
	}
}
