package chat

import (
	"context"

	"github.com/cloudwego/eino/compose"
	"github.com/woliveiras/weft/server/internal/llm"
)

type TurnRunner func(context.Context, []llm.ChatMessage, []ReadTool, ModelCall) (Interpretation, error)

// NewEinoTurn compiles the orchestration once. Actor-bound tools, messages and
// model callbacks enter through the invocation and never live in the graph.
func NewEinoTurn(ctx context.Context) (TurnRunner, error) {
	const interpret, read = "interpret", "read"
	graph := compose.NewGraph[*chatTurn, *chatTurn]()
	if err := graph.AddLambdaNode(interpret, compose.InvokableLambda(interpretTurn)); err != nil {
		return nil, err
	}
	if err := graph.AddLambdaNode(read, compose.InvokableLambda(readBatch)); err != nil {
		return nil, err
	}
	if err := graph.AddEdge(compose.START, interpret); err != nil {
		return nil, err
	}
	next := func(_ context.Context, turn *chatTurn) (string, error) {
		if len(turn.pending) > 0 {
			return read, nil
		}
		return compose.END, nil
	}
	for _, from := range []string{interpret, read} {
		if err := graph.AddBranch(from, compose.NewGraphBranch(next, map[string]bool{read: true, compose.END: true})); err != nil {
			return nil, err
		}
	}
	runner, err := graph.Compile(ctx, compose.WithMaxRunSteps(maxReads+2))
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, history []llm.ChatMessage, catalog []ReadTool, call ModelCall) (Interpretation, error) {
		out, err := runner.Invoke(ctx, &chatTurn{history: history, catalog: catalog, call: call})
		if err != nil {
			return Interpretation{}, err
		}
		return out.result, nil
	}, nil
}
