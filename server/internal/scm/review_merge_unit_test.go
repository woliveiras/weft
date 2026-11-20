package scm

import "testing"

func TestMergeCapabilityRequiresQualifiedRemoteContract(t *testing.T) {
	for _, k := range Kinds() {
		if k.AutomaticMerge {
			t.Fatalf("%s advertises unqualified automatic merge", k.ID)
		}
	}
}
