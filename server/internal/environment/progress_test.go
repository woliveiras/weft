package environment

import (
	"context"
	"reflect"
	"testing"
)

func TestBuildReasons(t *testing.T) {
	for _, tc := range []struct {
		view  View
		force bool
		want  []string
	}{
		{View{}, false, []string{"not_built"}},
		{View{Status: Status{State: StateFailed}}, false, []string{"retry_failed"}},
		{View{ChangeReasons: []string{"definition_changed"}}, false, []string{"definition_changed"}},
		{View{ChangeReasons: []string{"definition_changed"}}, true, []string{"explicit_rebuild"}},
	} {
		if got := buildReasons(&tc.view, tc.force); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("got %v want %v", got, tc.want)
		}
	}
}
func TestProgressSnapshot(t *testing.T) {
	ctx := progressContext(context.Background())
	var st Status
	setProgress(ctx, "waiting_capacity", "")
	snapshotProgress(ctx, &st)
	if st.Phase != "waiting_capacity" {
		t.Fatal(st)
	}
	setProgress(ctx, "setup", "compatible")
	snapshotProgress(ctx, &st)
	if st.Phase != "setup" || st.ReuseDecision != "compatible" {
		t.Fatal(st)
	}
	setProgress(context.Background(), "ignored", "ignored")
}
