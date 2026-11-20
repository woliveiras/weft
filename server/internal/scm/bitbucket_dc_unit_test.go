package scm

import (
	"testing"
)

func TestDCPageBoundary(t *testing.T) {
	yes, no := true, false
	one, zero := 1, 0
	for _, p := range []dcPage[int]{{}, {Values: []int{}}, {Values: []int{}, Last: &no}, {Values: []int{}, Last: &no, Next: &zero}} {
		if _, err := p.next(0); err == nil {
			t.Fatalf("accepted %+v", p)
		}
	}
	if n, err := (dcPage[int]{Values: []int{}, Last: &yes}).next(0); err != nil || n != -1 {
		t.Fatalf("last: %d %v", n, err)
	}
	if n, err := (dcPage[int]{Values: []int{}, Last: &no, Next: &one}).next(0); err != nil || n != 1 {
		t.Fatalf("next: %d %v", n, err)
	}
}
func TestDCRemoteAndAuth(t *testing.T) {
	h, err := newProviderHTTP("https://git.example/context", "synthetic")
	if err != nil {
		t.Fatal(err)
	}
	p := bitbucketDC{c: Connection{Repository: "ACME/app"}, http: h}
	if p.RemoteURL() != "https://git.example/context/scm/acme/app.git" || p.GitAuthHeader() != "Bearer synthetic" {
		t.Fatal("wrong DC Git contract")
	}
}

func TestPullRequestURLRejectsExecutableLinks(t *testing.T) {
	for _, raw := range []string{"javascript:alert(1)", "data:text/html,bad", "https://user:pass@example.com/pr/1", "http://example.com/pr/1", "/relative"} {
		if validPullRequestURL(raw) {
			t.Errorf("accepted %q", raw)
		}
	}
	if !validPullRequestURL("https://git.example/pr/1") {
		t.Fatal("rejected HTTPS PR")
	}
}
