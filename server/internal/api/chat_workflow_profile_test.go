package api_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/llm"
)

func TestChatUpdatesOnlyTheWorkflowProfile(t *testing.T) {
	srv, app := setupApp(t, nil)
	doc := `{"metadata":{"id":"profile-flow","name":"Profile workflow"},"spec":{"task":{"nodeId":"writer"},"nodes":[{"id":"writer","runtime":{"kind":"echo"},"tools":[{"name":"filesystem.read","paths":["notes"]}]}]},"x-test":"preserved"}`
	if got := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/json", doc, nil); got != http.StatusCreated {
		t.Fatalf("create: %d", got)
	}
	const patch = "apiVersion: weft.io/v1\nkind: Graph\nmetadata:\n  id: profile-flow\nspec:\n  task:\n    authorizationProfile: assisted"
	if err := app.Models.ReplaceProviders([]llm.Entry{{Config: llm.ProviderConfig{Name: "chat-fixture", Type: "openai"}, Provider: installationChatFixture{document: patch}}}, "chat-fixture"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Store.PutSetting(t.Context(), "chat/model", []byte(`{"provider":"chat-fixture","name":"fixture"}`), 0, "fixture"); err != nil {
		t.Fatal(err)
	}
	const base = "/api/v1/chat/sessions"
	var s chat.Session
	if got := call(t, srv, "POST", base, adminTok, "application/json", `{"requestId":"00000000000000000000000000000001","text":"Use Assisted by default for the profile-flow workflow."}`, &s); got != 200 || len(s.Messages) != 2 || s.Messages[1].Review == nil {
		t.Fatalf("review: %d %+v", got, s.Messages)
	}
	review := s.Messages[1].Review
	if review.Configuration == nil || strings.Contains(review.Configuration.Document, "filesystem.read") {
		t.Fatal("expected a scoped profile change")
	}
	var settled chat.Session
	if got := call(t, srv, "POST", base+"/"+s.ID+"/operations/"+review.ID+"/confirm", adminTok, "application/json", fmt.Sprintf(`{"digest":%q}`, review.Digest), &settled); got != 200 || settled.Messages[1].Review.State != chat.ReviewApplied {
		t.Fatalf("confirmation: %d %+v", got, settled.Messages)
	}
	g, err := app.Store.GetGraph(t.Context(), "profile-flow", 0)
	if err != nil {
		t.Fatal(err)
	}
	if g.Metadata.Version != 2 || g.Spec.Task.AuthorizationProfile != domain.TaskAssisted || g.Spec.Task.NodeID != "writer" || g.Metadata.Name != "Profile workflow" || g.Extensions["x-test"] != "preserved" || g.Node("writer").Tools[0].Paths[0] != "notes" {
		t.Fatalf("unexpected graph: %+v", g)
	}
}
