package taskconfig

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
)

func TestBindWorkflowDraftUsesSavedInputAndRefusesUnreviewedProject(t *testing.T) {
	t.Parallel()
	g := &domain.Graph{
		Metadata: domain.GraphMetadata{ID: "editorial", Version: 3},
		Spec: domain.GraphSpec{
			Nodes: []domain.Node{{ID: "reader"}, {ID: "checker"}},
			Channels: []domain.Channel{
				{ID: "requests", Type: domain.ChannelQueue, To: []string{"reader"}},
				{ID: "checks", Type: domain.ChannelQueue, To: []string{"checker"}},
			},
		},
	}
	request := Input{Title: "Notes", Request: "Read the notes", Outcome: "understand"}
	if _, err := BindWorkflowDraft(request, g); !errors.Is(err, ErrInvalid) {
		t.Fatalf("ambiguous bindings accepted: %v", err)
	}
	g.Spec.Task = &domain.TaskDefaults{NodeID: "reader", ResultNodeID: "checker", ChannelID: "requests", MessageType: "editorial.read"}
	input, err := BindWorkflowDraft(request, g)
	if err != nil || input.NodeID != "reader" || input.ResultNodeID != "checker" || input.ChannelID != "requests" || input.MessageType != "editorial.read" ||
		input.GraphVersion != 3 || input.GraphID != "editorial" {
		t.Fatalf("defaults not selected: %+v %v", input, err)
	}
	var payload map[string]string
	if err := json.Unmarshal([]byte(input.PayloadJSON), &payload); err != nil ||
		payload["title"] != request.Title || payload["description"] != request.Request {
		t.Fatalf("payload not derived from the reviewed request: %+v %v", payload, err)
	}
	g.Spec.Task.ProjectIDs = []string{"unreviewed"}
	if _, err := BindWorkflowDraft(request, g); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unreviewed repository accepted: %v", err)
	}
}
