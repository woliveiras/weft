package chat

import (
	"errors"
	"testing"
)

func TestTaskQueryValidatesTheDeclaredSchema(t *testing.T) {
	for _, raw := range []string{`{}`, `{"mode":"count","limit":1}`, `{"mode":"list","limit":0}`, `{"mode":"list","limit":null}`, `{"mode":"list","limit":9}`, `{"mode":"list","limit":1.2}`, `{"mode":"list","status":"finished"}`, `{"mode":"exists","status":null}`, `{"mode":"list","status":""}`, `{"mode":"list","mode":"count"}`, `{"Mode":"count"}`, `{"mode":"count","sql":"SELECT"}`, `{"mode":"drop"}`, `{"mode":"count"} []`} {
		if _, err := ParseTaskQuery([]byte(raw)); !errors.Is(err, ErrInvalidPlan) {
			t.Errorf("accepted %s", raw)
		}
	}
	for _, tc := range []struct {
		raw  string
		want TaskQuery
	}{
		{`{"mode":"list"}`, TaskQuery{Mode: "list", Limit: InspectionLimit}},
		{`{"mode":"exists","status":"running"}`, TaskQuery{Mode: "exists", Status: "running"}},
		{`{"mode":"list","status":"succeeded","limit":2,"title":"Tarefas recentes"}`, TaskQuery{Mode: "list", Status: "succeeded", Limit: 2, Title: "Tarefas recentes"}},
		{`{"mode":"count","title":"https://private.example/path"}`, TaskQuery{Mode: "count"}},
	} {
		got, err := ParseTaskQuery([]byte(tc.raw))
		if err != nil || got != tc.want {
			t.Errorf("%s => %+v, %v", tc.raw, got, err)
		}
	}
}

func TestTaskQueryProjectsOnlyMatchingSafeItems(t *testing.T) {
	q := TaskQueryResult{Mode: "list", Status: "running"}
	for _, row := range []TaskQueryItem{
		{ID: "tsk_a", Status: "failed", Title: "Failed"},
		{ID: "tsk_b", Status: "running", Title: "https://private.example/path"},
		{ID: "tsk_c", Status: "running", Title: "Other"},
	} {
		if err := q.Add(row, 1); err != nil {
			t.Fatal(err)
		}
	}
	if q.Total != 2 || len(q.Items) != 1 || q.Items[0].ID != "tsk_b" || q.Items[0].Title != "" {
		t.Fatalf("query result=%+v", q)
	}
	if err := q.Add(TaskQueryItem{ID: "unexpected/id", Status: "running"}, 1); !errors.Is(err, ErrReadUnavailable) {
		t.Fatal(err)
	}
	if err := q.Add(TaskQueryItem{ID: "tsk_d", Status: "not a status"}, 1); !errors.Is(err, ErrReadUnavailable) {
		t.Fatal(err)
	}
}
