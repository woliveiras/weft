package scm

import (
	"strings"
	"testing"
	"time"
)

func TestDeliveryEventIdentityAndMissingRevisions(t *testing.T) {
	c := Connection{Name: "team", Kind: "bitbucket-cloud", Repository: "acme/app", UpdatedAt: time.Now()}
	pr := PullRequest{Number: 7, URL: "https://bitbucket.org/acme/app/pull-requests/7", Head: "weft/fix", Base: "main", State: "open", HeadSHA: strings.Repeat("a", 40), BaseSHA: strings.Repeat("b", 40)}
	e := deliveryEvent(c, pr, "")
	if e["reviewRevisionAvailable"] != true || e["instance"] != "https://api.bitbucket.org/2.0" || e["provider"] != c.Kind || e["connectionRevision"] != c.UpdatedAt.UTC().Format(time.RFC3339Nano) {
		t.Fatalf("identity %+v", e)
	}
	for _, mutate := range []func(*PullRequest){func(p *PullRequest) { p.HeadSHA = "abcdef" }, func(p *PullRequest) { p.BaseSHA = "" }, func(p *PullRequest) { p.State = "closed" }, func(p *PullRequest) { p.Number = 0 }} {
		bad := pr
		mutate(&bad)
		if deliveryEvent(c, bad, "")["reviewRevisionAvailable"] != false {
			t.Fatal("incomplete delivery admitted for review")
		}
	}
	c.Kind = "bitbucket-dc"
	if deliveryEvent(c, pr, "")["reviewRevisionAvailable"] != false {
		t.Fatal("DC missing instance admitted")
	}
}

func TestAdapterDeliveryRevisionConversion(t *testing.T) {
	sha, base := strings.Repeat("a", 40), strings.Repeat("b", 40)
	gh := ghPull{}
	gh.Head.SHA = sha
	gh.Base.SHA = base
	bb := bbCloudPull{}
	bb.Source.Commit.Hash = sha
	bb.Destination.Commit.Hash = base
	dc := dcPull{ID: 1}
	dc.From.LatestCommit = sha
	dc.To.LatestCommit = base
	dc.Links.Self = append(dc.Links.Self, struct {
		Href string `json:"href"`
	}{Href: "https://git.example/pr/1"})
	d, err := dc.pr(true)
	if err != nil {
		t.Fatal(err)
	}
	for _, pr := range []*PullRequest{gh.pr(true), bb.pr(true), d} {
		if pr.HeadSHA != sha || pr.BaseSHA != base {
			t.Fatalf("lost revisions %+v", pr)
		}
	}
}
