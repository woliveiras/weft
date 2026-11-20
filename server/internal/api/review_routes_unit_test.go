package api

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReviewRouteBodyRequiresExplicitConfirmationAndKnownFields(t *testing.T) {
	for _, tc := range []struct {
		body           string
		preview, valid bool
	}{
		{`{"route":{"name":"review"},"confirm":true}`, false, true},
		{`{"route":{"name":"review"}}`, true, true},
		{`{"route":{"name":"review"}}`, false, false},
		{`{"route":{"name":"review"},"confirm":false}`, false, false},
		{`{"route":{"name":"review","unexpected":true},"confirm":true}`, false, false},
		{`{"route":{},"confirm":true} {}`, false, false},
		{strings.Repeat("x", 8193), false, false},
	} {
		r := httptest.NewRequest("POST", "/", strings.NewReader(tc.body))
		_, err := decodeReviewRoute(r, tc.preview)
		if (err == nil) != tc.valid {
			t.Fatal(tc, err)
		}
	}
}
