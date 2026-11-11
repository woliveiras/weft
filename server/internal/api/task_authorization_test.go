package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/taskauth"
	"github.com/woliveiras/weft/server/internal/taskconfig"
	"github.com/woliveiras/weft/server/internal/taskrun"
)

var taskAPIRoles = []struct {
	name, token string
	read        int
	adminWrite  int
	decide      int
}{
	{"anonymous", "", http.StatusUnauthorized, http.StatusUnauthorized, http.StatusUnauthorized},
	{"viewer", viewerTok, http.StatusOK, http.StatusForbidden, http.StatusForbidden},
	{"operator", operatorTok, http.StatusOK, http.StatusForbidden, http.StatusOK},
	{"admin", adminTok, http.StatusOK, http.StatusOK, http.StatusOK},
}

func TestTaskAuthorizationAPI(t *testing.T) {
	srv := setup(t)
	if c := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/json", taskGraph, nil); c != 201 {
		t.Fatal(c)
	}
	var d taskconfig.Draft
	if c := call(t, srv, "POST", "/api/v1/tasks", operatorTok, "application/json", taskBody, &d); c != 201 {
		t.Fatal(c)
	}
	path := "/api/v1/tasks/" + d.ID + "/authorization"
	read := func(t *testing.T, token string, want int) taskauth.Review {
		t.Helper()
		var v taskauth.Review
		var out any
		if want == http.StatusOK {
			out = &v
		}
		if c := call(t, srv, "GET", path, token, "", "", out); c != want {
			t.Fatalf("authorization read: got %d, want %d", c, want)
		}
		return v
	}
	review := read(t, viewerTok, http.StatusOK)
	put := func(t *testing.T, token, body, revision string) int {
		t.Helper()
		req, _ := http.NewRequest("PUT", srv.URL+path, strings.NewReader(body))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("If-Match", revision)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		return res.StatusCode
	}
	b, _ := json.Marshal(map[string]any{"mode": "full", "scope": review.Scope, "confirm": true})
	if c := put(t, adminTok, strings.Replace(string(b), "true", "false", 1), "0"); c != 400 {
		t.Fatal(c)
	}
	for _, role := range taskAPIRoles {
		t.Run(role.name, func(t *testing.T) {
			v := read(t, role.token, role.read)
			if role.read == http.StatusOK && (v.Scope == "" || v.Scope != review.Scope || len(v.Capabilities) != 1 || v.Record != nil) {
				t.Fatal("unexpected review before consent", v)
			}
			if c := put(t, role.token, string(b), "0"); c != role.adminWrite {
				t.Fatalf("authorization write: got %d, want %d", c, role.adminWrite)
			}
			if role.adminWrite != http.StatusOK && read(t, viewerTok, http.StatusOK).Record != nil {
				t.Fatal("denied request stored consent")
			}
		})
	}
	if c := put(t, adminTok, string(b), "0"); c < 400 {
		t.Fatal("stale write", c)
	}
	review = read(t, viewerTok, http.StatusOK)
	if review.Record == nil || review.Record.Mode != taskauth.Full || review.Record.Actor != "admin" || review.Record.Revision != 1 {
		t.Fatal(fmt.Sprint(review))
	}
	if c := put(t, adminTok, `{"mode":"invalid","confirm":true}`, "1"); c != 400 {
		t.Fatal(c)
	}

	for _, role := range taskAPIRoles {
		t.Run("cancel/"+role.name, func(t *testing.T) {
			want, state := role.adminWrite, "draft"
			if want == http.StatusOK {
				want, state = http.StatusAccepted, "cancelled"
			}
			if c := call(t, srv, "POST", "/api/v1/tasks/"+d.ID+"/cancel", role.token, "application/json", `{"confirm":true}`, nil); c != want {
				t.Fatalf("task cancellation: got %d, want %d", c, want)
			}
			var saved taskconfig.Draft
			if c := call(t, srv, "GET", "/api/v1/tasks/"+d.ID, viewerTok, "", "", &saved); c != 200 || saved.Status != state {
				t.Fatalf("persisted task: HTTP %d, state %q, want %q", c, saved.Status, state)
			}
		})
	}
}

