package reviewroute

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
)

func validInput() Input {
	return Input{Name: "portable", Connection: "team", ConnectionRevision: "2026-11-20T12:00:00Z", Provider: "github", Instance: "https://api.github.com", Repository: "team/app", GraphID: "review", GraphVersion: 1, ChannelID: "requests", NodeID: "reviewer", Enabled: true}
}
func pull() PullRequest {
	in := validInput()
	return PullRequest{Connection: in.Connection, ConnectionRevision: in.ConnectionRevision, Provider: in.Provider, Instance: in.Instance, Repository: in.Repository, Number: 7, URL: "https://github.com/team/app/pull/7", HeadSHA: strings.Repeat("a", 40), BaseSHA: strings.Repeat("b", 40)}
}
func TestInputRequiresExplicitScopedPins(t *testing.T) {
	for _, edit := range []func(*Input){func(v *Input) { v.Name = "../bad" }, func(v *Input) { v.ConnectionRevision = "" }, func(v *Input) { v.Provider = "gitlab" }, func(v *Input) { v.Instance = "https://user:secret@api.example" }, func(v *Input) { v.Instance = "https://api.example?" }, func(v *Input) { v.Instance = "http://api.example" }, func(v *Input) { v.GraphVersion = 0 }, func(v *Input) { v.ChannelID = "" }, func(v *Input) { v.NodeID = "bad\nnode" }} {
		v := validInput()
		edit(&v)
		if _, err := normalize(v); err == nil {
			t.Fatal(v)
		}
	}
	for _, provider := range []string{"github", "bitbucket-cloud", "bitbucket-dc"} {
		v := validInput()
		v.Provider = provider
		if _, err := normalize(v); err != nil {
			t.Fatal(provider, err)
		}
	}
}
func TestGlobalReceiptIdentityExcludesRouteEventAndSeparatesAuthority(t *testing.T) {
	p := pull()
	id := receiptID(p)
	for _, edit := range []func(*PullRequest){func(v *PullRequest) { v.Connection = "another" }, func(v *PullRequest) { v.ConnectionRevision = "2026-11-21T12:00:00Z" }, func(v *PullRequest) { v.Provider = "bitbucket-cloud" }, func(v *PullRequest) { v.Instance = "https://enterprise.example" }, func(v *PullRequest) { v.Repository = "team/another" }, func(v *PullRequest) { v.Number++ }, func(v *PullRequest) { v.HeadSHA = strings.Repeat("c", 40) }, func(v *PullRequest) { v.BaseSHA = strings.Repeat("d", 40) }} {
		other := p
		edit(&other)
		if receiptID(other) == id {
			t.Fatal(other)
		}
	}
	p.URL = "https://github.com/team/app/pull/7?display=changed"
	if receiptID(p) != id {
		t.Fatal("display URL changed resource identity")
	}
}
func TestEventMetadataFailsClosedWithoutInferringProvider(t *testing.T) {
	p := pull()
	event := func(p PullRequest, ready bool) *domain.Event {
		raw, _ := json.Marshal(struct {
			PullRequest
			Ready bool `json:"reviewRevisionAvailable"`
		}{p, ready})
		return &domain.Event{ID: "event", Seq: 9, Type: domain.EvPullRequestOpened, Data: raw}
	}
	if _, err := eventPull(event(p, true)); err != nil {
		t.Fatal(err)
	}
	if _, err := eventPull(event(p, false)); err == nil {
		t.Fatal("legacy fact granted routing")
	}
	for _, edit := range []func(*PullRequest){func(v *PullRequest) { v.Provider = "" }, func(v *PullRequest) { v.ConnectionRevision = "" }, func(v *PullRequest) { v.HeadSHA = "main" }, func(v *PullRequest) { v.URL = "javascript:alert(1)" }, func(v *PullRequest) { v.BaseSHA = "" }, func(v *PullRequest) { v.Number = 0 }} {
		other := p
		edit(&other)
		if _, err := eventPull(event(other, true)); err == nil {
			t.Fatal(other)
		}
	}
	if _, err := eventPull(&domain.Event{ID: "large", Seq: 9, Type: domain.EvPullRequestOpened, Data: json.RawMessage(strings.Repeat(" ", 16385))}); err == nil {
		t.Fatal("unbounded fact accepted")
	}
}
