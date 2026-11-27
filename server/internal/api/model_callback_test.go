package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/llm"
)

func TestModelCallbackDiagnostic(t *testing.T) {
	for _, tc := range []struct {
		err     error
		message string
		status  int
	}{
		{&llm.APIError{Status: 503, Retryable: true, Provider: "private-data", Body: "private-data"}, "Model provider returned HTTP 503 Service Unavailable", 503},
		{&llm.APIError{Body: "private-data"}, "Model provider reported a streaming error", 0},
		{fmt.Errorf("private-data: %w", llm.ErrCredentials), "Model provider credentials unavailable", 0},
		{context.DeadlineExceeded, "Model callback timed out", 0}, {context.Canceled, "Model callback canceled", 0},
		{errors.New("private-data"), "Model callback failed", 0},
	} {
		d := modelCallbackDiagnostic(tc.err)
		raw, err := json.Marshal(d)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "private-data") || d["message"] != tc.message || d["operation"] != "llm.chat" {
			t.Fatal(string(raw))
		}
		if tc.status != 0 && (d["providerStatus"] != tc.status || d["retryable"] != true) {
			t.Fatal(string(raw))
		}
	}
}
