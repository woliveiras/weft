package activity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/cost"
	"github.com/woliveiras/weft/server/internal/domain"
)

func TestParseHealthQuery(t *testing.T) {
	for in, want := range map[string]time.Duration{"": 24 * time.Hour, "24h": 24 * time.Hour, "7d": 7 * 24 * time.Hour} {
		q, err := ParseHealthQuery(url.Values{"window": {in}})
		if err != nil || q.Window != want || q.Ranking != DefaultRanking {
			t.Errorf("window %q = %+v, %v", in, q, err)
		}
	}
	for _, v := range []url.Values{{"window": {"1h"}}, {"window": {"8d"}}, {"window": {"1d"}}, {"ranking": {"0"}}, {"ranking": {"51"}}, {"ranking": {"x"}}} {
		if _, err := ParseHealthQuery(v); !errors.Is(err, ErrInvalid) {
			t.Errorf("ParseHealthQuery(%v) accepted: %v", v, err)
		}
	}
	if q, err := ParseHealthQuery(url.Values{"ranking": {"3"}}); err != nil || q.Ranking != 3 {
		t.Fatalf("ranking: %+v %v", q, err)
	}
}

func TestNormalizeError(t *testing.T) {
	cases := map[string]string{
		"":                       NoErrorMessage,
		"  \n\t ":                NoErrorMessage,
		"\n  first line\nsecond": "first line",
		"go test failed":         "go test failed",
		"exit status 1\nFAIL\tgithub.com/x/y 0.2s":                     "exit status <n>",
		"read /home/ana/work/task-run-17/main.go: permission denied":   "read <path>: permission denied",
		`open C:\Users\ana\file.txt failed`:                            "open <path> failed",
		"internal/api/server.go:42:7: undefined: foo":                  "<path>: undefined: foo",
		"execution 01JC2W7XQ9ZK3V4 timed out after 30s":                "execution <id> timed out after <n>",
		"execution 7f9c2ba4-e88f-4ab1-9d3c-1b2a3c4d5e6f not found":     "execution <id> not found",
		"container 3f4e9a0b1c2d exited with 137":                       "container <id> exited with <n>",
		"POST https://api.example.com/v1/chat?key=abc: 500":            "POST <url>: <n>",
		"notify ana@example.com failed":                                "notify <email> failed",
		"provider said 401: Authorization: Bearer sk-abcdefghijklmnop": "provider said <n>: Authorization: <redacted>",
		"bad request api_key=AIzaSyA1234567890abcdef&x=1":              "bad request api_key=<redacted>&x=<n>",
		`{"password": "hunter2hunter2"}`:                               `{"password": "<redacted>"}`,
		"token ghp_abcdefghijklmnopqrstuvwx rejected":                  "token <redacted> rejected",
		`tool "write_file" denied`:                                     `tool "write_file" denied`,
		`model replied "` + strings.Repeat("secret plan ", 10) + `"`:   "model replied <text>",
		"token budget exhausted for this run":                          "token budget exhausted for this run",
		"token refreshing_credentials failed":                          "token refreshing_credentials failed",
		"invalid token abcdef1234567xyz":                               "invalid token <redacted>",
		"model gpt4 is not available":                                  "model gpt4 is not available",
		"too   many\tspaces":                                           "too many spaces",
	}
	for in, want := range cases {
		if got := NormalizeError(in); got != want {
			t.Errorf("NormalizeError(%q)\n got %q\nwant %q", in, got, want)
		}
	}
	// One failure seen in many runs normalizes to one group.
	a := NormalizeError("workspace /var/lib/weft/ws/task-run-41/repo: checkout of 9a8b7c6d5e4f failed after 12 attempts")
	b := NormalizeError("workspace /var/lib/weft/ws/task-run-97/repo: checkout of 0f1e2d3c4b5a failed after 3 attempts")
	if a != b {
		t.Fatalf("not grouped:\n%q\n%q", a, b)
	}
	if got := NormalizeError(strings.Repeat("é", 500)); len([]rune(got)) != maxNormalizedRunes+1 {
		t.Fatalf("not bounded: %d runes", len([]rune(got)))
	}
}

func failExec(t *testing.T, s interface {
	PutExecution(context.Context, *domain.Execution) error
}, id, graph, node string, st domain.ExecutionStatus, started time.Time, dur time.Duration, msg string, usage domain.Usage) {
	t.Helper()
	e := &domain.Execution{ID: id, GraphID: graph, NodeID: node, Status: st, StartedAt: started, Error: msg, Usage: usage}
	if st.Terminal() {
		e.CompletedAt = ptr(started.Add(dur))
	}
	must(t, s.PutExecution(context.Background(), e))
}

