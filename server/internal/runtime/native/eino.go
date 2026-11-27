package native

import (
	"context"
	"math"
	"sync"

	"github.com/cloudwego/eino/compose"
	"github.com/woliveiras/weft/server/internal/runtime"
)

var compiledTurn = sync.OnceValues(func() (compose.Runnable[*nativeTurn, *nativeTurn], error) {
	const model, tools = "model", "tools"
	graph := compose.NewGraph[*nativeTurn, *nativeTurn]()
	if err := graph.AddLambdaNode(model, compose.InvokableLambda(modelRound)); err != nil {
		return nil, err
	}
	if err := graph.AddLambdaNode(tools, compose.InvokableLambda(runTools)); err != nil {
		return nil, err
	}
	if err := graph.AddEdge(compose.START, model); err != nil {
		return nil, err
	}
	next := func(_ context.Context, t *nativeTurn) (string, error) {
		switch {
		case t.finished:
			return compose.END, nil
		case len(t.pending) > 0:
			return tools, nil
		default:
			return model, nil
		}
	}
	for _, node := range []string{model, tools} {
		if err := graph.AddBranch(node, compose.NewGraphBranch(next, map[string]bool{compose.END: true, model: true, tools: true})); err != nil {
			return nil, err
		}
	}
	return graph.Compile(context.Background())
})

func invokeTurn(ctx context.Context, graph compose.Runnable[*nativeTurn, *nativeTurn], t *nativeTurn) (*runtime.TurnResult, error) {
	// Each round can invoke both nodes. Leave room for the model node to
	// report Weft's round-limit error and the number of actual tool calls.
	maxSteps := math.MaxInt
	if t.maxRounds < math.MaxInt/2 {
		maxSteps = 2*t.maxRounds + 2
	}
	out, err := graph.Invoke(ctx, t, compose.WithRuntimeMaxSteps(maxSteps))
	if err != nil {
		return nil, err
	}
	if !out.yielded {
		applySentinels(ctx, out.env, &out.result, out.lastText)
	}
	if out.result.Summary == "" {
		out.result.Summary = truncate(out.lastText, 1000)
	}
	return &out.result, nil
}
