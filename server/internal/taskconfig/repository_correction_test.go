package taskconfig

import (
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/storage/sqlstore"
)

func checkoutFixture() (Input, domain.TaskRepository) {
	in := good()
	in.ChannelID, in.MessageType, in.PayloadJSON = "requests", "request", `{"custom":true}`
	in.Repositories = []domain.TaskRepository{{Alias: "ui", ProjectID: "prj_ui", BaseCommit: strings.Repeat("a", 40)}, {Alias: "api", ProjectID: "prj_api", BaseCommit: strings.Repeat("b", 40)}}
	next := in.Repositories[1]
	next.ProjectID = "prj_next"
	next.BaseCommit = strings.Repeat("c", 40)
	return in, next
}

func TestReplaceOneDraftRepository(t *testing.T) {
	in, next := checkoutFixture()
	d := Draft{Status: "draft", Input: in}
	before := in.Repositories[1]
	got, err := d.ReplacedRepositoryInput(before, next)
	if err != nil || !reflect.DeepEqual(got.Repositories, []domain.TaskRepository{in.Repositories[0], next}) || got.PayloadJSON != in.PayloadJSON || d.Repositories[1].ProjectID != before.ProjectID {
		t.Fatal("replacement changed another field or the original draft", got, err)
	}
	duplicate := next
	duplicate.ProjectID = in.Repositories[0].ProjectID
	if _, err = d.ReplacedRepositoryInput(before, duplicate); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	stale := before
	stale.SourceRef = "another-ref"
	if _, err = d.ReplacedRepositoryInput(stale, next); !errors.Is(err, storage.ErrConflict) {
		t.Fatal(err)
	}
	d.EnvironmentApprovalID = "pending"
	if _, err = d.ReplacedRepositoryInput(before, next); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
}

func TestRepositoryCorrectionCommitsDependencyAndReceiptTogether(t *testing.T) {
	ctx := t.Context()
	store, err := sqlstore.OpenSQLite(ctx, filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	in, next := checkoutFixture()
	if _, err = store.SaveGraph(ctx, &domain.Graph{Metadata: domain.GraphMetadata{ID: in.GraphID}, Spec: domain.GraphSpec{Nodes: []domain.Node{{ID: in.NodeID}}}}, 0); err != nil {
		t.Fatal(err)
	}
	m := Manager{Store: store}
	d, err := m.Save(ctx, "", in, 0, "creator")
	if err != nil {
		t.Fatal(err)
	}
	rev, err := store.PutSetting(ctx, "test/project", []byte("{}"), 0, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	save := func(guard int64) (*Draft, error) {
		return m.ReplaceRepositoryForOperation(ctx, d.ID, d.Revision, in.Repositories[1], next, "operator", "edit-1", storage.SettingRevision{Key: "test/project", Revision: guard})
	}
	if _, err = save(rev + 1); !errors.Is(err, storage.ErrConflict) {
		t.Fatal("stale project accepted", err)
	}
	if _, err = save(rev); err != nil {
		t.Fatal(err)
	}
	if _, err = save(rev); !errors.Is(err, storage.ErrConflict) {
		t.Fatal("applied obsolete draft twice", err)
	}
	got, err := m.Get(ctx, d.ID)
	if err != nil || got.RepositoryRevision != 2 || got.CorrectionOperation != "edit-1" || got.CorrectionRevision != 2 || got.CreatedBy != "creator" || got.Status != "draft" || got.PayloadJSON != in.PayloadJSON || !reflect.DeepEqual(got.Repositories, []domain.TaskRepository{in.Repositories[0], next}) {
		t.Fatal(got, err)
	}
}
