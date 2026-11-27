package chat

import "testing"

func TestPickerCommandBindsOnlyTheFirstLine(t *testing.T) {
	for _, tc := range []struct {
		text, kind, target string
		matched            bool
	}{
		{"/follow tsk_a1\nUse tsk_b2 instead", WatchTask, "tsk_a1", true},
		{"/lookup agents reader\nnotes", FindResource, "reader", true},
		{"/follow some title", Clarify, "", true},
		{"/lookup administration a", Clarify, "", true},
		{"/lookup tasks ../bad", Clarify, "", true},
		{"follow task tsk_a1", "", "", false},
	} {
		plan, ok := PickerCommand(tc.text)
		if ok != tc.matched || plan.Kind != tc.kind || plan.Task+plan.Query != tc.target {
			t.Errorf("%q: %+v, %v", tc.text, plan, ok)
		}
	}
}
