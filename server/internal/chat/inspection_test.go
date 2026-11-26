package chat

import (
	"strings"
	"testing"
	"time"
)

func TestConsoleInspectionLimitsAndCommands(t *testing.T) {
	i := Inspection{Area: "agents"}
	i.Add("password=private-example")
	i.Add("hidden\nsecond line")
	for range 10 {
		i.Add("agent · version 2")
	}
	got := i.Text(time.Unix(0, 0))
	if strings.Contains(got, "private") || strings.Contains(got, "second line") || strings.Count(got, "• ") != InspectionLimit || !strings.Contains(got, "Additional records") {
		t.Fatal(got)
	}
	for _, input := range []string{`{"kind":"console.inspect","inspectArea":"shell"}`, `{"kind":"unsupported","inspectArea":"models"}`, `{"kind":"console.inspect","inspectArea":"budgets","task":"edit"}`} {
		if _, err := decodePlan(input); err == nil {
			t.Fatalf("accepted %s", input)
		}
	}
	if p, err := decodePlan(`{"kind":"console.inspect","inspectArea":"skills"}`); err != nil || p.Area != "skills" {
		t.Fatal(p, err)
	}
	if r, ok := ExplicitInspection(" /inspect usage "); !ok || r.Plan.Kind != InspectConsole {
		t.Fatal(r)
	}
	if r, ok := ExplicitInspection("/inspect /arbitrary"); !ok || r.Text == "" || r.Plan.Kind != "" {
		t.Fatal(r)
	}
	if _, ok := ExplicitInspection("open a workflow"); ok {
		t.Fatal("unexpected shortcut")
	}
}
