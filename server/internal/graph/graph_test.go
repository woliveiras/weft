package graph

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/woliveiras/weft/server/internal/domain"
)

const sample = `
apiVersion: weft.io/v1
kind: Graph
metadata: {name: Review Loop}
spec:
  parameters: [{name: topic, value: compilers}]
  nodes:
    - id: dev
      prompt: implement @topic from @input
      tools: [filesystem.read, {name: shell.exec, commands: ["go *"]}]
    - id: reviewer
      prompt: review it
    - id: qa
      prompt: test it
      cluster: {replicas: 3}
    - id: auditor
      prompt: watch everything
      subscribe: ["events.*"]
  channels:
    - {id: tasks, to: [dev]}
    - {id: code, from: dev, to: [reviewer]}
    - {id: approved, from: reviewer, to: [qa], when: 'payload.verdict == "approved"'}
    - {id: rework, from: reviewer, to: [dev], when: 'payload.verdict != "approved"'}
    - {id: events.review, type: topic, from: reviewer, to: []}
    - {id: alarm, type: broadcast, from: dev, to: [qa], types: ["alarm.*"]}
`

func load(t *testing.T) *domain.Graph {
	t.Helper()
	var g domain.Graph
	if err := yaml.Unmarshal([]byte(sample), &g); err != nil {
		t.Fatal(err)
	}
	return &g
}

func TestValidateAndNormalize(t *testing.T) {
	g := load(t)
	// channel ids with dots are invalid; fix to exercise the topic path
	g.Spec.Channels[4].ID = "events-review"
	g.Spec.Channels[4].Topic = "events.review"
	r := Validate(g, nil)
	if !r.Valid {
		t.Fatalf("expected valid, got %+v", r.Errors)
	}
	if g.Metadata.ID != "review-loop" {
		t.Fatalf("slug: %q", g.Metadata.ID)
	}
	dev := g.Node("dev")
	if dev.Loop.Mode != domain.LoopEvent {
		t.Fatalf("consumer should default to event mode, got %s", dev.Loop.Mode)
	}
	if len(dev.Tools) != 2 || dev.Tools[1].Commands[0] != "go *" {
		t.Fatalf("tool grants not parsed: %+v", dev.Tools)
	}
	if got := Substitute(dev.Prompt, Params(g)); got != "implement compilers from @input" {
		t.Fatalf("substitute: %q", got)
	}
	// auditor subscribes by pattern to the topic
	if in := g.Incoming("auditor"); len(in) != 1 || in[0].ID != "events-review" {
		t.Fatalf("subscription not resolved: %+v", in)
	}
	// broadcast creates one group per replica
	groups := SubjectGroups(g, g.Channel("alarm"), func(string) int { return 3 })
	if len(groups) != 3 {
		t.Fatalf("broadcast groups: %v", groups)
	}
	// queue: one shared group
	if gs := ConsumerGroups(g, "dev", 0); len(gs) != 2 || gs[0].Group != "tasks" {
		t.Fatalf("dev groups: %+v", gs)
	}
}

func TestValidateErrors(t *testing.T) {
	g := load(t)
	g.Spec.Channels[1].To = []string{"ghost"}
	g.Spec.Channels[2].When = "payload.verdict ==="
	g.Spec.Nodes[0].Loop = domain.LoopSpec{Mode: domain.LoopEvent, Stop: domain.StopSpec{Policy: domain.StopAuto}}
	r := Validate(g, nil)
	if r.Valid || len(r.Errors) < 3 {
		t.Fatalf("expected errors, got %+v", r)
	}
}

func TestValidateDeclaredChecks(t *testing.T) {
	g := load(t)
	g.Spec.Channels[4].ID = "events-review" // the fixture's dotted id is not under test here
	g.Spec.Checks = []domain.Check{{Name: "test", Command: "go test ./..."}, {Name: "lint", Command: "go vet ./..."}}
	if r := Validate(g, nil); !r.Valid {
		t.Fatalf("valid checks rejected: %+v", r.Errors)
	}
	g.Spec.Checks = append(g.Spec.Checks, domain.Check{Name: "test", Command: "make test"})
	r := Validate(g, nil)
	if r.Valid {
		t.Fatal("duplicate check accepted")
	}
	var found bool
	for _, e := range r.Errors {
		found = found || (e.Path == "spec.checks" && strings.Contains(e.Message, "duplicate"))
	}
	if !found {
		t.Fatalf("errors %+v", r.Errors)
	}
}

func TestRouting(t *testing.T) {
	g := load(t)
	g.Spec.Channels[4].ID = "events-review"
	Validate(g, nil)
	rt, err := NewRouter(g)
	if err != nil {
		t.Fatal(err)
	}
	ok, _ := domain.NewMessage("review.done", "reviewer", "", map[string]any{"verdict": "approved"})
	res := rt.Route("reviewer", ok, "")
	names := ids(res.Channels)
	if names != "approved,events-review" {
		t.Fatalf("approved routes: %s (%s)", names, res.Reason)
	}
	bad, _ := domain.NewMessage("review.done", "reviewer", "", map[string]any{"verdict": "changes"})
	if got := ids(rt.Route("reviewer", bad, "").Channels); got != "rework,events-review" {
		t.Fatalf("rework routes: %s", got)
	}
	// target narrows to channels reaching that node
	bad.Target = "dev"
	if got := ids(rt.Route("reviewer", bad, "").Channels); got != "rework" {
		t.Fatalf("target route: %s", got)
	}
	// type filter
	m, _ := domain.NewMessage("code.ready", "dev", "", nil)
	if got := ids(rt.Route("dev", m, "").Channels); got != "code" {
		t.Fatalf("type filter: %s", got)
	}
	m.Type = "alarm.fire"
	if got := ids(rt.Route("dev", m, "").Channels); got != "code,alarm" {
		t.Fatalf("alarm: %s", got)
	}
}

func TestSchedule(t *testing.T) {
	for _, s := range []string{"@every 1h", "*/5 * * * *", "@hourly"} {
		if _, err := ParseSchedule(s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
	if _, err := ParseSchedule("nope"); err == nil {
		t.Fatal("expected error")
	}
}

func ids(cs []*domain.Channel) string {
	out := ""
	for i, c := range cs {
		if i > 0 {
			out += ","
		}
		out += c.ID
	}
	return out
}
