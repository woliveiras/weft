package localinstall

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/woliveiras/weft/server/internal/delegation"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage/stores"
	"github.com/woliveiras/weft/server/internal/taskconfig"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNativeStartPersistRestartAndOccupiedPort(t *testing.T) {
	output, err := os.CreateTemp(t.TempDir(), "native-console")
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stdout
	os.Stdout = output
	defer func() {
		os.Stdout = original
		defer output.Close()
		data, err := os.ReadFile(output.Name())
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"INFO   Starting local Weft", "Weft is ready. Open", "closing browser streams", "Weft stopped; persistent data is retained"} {
			if !strings.Contains(string(data), want) {
				t.Errorf("missing console message %q", want)
			}
		}
		if strings.Contains(string(data), `"msg":`) {
			t.Error("native default emitted JSON logs")
		}
	}()
	root, web := fixture(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err = Init(root, web, port); err != nil {
		t.Fatal(err)
	}
	// An occupied port fails without admitting background work or replacing data.
	if err = Run(t.Context(), root); err == nil {
		t.Fatal("occupied port accepted")
	}
	if err = listener.Close(); err != nil {
		t.Fatal(err)
	}
	// This credential was generated exclusively inside this disposable test fixture.
	b, err := os.ReadFile(filepath.Join(root, "admin.token"))
	if err != nil {
		t.Fatal(err)
	}
	token := strings.TrimSpace(string(b))
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	request := func(method, path, body string, out any) int {
		t.Helper()
		req, err := http.NewRequest(method, base+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if out != nil {
			if err = json.NewDecoder(res.Body).Decode(out); err != nil {
				t.Fatal(err)
			}
		} else {
			_, _ = io.Copy(io.Discard, res.Body)
		}
		return res.StatusCode
	}
	start := func() (context.CancelFunc, chan error) {
		t.Helper()
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)
		go func() { done <- Run(ctx, root) }()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			res, err := http.Get(base + "/healthz")
			if err == nil {
				res.Body.Close()
				if res.StatusCode == 200 {
					return cancel, done
				}
			}
			select {
			case err := <-done:
				cancel()
				t.Fatal("native startup", err)
			default:
			}
			time.Sleep(20 * time.Millisecond)
		}
		cancel()
		t.Fatal("native startup timeout")
		return nil, nil
	}
	stop := func(cancel context.CancelFunc, done chan error) {
		t.Helper()
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("native shutdown timeout")
		}
	}
	cancel, done := start()
	var info struct {
		NativeProfile bool `json:"nativeProfile"`
	}
	if status := request("GET", "/api/v1/info", "", &info); status != 200 || !info.NativeProfile {
		t.Fatal("native capability missing", status)
	}
	if c := request("GET", "/", "", nil); c != 200 {
		t.Fatal(c)
	}
	if c := request("POST", "/api/v1/graphs", `{"metadata":{"id":"fixture"},"spec":{"nodes":[{"id":"worker","runtime":{"kind":"echo"}}]}}`, nil); c != 201 {
		t.Fatal(c)
	}
	var factory struct {
		Graph  domain.Graph       `json:"graph"`
		Record domain.GraphRecord `json:"record"`
	}
	if code := request("GET", "/api/v1/graphs/software-factory", "", &factory); code != 200 || factory.Graph.Metadata.Version != 1 || factory.Record.State != domain.GraphStopped {
		t.Fatal("factory not seeded stopped", code)
	}
	var v2 struct {
		Graph  domain.Graph       `json:"graph"`
		Record domain.GraphRecord `json:"record"`
	}
	if code := request("GET", "/api/v1/graphs/software-factory-v2", "", &v2); code != 200 || v2.Record.State != domain.GraphStopped || v2.Graph.Node("verifier") == nil {
		t.Fatal("factory v2 not seeded stopped", code)
	}
	factory.Graph.Metadata.Name = "Personal factory"
	factoryJSON, e := json.Marshal(factory.Graph)
	if e != nil {
		t.Fatal(e)
	}
	if code := request("PUT", "/api/v1/graphs/software-factory", string(factoryJSON), nil); code != 200 {
		t.Fatal(code)
	}
	var draft taskconfig.Draft
	if c := request("POST", "/api/v1/tasks", `{"title":"Native fixture","request":"Synthetic request","outcome":"freeform","graphId":"fixture","graphVersion":1,"nodeId":"worker"}`, &draft); c != 201 {
		t.Fatal(c)
	}
	queued := `{"metadata":{"id":"queued"},"spec":{"nodes":[{"id":"worker","runtime":{"kind":"echo"},"loop":{"mode":"event"}}],"channels":[{"id":"pending","to":["worker"]},{"id":"held","to":["worker"],"approval":true}]}}`
	if c := request("POST", "/api/v1/graphs", queued, nil); c != 201 {
		t.Fatal(c)
	}
	for _, channel := range []string{"pending", "held"} {
		if c := request("POST", "/api/v1/graphs/queued/channels/"+channel+"/messages", `{"type":"fixture","payload":{"test":true}}`, nil); c != 201 {
			t.Fatal(c)
		}
	}
	var held []domain.Approval
	if c := request("GET", "/api/v1/approvals?status=pending", "", &held); c != 200 || len(held) != 1 {
		t.Fatal("missing durable gate", c)
	}
	// An authenticated browser keeps SSE open while the service is stopped.
	streamRequest, err := http.NewRequest("GET", base+"/api/v1/events/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	streamRequest.Header.Set("Authorization", "Bearer "+token)
	streamResponse, err := http.DefaultClient.Do(streamRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer streamResponse.Body.Close()
	if streamResponse.StatusCode != 200 {
		t.Fatal(streamResponse.StatusCode)
	}
	streamClosed := make(chan error, 1)
	go func() { _, err := io.Copy(io.Discard, streamResponse.Body); streamClosed <- err }()
	stop(cancel, done)
	select {
	case err := <-streamClosed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("event stream outlived shutdown")
	}
	// Simulate a crashed native worker using only this disposable stopped profile.
	ledger, err := stores.Open(t.Context(), stores.Config{Type: "sqlite", DSN: filepath.Join(root, "weft.db")})
	if err != nil {
		t.Fatal(err)
	}
	budgets := delegation.Manager{Store: ledger}
	for _, id := range []string{"native-crashed", "foreign-active"} {
		instance := "weft-native"
		if id == "foreign-active" {
			instance = "foreign-worker"
		}
		if err := ledger.PutExecution(t.Context(), &domain.Execution{ID: id, GraphID: "software-factory", NodeID: "planner", Status: domain.ExecRunning, Instance: instance, StartedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
		for _, call := range []string{"pending", "dispatched"} {
			if err := budgets.ReserveTokens(t.Context(), id, call, 100, 30); err != nil {
				t.Fatal(err)
			}
		}
		if err := budgets.DispatchTokens(t.Context(), id, "dispatched"); err != nil {
			t.Fatal(err)
		}
	}
	if err := ledger.Close(); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(filepath.Dir(root), "native-backup.tar.gz")
	if err = Backup(t.Context(), root, archive); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(root, root+"-original-retained"); err != nil {
		t.Fatal(err)
	}
	if err = Restore(t.Context(), archive, root); err != nil {
		t.Fatal(err)
	}
	cancel, done = start()
	if code := request("GET", "/api/v1/graphs/software-factory", "", &factory); code != 200 || factory.Graph.Metadata.Name != "Personal factory" || factory.Graph.Metadata.Version != 2 {
		t.Fatal("factory edit lost on restart", code)
	}

	defer func() { stop(cancel, done) }()
	checkBudgets := func() {
		t.Helper()
		for _, id := range []string{"native-crashed", "foreign-active"} {
			var tree struct {
				Budget delegation.BudgetView `json:"budget"`
			}
			if code := request("GET", "/api/v1/executions/"+id+"/tree", "", &tree); code != 200 {
				t.Fatal(code)
			}
			if id == "native-crashed" && (tree.Budget.Reserved != 0 || tree.Budget.Charged != 30 || tree.Budget.Estimated != 30) {
				t.Fatal("native reservation recovery", tree.Budget)
			}
			if id == "foreign-active" && tree.Budget.Reserved != 60 {
				t.Fatal("foreign reservation changed", tree.Budget)
			}
		}
	}
	checkBudgets()
	var restored taskconfig.Draft
	if c := request("GET", "/api/v1/tasks/"+draft.ID, "", &restored); c != 200 || restored.Title != draft.Title || restored.Revision != draft.Revision {
		t.Fatal("lost draft on restart", c)
	}
	var recovered []domain.Approval
	if c := request("GET", "/api/v1/approvals?status=pending", "", &recovered); c != 200 || len(recovered) != 1 || recovered[0].ID != held[0].ID {
		t.Fatal("lost approval", c)
	}
	if c := request("POST", "/api/v1/graphs/queued/start", `{}`, nil); c != 200 && c != 202 {
		t.Fatal(c)
	}
	waitExecutions := func(count int) {
		t.Helper()
		deadline := time.Now().Add(6 * time.Second)
		for time.Now().Before(deadline) {
			var xs []domain.Execution
			request("GET", "/api/v1/graphs/queued/executions", "", &xs)
			if len(xs) == count {
				settled := true
				for _, x := range xs {
					settled = settled && x.Status == domain.ExecSucceeded
				}
				if settled {
					return
				}
			}
			time.Sleep(30 * time.Millisecond)
		}
		t.Fatal("restored queue did not settle", count)
	}
	waitExecutions(1)
	if c := request("POST", "/api/v1/approvals/"+held[0].ID+"/approve", `{}`, nil); c != 200 {
		t.Fatal(c)
	}
	waitExecutions(2)
	stop(cancel, done)
	cancel, done = start()
	checkBudgets()
	res, err := http.Get(base + "/api/v1/tasks")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 401 {
		t.Fatal("unauthenticated API", res.StatusCode)
	}
}
