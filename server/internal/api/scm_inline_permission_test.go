package api_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/scm"
)

func TestDataCenterInlinePermissionRequiresAdminAndCurrentConnection(t *testing.T) {
	srv, app := setupApp(t, nil)
	token := "synthetic-dc"
	cfg := scm.Input{Name: "dc", Kind: "bitbucket-dc", Repository: "TEAM/repo", APIURL: "https://dc.invalid", Token: &token}
	c, err := app.SCM.Save(t.Context(), cfg, "fixture", false)
	if err != nil || c.InlinePublication {
		t.Fatal(err)
	}
	const path = "/api/v1/scm/connections/dc/inline-publication"
	body := fmt.Sprintf(`{"enabled":true,"revision":%q}`, c.UpdatedAt.Format(time.RFC3339Nano))
	for _, token := range []string{viewerTok, operatorTok} {
		if status := call(t, srv, "PUT", path, token, "application/json", body, nil); status != 403 {
			t.Fatal(status)
		}
	}
	if status := call(t, srv, "PUT", path, adminTok, "application/json", `{"enabled":true}`, nil); status != 400 {
		t.Fatal(status)
	}
	var enabled scm.View
	if status := call(t, srv, "PUT", path, adminTok, "application/json", body, &enabled); status != 200 || !enabled.InlinePublication || !enabled.TokenConfigured || enabled.Token != "" || enabled.UpdatedAt.Equal(c.UpdatedAt) {
		t.Fatal(status)
	}
	if status := call(t, srv, "PUT", path, adminTok, "application/json", body, nil); status != 409 {
		t.Fatal(status)
	}
	cfg.Token = nil
	cfg.BranchPrefix = "work/"
	c, err = app.SCM.Save(t.Context(), cfg, "fixture", true)
	if err != nil || !c.InlinePublication {
		t.Fatal("unrelated update lost decision", err)
	}
	cfg.Repository = "TEAM/new"
	c, err = app.SCM.Save(t.Context(), cfg, "fixture", true)
	if err != nil || c.InlinePublication {
		t.Fatal("new destination inherited decision", err)
	}
}
