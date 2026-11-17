package api_test

import (
	"context"
	"net/http"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/projectconfig"
	"github.com/woliveiras/weft/server/internal/repositoryimport"
	"github.com/woliveiras/weft/server/internal/repositoryimport/importtest"
)

func TestRepositoryImportAPIPermissionsAndPersistence(t *testing.T) {
	srv, a := setupApp(t, nil)
	fixture, err := importtest.New(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer fixture.Server.Close()
	if err = a.RepositoryImports.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	manager, err := repositoryimport.New(repositoryimport.Options{Store: a.Store, Root: filepath.Join(t.TempDir(), "imports"), Client: fixture.Client})
	if err != nil {
		t.Fatal(err)
	}
	a.RepositoryImports = manager
	a.API.RepositoryImports = manager
	body := `{"url":"https://repository.fixture.example/source.git","ref":"HEAD","confirmed":true}`
	for _, token := range []string{viewerTok, operatorTok} {
		if c := call(t, srv, "POST", "/api/v1/repository-imports", token, "application/json", body, nil); c != 403 {
			t.Fatal(c)
		}
	}
	for _, bad := range []string{`{"url":"https://example.com/repo","authentication":{"mode":"local_git"},"confirmed":true}`, `{"url":"https://example.com/repo","authentication":{"mode":"token","username":"u","token":"bad\nvalue"},"confirmed":true}`, `{"url":"file:///tmp/repo","confirmed":true}`, `{"url":"https://user:secret@host.example/repo","confirmed":true}`, `{"url":"https://example.com/repo","tools":["shell.exec"],"confirmed":true}`, `{"url":"https://example.com/repo","confirmed":false}`} {
		if c := call(t, srv, "POST", "/api/v1/repository-imports", adminTok, "application/json", bad, nil); c != 400 {
			t.Fatal(c)
		}
	}
	var record repositoryimport.Record
	if c := call(t, srv, "POST", "/api/v1/repository-imports", adminTok, "application/json", body, &record); c != 202 {
		t.Fatal(c)
	}
	end := time.Now().Add(10 * time.Second)
	for time.Now().Before(end) {
		if c := call(t, srv, "GET", "/api/v1/repository-imports/"+record.ID, viewerTok, "", "", &record); c != 200 {
			t.Fatal(c)
		}
		if record.State == "ready" || record.State == "failed" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if record.State != "ready" || record.Commit != fixture.Commit {
		t.Fatal(record)
	}
	var list []*repositoryimport.Record
	if c := call(t, srv, "GET", "/api/v1/repository-imports", viewerTok, "", "", &list); c != 200 || len(list) != 1 {
		t.Fatal(c, list)
	}
	projects, err := (projectconfig.Manager{Store: a.Store}).List(t.Context())
	if err != nil || len(projects) != 1 || projects[0].Head != record.Commit {
		t.Fatal(projects, err)
	}
	first := record.Attempt
	if c := call(t, srv, "POST", "/api/v1/repository-imports", adminTok, "application/json", body, &record); c != 202 || record.Attempt != first {
		t.Fatal(c, record)
	}
	req, _ := http.NewRequestWithContext(context.Background(), "POST", srv.URL+"/api/v1/repository-imports/"+record.ID+"/cancel", nil)
	req.Header.Set("Authorization", "Bearer "+adminTok)
	req.Header.Set("If-Match", strconv.FormatInt(record.Revision, 10))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 409 {
		t.Fatal(resp.StatusCode)
	}
	graphs, err := a.Store.ListGraphs(t.Context())
	if err != nil || len(graphs) != 0 {
		t.Fatal("import created a workflow", graphs, err)
	}
}
