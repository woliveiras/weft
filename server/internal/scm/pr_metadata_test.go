package scm

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

type listingFixture struct {
	reviewReader
	checks int
	result error
}

func (f *listingFixture) checkPullRequestListing(context.Context) error { f.checks++; return f.result }

func TestMetadataLookupNeedsRepositoryReadAccessToProveAbsence(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests, http.StatusNotFound} {
		reader := &listingFixture{}
		if got := verifiedPullRequestAbsence(t.Context(), reader, &providerHTTPError{Status: status}); got != (status == http.StatusNotFound) || reader.checks > 1 {
			t.Errorf("status %d: absence %t, checks %d", status, got, reader.checks)
		}
	}
	reader := &listingFixture{result: errors.New("unknown visibility")}
	if verifiedPullRequestAbsence(t.Context(), reader, &providerHTTPError{Status: http.StatusNotFound}) || reader.checks != 1 {
		t.Fatal("unverified 404 was reported as absence")
	}
}