func TestHealthAggregatesRanksAndLinks(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	saveGraph(t, s, "wf", "Software factory", nil)
	saveGraph(t, s, "task-run-1", "Fix login", map[string]string{domain.LabelTask: "task-1", domain.LabelTaskWorkflow: "wf"})
	saveGraph(t, s, "docs", "Docs", nil)
	u := domain.Usage{InputTokens: 100, OutputTokens: 10}
	// Outside the window: ignored.
	failExec(t, s, "old", "wf", "tester", domain.ExecFailed, now.Add(-25*time.Hour), time.Second, "ancient", u)
	// tester (wf, partly through a task run): 3 failed of 4 finished.
	failExec(t, s, "t1", "wf", "tester", domain.ExecFailed, now.Add(-5*time.Hour), 10*time.Second, "read /tmp/run-1/out.txt: no such file", u)
	failExec(t, s, "t2", "task-run-1", "tester", domain.ExecFailed, now.Add(-4*time.Hour), 20*time.Second, "read /tmp/run-2/out.txt: no such file", u)
	failExec(t, s, "t3", "wf", "tester", domain.ExecInterrupted, now.Add(-3*time.Hour), 30*time.Second, "", u)
	failExec(t, s, "t4", "wf", "tester", domain.ExecSucceeded, now.Add(-2*time.Hour), 40*time.Second, "", u)
	// coder (wf): slow and expensive, all good; one still running.
	for i := 0; i < 10; i++ {
		failExec(t, s, fmt.Sprintf("c%d", i), "wf", "coder", domain.ExecSucceeded, now.Add(-time.Duration(20-i)*time.Hour), time.Duration(i+1)*time.Minute, "", u)
	}
	failExec(t, s, "c-run", "task-run-1", "coder", domain.ExecRunning, now.Add(-time.Minute), 0, "", u)
	// writer (docs): one cancelled run.
	failExec(t, s, "w1", "docs", "writer", domain.ExecCancelled, now.Add(-time.Hour), time.Second, "cancelled", u)
	event(t, s, domain.EvCostRecorded, "wf", "", now.Add(-10*time.Hour), cost.Record{Execution: "c5", CostMicros: 9000, Priced: true, Currency: "USD"})
	event(t, s, domain.EvCostRecorded, "wf", "", now.Add(-4*time.Hour), cost.Record{Execution: "t2", CostMicros: 100, Priced: true, Currency: "USD"})

	b := Builder{Store: s, Now: func() time.Time { return now }}
	v, err := b.Health(ctx, HealthQuery{})
	must(t, err)
	if v.Window != "24h" || v.Currency != "USD" || len(v.Errors) != 0 || v.Truncated != (HealthTruncated{}) || v.Limits.Ranking != DefaultRanking {
		t.Fatalf("meta: %+v", v)
	}
	agents := map[string]HealthRow{}
	for _, r := range v.Agents {
		agents[r.Key] = r
	}
	tester := agents["wf/tester"]
	if tester.Runs != 4 || tester.Finished != 4 || tester.Failed != 3 || tester.Succeeded != 1 || *tester.FailureRate != 0.75 || tester.WorkflowName != "Software factory" || tester.NodeID != "tester" {
		t.Fatalf("tester: %+v", tester)
	}
	if *tester.P50Ms != 20_000 || *tester.P90Ms != 40_000 || tester.Usage.InputTokens != 400 || *tester.CostMicros != 100 {
		t.Fatalf("tester stats: p50 %d p90 %d %+v cost %d", *tester.P50Ms, *tester.P90Ms, tester.Usage, *tester.CostMicros)
	}
	if len(tester.TopErrors) != 2 || tester.TopErrors[0].Message != "read <path>: no such file" || tester.TopErrors[0].Count != 2 ||
		tester.TopErrors[0].LastRun.ID != "t2" || tester.TopErrors[0].LastRun.GraphID != "task-run-1" || !tester.TopErrors[0].LastRun.StartedAt.Equal(now.Add(-4*time.Hour)) ||
		tester.TopErrors[1].Message != NoErrorMessage || tester.OtherErrors != 0 {
		t.Fatalf("tester errors: %+v", tester.TopErrors)
	}
	if tester.LastRun.ID != "t4" || tester.LastFailure.ID != "t3" {
		t.Fatalf("tester links: %+v %+v", tester.LastRun, tester.LastFailure)
	}
	coder := agents["wf/coder"]
	if coder.Runs != 11 || coder.Finished != 10 || *coder.FailureRate != 0 || *coder.P50Ms != 5*60_000 || *coder.P90Ms != 9*60_000 || coder.LastRun.ID != "c-run" || coder.LastFailure != nil {
		t.Fatalf("coder: %+v", coder)
	}
	if w := agents["docs/writer"]; w.Cancelled != 1 || w.Failed != 0 || *w.FailureRate != 0 || len(w.TopErrors) != 0 || w.TopErrors == nil {
		t.Fatalf("cancelled is not a failure: %+v", w)
	}
	if v.Agents[0].Key+" "+v.Agents[1].Key != "wf/coder wf/tester" {
		t.Fatalf("busiest first: %s %s", v.Agents[0].Key, v.Agents[1].Key)
	}
	wf := v.Workflows[0]
	if wf.Key != "wf" || wf.Runs != 15 || wf.Failed != 3 || *wf.CostMicros != 9100 || wf.NodeID != "" {
		t.Fatalf("workflow wf: %+v", wf)
	}
	r := v.Rankings
	if fmt.Sprint(r.FailsMost.Agents) != "[wf/tester]" || fmt.Sprint(r.FailsMost.Workflows) != "[wf]" {
		t.Fatalf("fails most: %+v", r.FailsMost)
	}
	if fmt.Sprint(r.Slowest.Agents) != "[wf/coder wf/tester docs/writer]" || fmt.Sprint(r.Slowest.Workflows) != "[wf docs]" {
		t.Fatalf("slowest: %+v", r.Slowest)
	}
	if fmt.Sprint(r.MostExpensive.Agents) != "[wf/coder wf/tester]" || fmt.Sprint(r.MostExpensive.Workflows) != "[wf]" {
		t.Fatalf("most expensive: %+v", r.MostExpensive)
	}

	// Ranking size is honoured.
	v, err = b.Health(ctx, HealthQuery{Ranking: 1})
	must(t, err)
	if fmt.Sprint(v.Rankings.Slowest.Agents) != "[wf/coder]" {
		t.Fatalf("ranking 1: %+v", v.Rankings.Slowest)
	}
	// The 7 d window reaches the older failure.
	v, err = b.Health(ctx, HealthQuery{Window: 7 * 24 * time.Hour})
	must(t, err)
	if v.Window != "7d" || v.Agents[1].Key != "wf/tester" || v.Agents[1].Runs != 5 {
		t.Fatalf("7d: %s %+v", v.Window, v.Agents[1])
	}

	// No secret, path or payload text reaches the response.
	failExec(t, s, "leak", "wf", "tester", domain.ExecFailed, now.Add(-time.Minute), time.Second, "401 from https://api.example.com: Bearer sk-livesecretvalue123\nfull prompt: tell me everything", u)
	v, err = b.Health(ctx, HealthQuery{})
	must(t, err)
	raw, _ := json.Marshal(v)
	for _, bad := range []string{"sk-livesecretvalue123", "api.example.com", "/tmp/run-1", "tell me everything", "ancient"} {
		if strings.Contains(string(raw), bad) {
			t.Errorf("response leaks %q", bad)
		}
	}
}