func TestTaskApprovalAPIRoles(t *testing.T) {
	srv := setup(t)
	const graph = `{"metadata":{"id":"task-approval-roles"},"spec":{"nodes":[{"id":"worker","runtime":{"kind":"echo"},"loop":{"mode":"event"}}],"channels":[{"id":"requests","to":["worker"],"approval":true}]}}`
	if c := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/json", graph, nil); c != 201 {
		t.Fatal(c)
	}
	input := taskconfig.Input{
		Title: "Review task input", Request: "Explain", Outcome: "freeform",
		GraphID: "task-approval-roles", GraphVersion: 1, NodeID: "worker", ResultNodeID: "worker",
		ChannelID: "requests", MessageType: "feature.request", PayloadJSON: "{}",
	}
	body, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}

	for _, decision := range []struct {
		name string
		want domain.ApprovalStatus
	}{
		{"approve", domain.ApprovalApproved},
		{"reject", domain.ApprovalRejected},
	} {
		for _, role := range taskAPIRoles {
			t.Run(decision.name+"/"+role.name, func(t *testing.T) {
				var d taskconfig.Draft
				if c := call(t, srv, "POST", "/api/v1/tasks", operatorTok, "application/json", string(body), &d); c != 201 {
					t.Fatal(c)
				}
				if c := taskStart(t, srv.URL, d.ID, adminTok, d.Revision); c != 202 {
					t.Fatal(c)
				}
				inbox := "/api/v1/approvals?graph=" + taskrun.ExecutionID(d.ID)
				progress := "/api/v1/tasks/" + d.ID + "/progress"
				var approvals []domain.Approval
				if c := call(t, srv, "GET", inbox, viewerTok, "", "", &approvals); c != 200 || len(approvals) != 1 || approvals[0].Status != domain.ApprovalPending {
					t.Fatalf("pending approval: HTTP %d, %+v", c, approvals)
				}
				id := approvals[0].ID
				var view taskrun.View
				var inboxOut, progressOut any
				if role.read == http.StatusOK {
					inboxOut, progressOut = &approvals, &view
				}
				if c := call(t, srv, "GET", inbox, role.token, "", "", inboxOut); c != role.read {
					t.Fatalf("inbox read: got %d, want %d", c, role.read)
				}
				if c := call(t, srv, "GET", progress, role.token, "", "", progressOut); c != role.read {
					t.Fatalf("task progress read: got %d, want %d", c, role.read)
				}
				if role.read == http.StatusOK && (len(approvals) != 1 || approvals[0].ID != id || len(view.PendingApprovals) != 1 || view.PendingApprovals[0].ID != id) {
					t.Fatal("inbox and task did not show the same pending approval")
				}
				if c := call(t, srv, "POST", "/api/v1/approvals/"+id+"/"+decision.name, role.token, "application/json", "{}", nil); c != role.decide {
					t.Fatalf("approval decision: got %d, want %d", c, role.decide)
				}
				want, actor := domain.ApprovalPending, ""
				if role.decide == http.StatusOK {
					want, actor = decision.want, role.name
				}
				if c := call(t, srv, "GET", inbox, viewerTok, "", "", &approvals); c != 200 || len(approvals) != 1 || approvals[0].Status != want || approvals[0].DecidedBy != actor {
					t.Fatalf("persisted approval: HTTP %d, %+v", c, approvals)
				}
				view = taskrun.View{}
				if c := call(t, srv, "GET", progress, viewerTok, "", "", &view); c != 200 || len(view.Decisions) != 1 ||
					view.Decisions[0].ID != id || view.Decisions[0].Status != want {
					t.Fatalf("task decision disagrees with the inbox: HTTP %d, %+v", c, view.Decisions)
				}
			})
		}
	}
}

func TestWorkspaceConsentRejectsCheckRunUnderNativeIsolation(t *testing.T) {
	for _, isolation := range []string{"process", "kubernetes"} {
		t.Run(isolation, func(t *testing.T) {
			srv := setup(t)
			workflow := strings.Replace(taskGraph, `"tools":[]`, fmt.Sprintf(`"isolation":{"mode":%q},"tools":[{"name":"check.run"}]`, isolation), 1)
			if c := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/json", workflow, nil); c != 201 {
				t.Fatalf("save workflow: %d", c)
			}
			var draft taskconfig.Draft
			if c := call(t, srv, "POST", "/api/v1/tasks", operatorTok, "application/json", taskBody, &draft); c != 201 {
				t.Fatalf("save draft: %d", c)
			}
			path := "/api/v1/tasks/" + draft.ID + "/authorization"
			var review taskauth.Review
			if c := call(t, srv, "GET", path, adminTok, "", "", &review); c != 200 {
				t.Fatalf("review consent: %d", c)
			}
			body := fmt.Sprintf(`{"mode":"workspace","scope":%q,"confirm":true}`, review.Scope)
			req, err := http.NewRequestWithContext(t.Context(), "PUT", srv.URL+path, strings.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Authorization", "Bearer "+adminTok)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("If-Match", "0")
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			var problem apiError
			if err := json.NewDecoder(res.Body).Decode(&problem); err != nil {
				t.Fatal(err)
			}
			if res.StatusCode != 400 || !strings.Contains(problem.Error, "process shell cannot be confined to a worktree; use Ask for approval, Full access or Docker isolation") {
				t.Fatalf("save consent: %d %+v", res.StatusCode, problem)
			}
			review = taskauth.Review{}
			if c := call(t, srv, "GET", path, adminTok, "", "", &review); c != 200 || review.Record != nil {
				t.Fatalf("rejected consent was persisted: %d %+v", c, review.Record)
			}
		})
	}
}
