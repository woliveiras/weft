package taskconfig

import (
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage/fsstore"
	"testing"
)

func TestIssueOriginValidationAndDeliveryRequirement(t *testing.T) {
	good := IssueOrigin{Kind: "jira", ConnectionID: "tracker", ConnectionRevision: 3, Instance: "https://api.atlassian.com/ex/jira/11111111-2222-3333-4444-555555555555", Key: "APP-7", URL: "https://team.atlassian.net/browse/APP-7"}
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*IssueOrigin){func(o *IssueOrigin) { o.ConnectionID = "*" }, func(o *IssueOrigin) { o.ConnectionRevision = 0 }, func(o *IssueOrigin) { o.Instance = "https://wrong.example" },
		func(o *IssueOrigin) { o.Instance = "11111111-2222-3333-4444-555555555555" },
		func(o *IssueOrigin) {
			o.Instance = "https://wrong.example/ex/jira/11111111-2222-3333-4444-555555555555"
		}, func(o *IssueOrigin) { o.Key = "Review" }, func(o *IssueOrigin) { o.URL = "https://team.atlassian.net/browse/OTHER-1" }, func(o *IssueOrigin) { o.URL = "https://user:password@team.atlassian.net/browse/APP-7" }, func(o *IssueOrigin) { o.URL += "?token=secret" }} {
		bad := good
		mutate(&bad)
		if bad.Validate() == nil {
			t.Fatalf("accepted %+v", bad)
		}
	}
	if (&Draft{}).RequiresPullRequest() || !(&Draft{TriggerIssueOrigin: &good}).RequiresPullRequest() || !(&Draft{TriggerGitHubIssue: "acme/app#7"}).RequiresPullRequest() {
		t.Fatal("delivery requirement")
	}
}

func TestTrustedOriginSurvivesEditsAndStoreRestart(t *testing.T) {
	testTrustedOriginPersistence(t, IssueOrigin{Kind: "jira", ConnectionID: "jira-team", ConnectionRevision: 7, Instance: "https://api.atlassian.com/ex/jira/11111111-2222-3333-4444-555555555555", Key: "APP-7"})
}

func TestNativeTrustedOriginSurvivesEditsAndStoreRestart(t *testing.T) {
	testTrustedOriginPersistence(t, IssueOrigin{Kind: "jira-dc", ConnectionID: "jira-team", ConnectionRevision: 7, Instance: "https://jira.example/context", ProjectID: "10001", Key: "APP-7", URL: "https://jira.example/context/browse/APP-7"})
}

func testTrustedOriginPersistence(t *testing.T, origin IssueOrigin) {
	t.Helper()
	ctx := t.Context()
	root := t.TempDir()
	st, err := fsstore.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	g := &domain.Graph{Metadata: domain.GraphMetadata{ID: "factory"}, Spec: domain.GraphSpec{Nodes: []domain.Node{{ID: "reader"}}}}
	if _, err = st.SaveGraph(ctx, g, 0); err != nil {
		t.Fatal(err)
	}
	m := Manager{Store: st}
	in := good()
	in.SCMConnection = "bitbucket-team"
	invalid := origin
	invalid.Instance = "11111111-2222-3333-4444-555555555555"
	if _, err = m.SaveTriggeredIssue(ctx, in, "admin", invalid); err == nil {
		t.Fatal("bare cloud ID persisted")
	}
	rows, err := st.ListSettings(ctx, prefix)
	if err != nil || len(rows) != 0 {
		t.Fatalf("invalid origin mutated storage: %d %v", len(rows), err)
	}
	draft, err := m.SaveTriggeredIssue(ctx, in, "admin", origin)
	if err != nil {
		t.Fatal(err)
	}
	in.Request = "Edited user text: Closes #999"
	draft, err = m.Save(ctx, draft.ID, in, draft.Revision, "admin")
	if err != nil {
		t.Fatal(err)
	}
	draft, err = m.SavePrompt(ctx, PromptInput{ID: draft.ID, Revision: draft.Revision, Request: "Another edit"}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err = st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = fsstore.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	draft, err = (Manager{Store: st}).Get(ctx, draft.ID)
	if err != nil || draft.TriggerIssueOrigin == nil || *draft.TriggerIssueOrigin != origin || draft.TriggerGitHubIssue != "" || draft.SCMConnection != "bitbucket-team" || draft.Source != nil {
		t.Fatalf("snapshot %+v %v", draft, err)
	}
}

func TestNativeIssueOriginRejectsAmbiguousScopeAndLinks(t *testing.T) {
	origin := IssueOrigin{Kind: "jira-dc", ConnectionID: "native", ConnectionRevision: 1, Instance: "https://jira.example/context", ProjectID: "10001", Key: "APP-7", URL: "https://jira.example/context/browse/APP-7"}
	if err := origin.Validate(); err != nil {
		t.Fatal(err)
	}
	contextNamedRest := origin
	contextNamedRest.Instance = "https://jira.example/rest"
	contextNamedRest.URL = contextNamedRest.Instance + "/browse/APP-7"
	if err := contextNamedRest.Validate(); err != nil {
		t.Fatal("connection-valid context rejected after claim", err)
	}
	for _, mutate := range []func(*IssueOrigin){
		func(o *IssueOrigin) { o.ProjectID = "APP" },
		func(o *IssueOrigin) { o.ProjectID = "" },
		func(o *IssueOrigin) { o.Instance = "http://jira.example/context" },
		func(o *IssueOrigin) { o.Instance += "?" },
		func(o *IssueOrigin) { o.Instance += "/../other" },
		func(o *IssueOrigin) { o.Instance += "/rest/api/2" },
		func(o *IssueOrigin) { o.URL = "https://other.example/browse/APP-7" },
		func(o *IssueOrigin) { o.URL += "?token=secret" },
		func(o *IssueOrigin) { o.Key = "APP-8" },
	} {
		bad := origin
		mutate(&bad)
		if bad.Validate() == nil {
			t.Fatal("invalid trusted origin", bad)
		}
	}
}