func TestHealthEmptyInstallation(t *testing.T) {
	v, err := Builder{Store: newStore(t), Now: func() time.Time { return now }}.Health(context.Background(), HealthQuery{})
	must(t, err)
	if v.Agents == nil || v.Workflows == nil || len(v.Agents) != 0 || v.Rankings == nil || v.Rankings.FailsMost.Agents == nil || v.Rankings.MostExpensive == nil {
		t.Fatalf("an empty installation is an empty success: %+v", v)
	}
}

func TestHealthTopErrorsAreBounded(t *testing.T) {
	s := newStore(t)
	for i := 0; i < TopErrors+3; i++ {
		for j := 0; j <= i; j++ { // error i occurs i+1 times
			failExec(t, s, fmt.Sprintf("x%d-%d", i, j), "wf", "a", domain.ExecFailed, now.Add(-time.Duration(i*10+j)*time.Minute), time.Second, fmt.Sprintf("step %c broke", 'a'+i), domain.Usage{})
		}
	}
	v, err := Builder{Store: s, Now: func() time.Time { return now }}.Health(context.Background(), HealthQuery{})
	must(t, err)
	row := v.Agents[0]
	if len(row.TopErrors) != TopErrors || row.TopErrors[0].Message != "step h broke" || row.TopErrors[0].Count != 8 {
		t.Fatalf("top errors: %+v", row.TopErrors)
	}
	if row.OtherErrors != 1+2+3 || row.Failed != 36 {
		t.Fatalf("other errors %d failed %d", row.OtherErrors, row.Failed)
	}
}

