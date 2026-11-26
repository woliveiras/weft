package review

import (
	"context"

	"github.com/woliveiras/weft/server/internal/workspace"
)

func Candidate(ctx context.Context, source, destination string) (string, error) {
	return workspace.Candidate(ctx, source, destination)
}
func Fingerprint(ctx context.Context, source string) (string, error) {
	return workspace.Fingerprint(ctx, source)
}
