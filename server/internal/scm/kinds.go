package scm

// Kind describes a registered delivery adapter without credentials or network calls.
// Capabilities describe implemented operations, not a live-account qualification.
type Kind struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	RepositoryHint    string `json:"repositoryHint"`
	APIURLHint        string `json:"apiUrlHint"`
	APIURLRequired    bool   `json:"apiUrlRequired"`
	CredentialHelp    string `json:"credentialHelp"`
	Push              bool   `json:"push"`
	CreatePullRequest bool   `json:"createPullRequest"`
}

// Kinds lists built-in adapters exposed for configuration. Test-only registered
// factories are deliberately excluded from this operator-facing catalogue.
func Kinds() []Kind {
	all := []Kind{
		{ID: "github", Name: "GitHub", RepositoryHint: "owner/name", APIURLHint: "https://api.github.com", CredentialHelp: "A repository-scoped token with permission to push task branches and open pull requests.", Push: true, CreatePullRequest: true},
		{ID: "bitbucket-cloud", Name: "Bitbucket Cloud", RepositoryHint: "workspace/repository-slug", APIURLHint: "https://api.bitbucket.org/2.0", CredentialHelp: "A user OAuth access token with account and repository permissions, including repository write and pull request write. API tokens and app passwords are not supported by this adapter. Token acquisition and renewal are managed outside Weft.", Push: true, CreatePullRequest: true},
		{ID: "bitbucket-dc", Name: "Bitbucket Data Center", RepositoryHint: "PROJECT/repository-slug", APIURLHint: "https://bitbucket.example.com/context", APIURLRequired: true, CredentialHelp: "An HTTP access token sent as Bearer, with repository read, write and pull request access. Enter the site base URL, including its context path; omit /rest/api/1.0. Personal projects are not yet supported. The server must trust the site's TLS certificate.", Push: true, CreatePullRequest: true},
	}
	out := make([]Kind, 0, len(all))
	for _, k := range all {
		if _, ok := factory(k.ID); ok {
			out = append(out, k)
		}
	}
	return out
}
