package graph

import (
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
)

func TestRepoKind(t *testing.T) {
	cases := map[string]RepoURLKind{
		"https://github.com/woliveiras/bookaneer.git": RepoRemote,
		"https://github.com/woliveiras/bookaneer":     RepoRemote,
		"ssh://git@github.com/owner/repo.git":         RepoRemote,
		"git://example.org/repo.git":                  RepoRemote,
		"git@github.com:owner/repo.git":               RepoRemote,
		"http://intranet/repo.git":                    RepoInsecure,
		"/var/lib/weft/workspaces/other-graph":        RepoInvalid,
		"../other-graph":                              RepoInvalid,
		"other-graph":                                 RepoInvalid,
		"file:///etc":                                 RepoInvalid,
		"ext::sh -c touch% /tmp/pwned":                RepoInvalid,
		"--upload-pack=touch /tmp/x":                  RepoInvalid,
		"https://":                                    RepoInvalid,
		"C:\\repos\\x":                                RepoInvalid,
		"":                                            RepoInvalid,
	}
	for repo, want := range cases {
		if got := RepoKind(repo); got != want {
			t.Errorf("RepoKind(%q) = %v, want %v", repo, got, want)
		}
	}
}

func TestValidateWorkspaceRepo(t *testing.T) {
	doc := func(repo string) *domain.Graph {
		return &domain.Graph{
			Metadata: domain.GraphMetadata{Name: "r"},
			Spec: domain.GraphSpec{
				Workspace: domain.WorkspaceSpec{Repo: repo},
				Nodes:     []domain.Node{{ID: "a", Prompt: "p"}},
			},
		}
	}
	if r := Validate(doc("https://github.com/o/r.git"), nil); !r.Valid {
		t.Fatalf("https: %+v", r.Errors)
	}
	r := Validate(doc("/var/lib/weft/workspaces/other"), nil)
	if r.Valid || r.Errors[0].Path != "spec.workspace.repo" || !strings.Contains(r.Errors[0].Message, "local paths") {
		t.Fatalf("local path: %+v", r)
	}
	r = Validate(doc("http://intranet/r.git"), nil)
	if !r.Valid || len(r.Warnings) != 1 || r.Warnings[0].Path != "spec.workspace.repo" {
		t.Fatalf("http: %+v", r)
	}
}
