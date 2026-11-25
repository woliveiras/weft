package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/taskconfig"
	"github.com/woliveiras/weft/server/internal/taskrun"
)

func TestTaskMessageGateFailureSettlesWithoutExecution(t *testing.T) {
	for _, decision := range []domain.ApprovalStatus{domain.ApprovalRejected, domain.ApprovalExpired} {
		t.Run(string(decision), func(t *testing.T) {
			srv, a := setupApp(t, nil)
			doc := `{"metadata":{"id":"task-held"},"spec":{"nodes":[{"id":"worker","runtime":{"kind":"echo"},"loop":{"mode":"event"}}],"channels":[{"id":"requests","to":["worker"],"approval":true}]}}`
			if c := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/json", doc, nil); c != 201 {
				t.Fatal(c)
			}
			input := taskconfig.Input{Title: "Held task", Request: "Explain", Outcome: "freeform", GraphID: "task-held", GraphVersion: 1, NodeID: "worker", ResultNodeID: "worker", ChannelID: "requests", MessageType: "feature.request", PayloadJSON: `{}`}
			body, _ := json.Marshal(input)
			var source taskconfig.Draft
			if c := call(t, srv, "POST", "/api/v1/tasks", adminTok, "application/json", string(body), &source); c != 201 {
				t.Fatal(c)
			}
			if c := taskStart(t, srv.URL, source.ID, adminTok, source.Revision); c != 202 {
				t.Fatal(c)
			}
			graphID := taskrun.ExecutionID(source.ID)
			approvals, err := a.Store.ListApprovals(t.Context(), storage.ApprovalFilter{GraphID: graphID, Status: domain.ApprovalPending})
			if err != nil || len(approvals) != 1 {
				t.Fatal(approvals, err)
			}
			approval := approvals[0]
			other, err := a.Approvals.Create(t.Context(), &domain.Approval{GraphID: graphID, NodeID: "worker", Kind: domain.ApprovalTool, Title: "Other pending decision"})
			if err != nil {
				t.Fatal(err)
			}
			// Reconciling a pending gate must retain the task and its running copy.
			if err = a.API.TaskRunner().Settle(t.Context(), graphID); err != nil {
				t.Fatal(err)
			}
			var pending taskconfig.Draft
			if c := call(t, srv, "GET", "/api/v1/tasks/"+source.ID, viewerTok, "", "", &pending); c != 200 || pending.Status != "running" {
				t.Fatal(c, pending)
			}
			if decision == domain.ApprovalRejected {
				if c := call(t, srv, "POST", "/api/v1/approvals/"+approval.ID+"/reject", adminTok, "application/json", `{}`, nil); c != 200 {
					t.Fatal(c)
				}
			} else {
				// Exercise the real timeout sweep with an already-overdue persisted gate.
				deadline := time.Now().Add(-time.Second)
				approval.ExpiresAt = &deadline
				if err = a.Store.PutApproval(t.Context(), approval); err != nil {
					t.Fatal(err)
				}
				a.Approvals.Sweep(t.Context())
			}
			// A failed gate cannot terminate another pending decision in this task.
			if err = a.API.TaskRunner().Settle(t.Context(), graphID); err != nil {
				t.Fatal(err)
			}
			var active taskrun.View
			if c := call(t, srv, "GET", "/api/v1/tasks/"+source.ID+"/progress", viewerTok, "", "", &active); c != 200 || active.State != "running" || len(active.PendingApprovals) != 1 {
				t.Fatal(c, active)
			}
			activeRecord, err := a.Store.GetGraphRecord(t.Context(), graphID)
			if err != nil || activeRecord.DesiredState != domain.GraphRunning {
				t.Fatal(activeRecord, err)
			}
			if c := call(t, srv, "POST", "/api/v1/approvals/"+other.ID+"/reject", adminTok, "application/json", `{}`, nil); c != 200 {
				t.Fatal(c)
			}
			waitFor(t, 5*time.Second, "persisted failed task and stopped copy", func() bool {
				if err := a.API.TaskRunner().Settle(t.Context(), graphID); err != nil {
					t.Fatal(err)
				}
				if c := call(t, srv, "GET", "/api/v1/tasks/"+source.ID, viewerTok, "", "", &source); c != 200 {
					t.Fatal(c)
				}
				rec, err := a.Store.GetGraphRecord(t.Context(), graphID)
				return err == nil && source.Status == "failed" && rec.State == domain.GraphStopped && rec.DesiredState == domain.GraphStopped
			})
			var view taskrun.View
			if c := call(t, srv, "GET", "/api/v1/tasks/"+source.ID+"/progress", viewerTok, "", "", &view); c != 200 || view.State != "failed" || len(view.Executions) != 0 || len(view.PendingApprovals) != 0 || len(view.Decisions) != 2 {
				t.Fatal(c, view)
			}
			status, _, child := retryTaskRequest(t, srv.URL, source.ID, operatorTok, fmt.Sprint(source.Revision), `{"confirm":true}`)
			if status != http.StatusCreated || child.Status != "draft" || child.RetryOfTaskID != source.ID {
				t.Fatal(status, child)
			}
			xs, err := a.Store.ListExecutions(t.Context(), storage.ExecutionFilter{GraphID: graphID})
			if err != nil || len(xs) != 0 {
				t.Fatal("gate failure dispatched execution", xs, err)
			}
			stored, err := a.Store.GetApproval(t.Context(), approval.ID)
			if err != nil || stored.Status != decision {
				t.Fatal("retry changed original decision", stored, err)
			}
		})
	}
}
