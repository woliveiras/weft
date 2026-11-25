package domain_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
)

func TestTaskExecutionIDFitsDNSBoundaryAndPreservesShortIdentity(t *testing.T) {
	validDNS := regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	for _, size := range []int{1, 30, 54, 55, 64, 80} {
		id := "tsk_" + strings.Repeat("a", size)
		got := domain.TaskExecutionID(id)
		if !validDNS.MatchString(got) || got != domain.TaskExecutionID(id) {
			t.Fatalf("unstable or invalid graph ID for length %d: %q", size, got)
		}
		if size <= 54 && got != domain.TaskRunGraphPrefix+strings.TrimPrefix(id, "tsk_") {
			t.Fatalf("short task mapping changed: %q", got)
		}
		if size > 54 && (len(got) != 63 || !strings.HasPrefix(got, "task-run-h-")) {
			t.Fatalf("long task ID not namespaced and bounded: %q", got)
		}
	}
	first := "tsk_" + strings.Repeat("a", 79) + "b"
	second := "tsk_" + strings.Repeat("a", 79) + "c"
	if domain.TaskExecutionID(first) == domain.TaskExecutionID(second) {
		t.Fatal("identities differing beyond the DNS boundary collided")
	}
}