func TestHealthRowsKeepRankedOnesWhenCut(t *testing.T) {
	s := newStore(t)
	page := make([]*domain.Execution, 0, maxHealthRows+20)
	// Many busy, healthy agents, and one rare failing agent that would fall
	// off a busiest-first cut.
	for i := 0; i < maxHealthRows+19; i++ {
		for j := 0; j < 2; j++ {
			st := now.Add(-time.Duration(len(page)) * time.Second)
			page = append(page, &domain.Execution{ID: fmt.Sprintf("x%d-%d", i, j), GraphID: "wf", NodeID: fmt.Sprintf("n%03d", i), Status: domain.ExecSucceeded, StartedAt: st, CompletedAt: ptr(st)})
		}
	}
	st := now.Add(-time.Duration(len(page)) * time.Second)
	page = append(page, &domain.Execution{ID: "rare", GraphID: "wf", NodeID: "zz-rare", Status: domain.ExecFailed, StartedAt: st, CompletedAt: ptr(st)})
	v, err := Builder{Store: failing{Store: s, page: page}, Now: func() time.Time { return now }}.Health(context.Background(), HealthQuery{})
	must(t, err)
	if len(v.Agents) != maxHealthRows || !v.Truncated.Agents || v.Truncated.Workflows || v.Truncated.Executions {
		t.Fatalf("rows %d truncated %+v", len(v.Agents), v.Truncated)
	}
	found := false
	for _, r := range v.Agents {
		found = found || r.Key == "wf/zz-rare"
	}
	if !found || fmt.Sprint(v.Rankings.FailsMost.Agents) != "[wf/zz-rare]" {
		t.Fatal("a ranked row must stay listed")
	}
}

func TestHealthFullPageIsTruncated(t *testing.T) {
	page := make([]*domain.Execution, maxExecutions)
	for i := range page {
		page[i] = &domain.Execution{ID: fmt.Sprintf("x%04d", i), GraphID: "wf", NodeID: "a", Status: domain.ExecSucceeded, StartedAt: now.Add(-time.Duration(i) * time.Second)}
	}
	v, err := Builder{Store: failing{Store: newStore(t), page: page}, Now: func() time.Time { return now }}.Health(context.Background(), HealthQuery{})
	must(t, err)
	if !v.Truncated.Executions || v.Agents[0].Runs != maxExecutions {
		t.Fatalf("truncated %+v runs %d", v.Truncated, v.Agents[0].Runs)
	}
}

func TestHealthSectionFailures(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	failExec(t, s, "x1", "wf", "a", domain.ExecFailed, now.Add(-time.Hour), time.Second, "boom", domain.Usage{})
	boom := errors.New("database is locked")
	b := Builder{Store: failing{Store: s, executions: boom, events: boom}, Now: func() time.Time { return now }}
	v, err := b.Health(ctx, HealthQuery{})
	must(t, err)
	codes := map[string]string{}
	for _, e := range v.Errors {
		codes[e.Section] = e.Code
	}
	if codes["executions"] != "failed" || codes["cost"] != "failed" || v.Agents != nil || v.Workflows != nil || v.Rankings != nil {
		t.Fatalf("failed sections must be null with an error: %+v", v)
	}
	raw, _ := json.Marshal(v)
	if strings.Contains(string(raw), "database is locked") {
		t.Fatal("internal errors must not reach the response")
	}

	// Only cost failing: statistics stay, cost is unknown and not ranked.
	v, err = Builder{Store: failing{Store: s, events: boom}, Now: b.Now}.Health(ctx, HealthQuery{})
	must(t, err)
	if len(v.Agents) != 1 || v.Agents[0].CostMicros != nil || v.Rankings.MostExpensive != nil || len(v.Rankings.FailsMost.Agents) != 1 || len(v.Errors) != 1 {
		t.Fatalf("cost failure: %+v %+v", v.Agents, v.Errors)
	}

	if _, err := b.Health(canceled(), HealthQuery{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled request: %v", err)
	}
}

func TestPercentile(t *testing.T) {
	vals := []int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	if percentile(vals, 0.5) != 5 || percentile(vals, 0.9) != 9 || percentile([]int64{7}, 0.9) != 7 || percentile([]int64{1, 2}, 0.5) != 1 {
		t.Fatal("nearest rank")
	}
}
