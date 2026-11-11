package api_test

import (
	"testing"
	"time"
)

const activityGraph = `
apiVersion: weft.io/v1
kind: Graph
metadata: {name: activity-demo}
spec:
  defaults: {runtime: {kind: echo}}
  nodes: [{id: maker}, {id: shipper}]
  channels:
    - {id: in, to: [maker]}
    - {id: release, from: maker, to: [shipper], approval: true}
`

type activityResponse struct {
	Window   string `json:"window"`
	Counters struct {
		Running  *int `json:"running"`
		Waiting  *int `json:"waiting"`
		Failed   *int `json:"failed"`
		Finished *int `json:"finished"`
	} `json:"counters"`
	NeedsYou []struct {
		Kind, ID, GraphID, GraphName, NodeID string
	} `json:"needsYou"`
	Running []struct {
		GraphID, GraphName, Workflow, State string
		Replicas                            []struct{ NodeID, State, Phase string }
	} `json:"running"`
	History []struct {
		ID, GraphID, NodeID, Status, Workflow string
		DurationMs                            int64
		CostMicros                            *int64
	} `json:"history"`
	Options *struct {
		Agents []string `json:"agents"`
	} `json:"options"`
	Errors []struct{ Section, Code, Message string } `json:"errors"`
}

func (a activityResponse) errorCode(section string) string {
	for _, e := range a.Errors {
		if e.Section == section {
			return e.Code
		}
	}
	return ""
}

// The Activity view reads what the real engine records: a gated hand-off
// waits for a person, the producing turn appears in history, the graph's
// live replicas appear as running, and deciding the approval moves the work
// on. Reviews follow the existing role gate.
func TestActivityAPIWithEngine(t *testing.T) {
	srv := setup(t)
	if c := call(t, srv, "GET", "/api/v1/activity", "", "", "", nil); c != 401 {
		t.Fatalf("anonymous: %d", c)
	}
	for _, q := range []string{"window=1m", "window=9d", "window=x", "limit=0", "limit=500", "status=bogus"} {
		var e struct{ Code string }
		if c := call(t, srv, "GET", "/api/v1/activity?"+q, viewerTok, "", "", &e); c != 400 || e.Code != "invalid" {
			t.Fatalf("%s: %d %+v", q, c, e)
		}
	}
	var empty activityResponse
	if c := call(t, srv, "GET", "/api/v1/activity", viewerTok, "", "", &empty); c != 200 || empty.Window != "24h" || *empty.Counters.Waiting != 0 || len(empty.History) != 0 || empty.History == nil {
		t.Fatalf("empty installation: %d %+v", c, empty)
	}

	if c := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/yaml", activityGraph, nil); c != 201 {
		t.Fatalf("create: %d", c)
	}
	if c := call(t, srv, "POST", "/api/v1/graphs/activity-demo/start", adminTok, "", "", nil); c != 200 {
		t.Fatalf("start: %d", c)
	}
	if c := call(t, srv, "POST", "/api/v1/graphs/activity-demo/channels/in/messages", operatorTok, "application/json", `{"type":"build","payload":{"v":1}}`, nil); c != 201 {
		t.Fatalf("inject: %d", c)
	}

	var v activityResponse
	waitFor(t, 15*time.Second, "the held hand-off in Needs you", func() bool {
		v = activityResponse{}
		call(t, srv, "GET", "/api/v1/activity", viewerTok, "", "", &v)
		return len(v.NeedsYou) == 1 && len(v.History) == 1 && v.History[0].Status == "succeeded"
	})
	if n := v.NeedsYou[0]; n.Kind != "message" || n.GraphID != "activity-demo" || n.GraphName != "activity-demo" || *v.Counters.Waiting != 1 {
		t.Fatalf("needs you: %+v waiting %d", v.NeedsYou, *v.Counters.Waiting)
	}
	if h := v.History[0]; h.NodeID != "maker" || h.Workflow != "activity-demo" || h.CostMicros == nil || *v.Counters.Finished != 1 || *v.Counters.Failed != 0 {
		t.Fatalf("history: %+v counters %+v", v.History, v.Counters)
	}
	if len(v.Running) != 1 || v.Running[0].GraphID != "activity-demo" || v.Running[0].State != "running" || len(v.Running[0].Replicas) != 2 {
		t.Fatalf("running: %+v", v.Running)
	}
	if v.errorCode("reviews") != "forbidden" {
		t.Fatalf("a viewer must be told reviews are hidden: %+v", v.Errors)
	}
	var op activityResponse
	call(t, srv, "GET", "/api/v1/activity", operatorTok, "", "", &op)
	if op.errorCode("reviews") == "forbidden" || len(op.NeedsYou) != 1 {
		t.Fatalf("operator: %+v", op.Errors)
	}

	// Filters narrow history only.
	var filtered activityResponse
	call(t, srv, "GET", "/api/v1/activity?status=failed&agent=maker&window=1h", viewerTok, "", "", &filtered)
	if len(filtered.History) != 0 || *filtered.Counters.Finished != 1 || filtered.Window != "1h" {
		t.Fatalf("filtered: %+v", filtered)
	}

	if c := call(t, srv, "POST", "/api/v1/approvals/"+v.NeedsYou[0].ID+"/approve", operatorTok, "", "", nil); c != 200 {
		t.Fatalf("approve: %d", c)
	}
	waitFor(t, 15*time.Second, "the shipper turn after approval", func() bool {
		v = activityResponse{}
		call(t, srv, "GET", "/api/v1/activity?status=succeeded", viewerTok, "", "", &v)
		return len(v.NeedsYou) == 0 && len(v.History) == 2 && v.History[0].NodeID == "shipper"
	})
	if *v.Counters.Waiting != 0 || *v.Counters.Finished != 2 || len(v.Options.Agents) != 2 {
		t.Fatalf("after approval: %+v %+v", v.Counters, v.Options)
	}

	if c := call(t, srv, "POST", "/api/v1/graphs/activity-demo/stop", adminTok, "", "", nil); c != 200 {
		t.Fatalf("stop: %d", c)
	}
	waitFor(t, 15*time.Second, "the stopped graph to leave Running now", func() bool {
		v = activityResponse{}
		call(t, srv, "GET", "/api/v1/activity", viewerTok, "", "", &v)
		return len(v.Running) == 0 && v.Running != nil && *v.Counters.Running == 0
	})
}
