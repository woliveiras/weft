package engine

import (
	"github.com/woliveiras/weft/server/internal/llm"
	"testing"
)

func TestReservationUsageClassification(t *testing.T) {
	for _, tc := range []struct {
		response *llm.ChatResponse
		known    bool
		total    int
	}{
		{nil, false, 0}, {&llm.ChatResponse{}, false, 0},
		{&llm.ChatResponse{Usage: llm.Usage{InputTokens: -1, OutputTokens: 2}}, false, 0},
		{&llm.ChatResponse{Usage: llm.Usage{InputTokens: 4, OutputTokens: 2}}, true, 6},
	} {
		r := &tokenReservation{}
		r.report(tc.response)
		if r.known != tc.known || r.reported != tc.total {
			t.Fatal(r, tc)
		}
	}
}
