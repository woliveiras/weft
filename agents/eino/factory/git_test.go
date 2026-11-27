package factory

import (
	"context"
	"encoding/json"
	"net/http"
	"path"
	"slices"
	"testing"

	"github.com/woliveiras/weft/agents/eino/protocol"
	"github.com/woliveiras/weft/agents/eino/protocol/protocoltest"
)

func TestCommitFiles(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		secondDiff               protocol.ToolResult
		diffStatus, commitStatus int
		commit, wantError        bool
	}{
		{"clean", protocol.ToolResult{Content: "no differences"}, 200, 200, false, false},
		{"second path changed", protocol.ToolResult{Content: "diff --git"}, 200, 200, true, false},
		{"truncated", protocol.ToolResult{Content: "no differences", Truncated: true}, 200, 200, true, false},
		{"write-only grant", protocol.ToolResult{}, 403, 200, true, false},
		{"diff failure", protocol.ToolResult{IsError: true}, 200, 200, false, true},
		{"commit failure", protocol.ToolResult{Content: "diff --git"}, 200, 500, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			paths := []string{"first.go", "second.go"}
			var inspected []string
			commits := 0
			gateway := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				result := protocol.ToolResult{Content: "ok"}
				switch path.Base(r.URL.Path) {
				case "git.add":
					var in struct{ Paths []string }
					if json.NewDecoder(r.Body).Decode(&in) != nil || !slices.Equal(in.Paths, paths) {
						t.Error("wrong staged paths")
					}
				case "git.diff":
					var in struct {
						Staged bool
						Path   string
					}
					if json.NewDecoder(r.Body).Decode(&in) != nil || !in.Staged {
						t.Error("expected staged diff")
					}
					inspected = append(inspected, in.Path)
					result.Content = "no differences"
					if in.Path == paths[1] {
						w.WriteHeader(tc.diffStatus)
						result = tc.secondDiff
					}
				case "git.commit":
					commits++
					w.WriteHeader(tc.commitStatus)
				default:
					t.Error(r.URL.Path)
					w.WriteHeader(404)
				}
				_ = json.NewEncoder(w).Encode(result)
			})
			role := func(ctx context.Context, _ *protocol.TurnInput, c *protocol.Client) (protocol.TurnResult, error) {
				return protocol.TurnResult{}, commitFiles(ctx, c, paths, "update")
			}
			_, err := protocoltest.Run(t, protocol.TurnInput{Agent: "developer"}, role, gateway)
			if (err != nil) != tc.wantError || (commits == 1) != tc.commit || commits > 1 || !slices.Equal(inspected, paths) {
				t.Fatalf("err=%v commits=%d paths=%v", err, commits, inspected)
			}
		})
	}
}
