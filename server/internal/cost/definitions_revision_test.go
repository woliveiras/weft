package cost

import "testing"

func TestDefinitionRevisionsTrackOnlyChangedObjects(t *testing.T) {
	initial := []Definition{{Scope: Installation, ID: "*", LimitMicros: 10}, {Scope: User, ID: "alice", LimitMicros: 20}}
	// Legacy definitions acquire no new revision merely because another object is saved.
	revs := definitionRevisions(initial, initial, nil, 3)
	if revs["installation/*"] != 0 || revs["user/alice"] != 0 {
		t.Fatal(revs)
	}
	changed := append([]Definition(nil), initial...)
	changed[0].LimitMicros = 11
	revs = definitionRevisions(initial, changed, revs, 4)
	if revs["installation/*"] != 4 || revs["user/alice"] != 0 {
		t.Fatal(revs)
	}
	revs = definitionRevisions(changed, initial, revs, 5)
	if revs["installation/*"] != 5 || revs["user/alice"] != 0 {
		t.Fatal("restoring a value must advance its revision", revs)
	}
	deleted := definitionRevisions(initial, initial[1:], revs, 6)
	if _, ok := deleted["installation/*"]; ok {
		t.Fatal("removed objects must not accumulate revision metadata")
	}
	revs = definitionRevisions(initial[1:], initial, deleted, 7)
	if revs["installation/*"] != 7 || revs["user/alice"] != 0 {
		t.Fatal("recreation must not reuse the deleted revision", revs)
	}
}
