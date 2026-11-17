package instructions

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/storage"
)

type pathErrorJournal struct {
	storage.Store
	err error
}

func (s pathErrorJournal) PutSetting(context.Context, string, []byte, int64, string) (int64, error) {
	return 0, s.err
}

func TestGuidanceJournalRedactsStorageFailure(t *testing.T) {
	privatePath := filepath.Join(t.TempDir(), "private", "journal")
	m := Manager{Store: pathErrorJournal{err: &os.PathError{Op: "write", Path: privatePath, Err: os.ErrPermission}}}
	session := &Session{manager: m, snapshot: &Snapshot{ExecutionID: "audit", Budget: 512}}
	for _, tc := range []struct {
		name string
		run  func() error
	}{
		{"body", func() error {
			return session.recordRead(t.Context(), Entry{Scope: "repository-skill", Stage: "body", Included: true}, "sample")
		}},
		{"tool limit", func() error {
			_, err := session.OmitToolCallLimit(t.Context(), "repository:sample", "ref.md")
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.run()
			if err == nil || err.Error() != "guidance audit persistence unavailable" || strings.Contains(err.Error(), privatePath) {
				t.Fatalf("unexpected public error: %v", err)
			}
			if session.sequence != 0 || len(session.reads) != 0 || len(session.pending) != 0 || session.snapshot.Budget != 512 {
				t.Fatal("failed write changed live delivery state")
			}
		})
	}
}
