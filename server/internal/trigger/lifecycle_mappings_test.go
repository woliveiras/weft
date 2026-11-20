package trigger

import (
	"encoding/json"
	"testing"
	"time"
)

func TestLifecycleMappingPinsAreExplicitAndLegacyEncodingIsUnchanged(t *testing.T) {
	for _, v := range []LifecycleMappings{{WorkStarted: MappingPin{Name: "review"}}, {PRDelivered: MappingPin{Revision: 1}}, {WorkStarted: MappingPin{Name: "../review", Revision: 1}}} {
		if v.validate(SourceConfig{Kind: "jira-dc"}) == nil {
			t.Fatal(v)
		}
	}
	good := LifecycleMappings{WorkStarted: MappingPin{Name: "review", Revision: 1}}
	if good.validate(SourceConfig{Kind: "jira-dc"}) != nil || good.validate(SourceConfig{Kind: "jira"}) == nil {
		t.Fatal("edition validation failed")
	}
	raw, err := json.Marshal(Input{})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		t.Fatal("invalid encoding")
	}
	if _, found := fields["lifecycleMappings"]; found {
		t.Fatal("new empty field changes legacy consent encoding")
	}
	if good.pin("unknown") != (MappingPin{}) || good.pin("work_started").Name != "review" {
		t.Fatal("stage lookup")
	}
}
func TestLifecycleSyncIdentityAndStateValidation(t *testing.T) {
	v := LifecycleSync{TriggerID: "trigger", TaskID: "task", Stage: "work_started", Evidence: "execution", IssueID: "10007", IssueTypeID: "10002", IssueKey: "APP-7", Source: SourceConfig{Kind: "jira-dc", ConnectionID: "native", SiteURL: "https://jira.example", ProjectID: "10001"}, ConnectionRevision: 1, Mapping: MappingPin{Name: "review", Revision: 1}, ConsentScope: "scope", Actor: "admin", State: "queued"}
	v.CreatedAt = time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	v.UpdatedAt = v.CreatedAt
	v.ID = lifecycleID(v.TriggerID, v.TaskID, v.Stage)
	if !v.valid() {
		t.Fatal(v)
	}
	for _, edit := range []func(*LifecycleSync){func(v *LifecycleSync) { v.TaskID = "other" }, func(v *LifecycleSync) { v.Stage = "merged" }, func(v *LifecycleSync) { v.IssueID = "APP-7" }, func(v *LifecycleSync) { v.Mapping.Revision = 0 }, func(v *LifecycleSync) { v.Source.SiteURL = "http://jira.example" }, func(v *LifecycleSync) { v.State = "done" }} {
		other := v
		edit(&other)
		if other.valid() {
			t.Fatal(other)
		}
	}
}
