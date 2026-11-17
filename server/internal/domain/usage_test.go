package domain

import "testing"

func TestUsageAddIncludesCacheTokens(t *testing.T) {
	u := Usage{InputTokens: 10, OutputTokens: 2, CacheReadTokens: 4}
	u.Add(Usage{InputTokens: 5, OutputTokens: 1, CacheReadTokens: 1, CacheWriteTokens: 3})
	if u != (Usage{InputTokens: 15, OutputTokens: 3, CacheReadTokens: 5, CacheWriteTokens: 3}) {
		t.Fatalf("usage = %+v", u)
	}
}
