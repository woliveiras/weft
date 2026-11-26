package native

import (
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/runtime"
	"testing"
)

func TestPullRequestAnalysisCannotDispatchTools(t *testing.T) {
	in := &runtime.TurnInput{AnalysisOnly: true, Prompt: "Check the change.", Inputs: []*domain.Message{{Payload: []byte(`{"Diff":"+example"}`)}}, Tools: granted}
	for _, reply := range []*llm.ChatResponse{text("One finding."), toolCall("emit", `{"type":"publish"}`), toolCall("state_set", `{"key":"a","value":"b"}`), text("")} {
		env := &scriptedEnv{replies: []*llm.ChatResponse{reply}}
		res, err := turn(t, env, in)
		if reply.Message.Content == "One finding." {
			if err != nil || res.Summary != reply.Message.Content {
				t.Fatalf("draft: %+v %v", res, err)
			}
		} else if err == nil {
			t.Fatal("expected an incomplete review")
		}
		if len(env.asked) != 1 || len(env.asked[0].Tools) != 0 || len(env.tools) != 0 || len(env.emits) != 0 || len(env.outputs) != 0 {
			t.Fatal("read-only review invoked an effect")
		}
	}
}
