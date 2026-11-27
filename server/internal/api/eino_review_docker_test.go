//go:build einodocker

package api_test

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/app"
	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/review"
	"github.com/woliveiras/weft/server/internal/storage"
)

// The controller and the dedicated sandbox daemon must see the same absolute
// workspace path. The daemon needs a local alpine:3.22 image.
func TestEinoDockerReviewAfterRestart(t *testing.T) {
	shared, bin := os.Getenv("WEFT_TEST_REVIEW_ROOT"), os.Getenv("WEFT_TEST_ENV_DOCKER_BIN")
	if !filepath.IsAbs(shared) || bin == "" {
		t.Fatal("set WEFT_TEST_REVIEW_ROOT and WEFT_TEST_ENV_DOCKER_BIN for the dedicated daemon")
	}
	binary := filepath.Join(t.TempDir(), "review-agent")
	build := exec.CommandContext(t.Context(), "go", "build", "-o", binary, "./testdata/review-agent")
	build.Dir = "../../../agents/eino"
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	for _, decision := range []string{"approved", "rejected"} {
		t.Run(decision, func(t *testing.T) {
			dir, err := os.MkdirTemp(shared, "candidate-")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := os.RemoveAll(dir); err != nil {
					t.Error(err)
				}
			})
			open := func(t *testing.T) (*httptest.Server, *app.App) {
				cb := httptest.NewUnstartedServer(nil)
				srv, a := setupApp(t, func(c *config.Config) {
					c.Server.InternalURL = "http://" + cb.Listener.Addr().String()
					c.Storage.Dir = filepath.Join(dir, "db")
					c.Workspaces.Root = filepath.Join(dir, "ws")
					c.Queue = config.Queue{Type: "fs", Dir: filepath.Join(dir, "broker")}
					c.Isolation.Docker.Bin = bin
					c.Isolation.Docker.Image = "alpine:3.22"
				})
				cb.Config.Handler = a.API.Handler()
				cb.Start()
				t.Cleanup(cb.Close)
				done := make(chan struct{})
				go func() { defer close(done); a.Reviews.Run(t.Context()) }()
				t.Cleanup(func() { <-done })
				return srv, a
			}
			const path = "/api/v1/graphs/candidate"
			var pending *review.Record
			list := func(t *testing.T, a *app.App, ch string, status domain.MessageStatus) []*domain.MessageRecord {
				rows, err := a.Store.ListMessages(t.Context(), storage.MessageFilter{GraphID: "candidate", Channel: ch, Status: status})
				if err != nil {
					t.Fatal(err)
				}
				return rows
			}
			if !t.Run("suspend", func(t *testing.T) {
				srv, a := open(t)
				graph := fmt.Sprintf(`{"metadata":{"id":"candidate"},"spec":{"nodes":[
 {"id":"worker","runtime":{"kind":"external","framework":"eino","agent":"candidate","command":[%q]},"isolation":{"mode":"docker","image":"alpine:3.22","network":"none"},"tools":[{"name":"review.request"},{"name":"shell.exec","commands":["cat"]},{"name":"filesystem.write","paths":["after-gate.txt"]}]},
 {"id":"sink","runtime":{"kind":"echo"},"isolation":{"mode":"process"}}],
 "channels":[{"id":"input","to":["worker"]},{"id":"output","from":"worker","to":["sink"]}]}}`, binary)
				if code := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/json", graph, nil); code != 201 {
					t.Fatal("save", code)
				}
				work := filepath.Join(dir, "ws", "candidate")
				if err := os.MkdirAll(work, 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(work, "candidate.txt"), []byte(decision+" fixture"), 0644); err != nil {
					t.Fatal(err)
				}
				if code := call(t, srv, "POST", path+"/start", operatorTok, "", "", nil); code != 200 {
					t.Fatal("start", code)
				}
				waitFor(t, 20*time.Second, "replicas ready", func() bool {
					rs, err := a.Store.ListReplicaStatus(t.Context(), "candidate")
					return err == nil && len(rs) == 2 && rs[0].State == domain.ReplicaIdle && rs[1].State == domain.ReplicaIdle
				})
				if code := call(t, srv, "POST", path+"/channels/input/messages", operatorTok, "application/json", `{"type":"job","payload":{}}`, nil); code != 201 {
					t.Fatal("inject", code)
				}
				waitFor(t, 15*time.Second, "suspended with input acknowledged", func() bool {
					rs, err := a.Reviews.List(t.Context())
					if err != nil {
						t.Fatal(err)
					}
					if len(rs) == 1 {
						pending = rs[0]
						ms := list(t, a, "input", domain.MsgAcked)
						return len(ms) == 1
					}
					xs, err := a.Store.ListExecutions(t.Context(), storage.ExecutionFilter{GraphID: "candidate", Status: domain.ExecFailed})
					if err != nil {
						t.Fatal(err)
					}
					if len(xs) > 0 {
						t.Fatal(xs[0].Error)
					}
					return false
				})
				if pending.State != "pending" || pending.Candidate == "" || len(list(t, a, "output", "")) != 0 {
					t.Fatal("invalid suspended review", pending.State)
				}
				if _, err := os.Stat(filepath.Join(pending.Source, "after-gate.txt")); !os.IsNotExist(err) {
					t.Fatal("post-review tool ran", err)
				}
			}) {
				return
			}
			srv, a := open(t)
			current, err := a.Reviews.Get(t.Context(), pending.ID)
			if err != nil || current.State != "pending" || current.Candidate != pending.Candidate || current.ExecutionID != pending.ExecutionID {
				t.Fatal("review changed across restart", err)
			}
			candidate, err := os.ReadFile(filepath.Join(current.Directory, "candidate", "candidate.txt"))
			if err != nil || string(candidate) != decision+" fixture" {
				t.Fatal("candidate contents changed", err)
			}
			body := fmt.Sprintf(`{"state":%q,"method":"without_execution","revision":%d}`, decision, current.Generation)
			if code := call(t, srv, "POST", "/api/v1/reviews/"+current.ID+"/decide", operatorTok, "application/json", body, nil); code != 200 {
				t.Fatal("decision", code)
			}
			waitFor(t, 12*time.Second, "decision delivered", func() bool {
				r, err := a.Reviews.Get(t.Context(), current.ID)
				return err == nil && r.Notified && r.State == decision
			})
			if decision == "rejected" {
				xs, err := a.Store.ListExecutions(t.Context(), storage.ExecutionFilter{GraphID: "candidate", NodeID: "worker"})
				if err != nil || len(xs) != 1 || xs[0].ID != pending.ExecutionID {
					t.Fatal("unexpected execution after rejection", err)
				}
				if len(list(t, a, "output", "")) != 0 {
					t.Fatal("rejected candidate emitted")
				}
				return
			}
			waitFor(t, 15*time.Second, "reviewed output consumed", func() bool {
				ms := list(t, a, "output", domain.MsgAcked)
				if len(ms) != 1 {
					return false
				}
				var p map[string]string
				if err := json.Unmarshal(ms[0].Message.Payload, &p); err != nil {
					t.Fatal(err)
				}
				return ms[0].Message.Type == "reviewed" && p["state"] == decision && p["reviewId"] == current.ID && p["candidate"] == pending.Candidate && p["execution"] != "" && p["execution"] != pending.ExecutionID && p["execution"] == ms[0].Message.MetaString(domain.MetaExecutionID)
			})
		})
	}
}
