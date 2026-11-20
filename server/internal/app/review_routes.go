package app

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/woliveiras/weft/server/internal/agentcatalog"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/engine"
	"github.com/woliveiras/weft/server/internal/graph"
	"github.com/woliveiras/weft/server/internal/reviewroute"
	"github.com/woliveiras/weft/server/internal/scm"
	"github.com/woliveiras/weft/server/internal/tools"
)

func (a *App) newReviewRoutes() *reviewroute.Manager {
	m := &reviewroute.Manager{Store: a.Store, SCM: a.SCM, Validate: a.validateReviewRoute}
	m.Dispatch = func(ctx context.Context, r reviewroute.Route, v reviewroute.Receipt) (string, error) {
		// No issue body or provider instructions become routing authority.
		payload, err := json.Marshal(v.PullRequest)
		if err != nil {
			return "", err
		}
		msg, err := domain.NewMessage(reviewroute.MessageType, "", "", payload)
		if err != nil {
			return "", err
		}
		msg.ID = v.MessageID
		msg.SetMeta(domain.MetaCorrelationID, v.MessageID)
		msg.SetMeta(domain.MetaCausationID, v.EventID)
		return a.Engine.DispatchReview(ctx, engine.ReviewDispatch{GraphID: r.GraphID, Version: r.GraphVersion, Channel: r.ChannelID, Node: r.NodeID, ExpectedRunID: v.ExpectedRunID, Message: msg})
	}
	return m
}
func (a *App) validateReviewRoute(ctx context.Context, r reviewroute.Route) error {
	g, err := a.Store.GetGraph(ctx, r.GraphID, r.GraphVersion)
	if err != nil {
		return err
	}
	issues, err := (agentcatalog.Manager{Store: a.Store}).Resolve(ctx, g)
	if err != nil {
		return err
	}
	if len(issues) > 0 {
		return fmt.Errorf("%w: resolve the selected agent version", reviewroute.ErrInvalid)
	}
	graph.Normalize(g)
	n := g.Node(r.NodeID)
	c := g.Channel(r.ChannelID)
	if n == nil || n.Disabled || n.Loop.Mode != domain.LoopEvent || c == nil || !slices.Equal(g.Consumers(c), []string{r.NodeID}) {
		return fmt.Errorf("%w: choose an exclusive event-agent ingress", reviewroute.ErrInvalid)
	}
	// Require named grants already present in this immutable graph version. An
	// automatic route never adds a tool, widens a repository bound or enables merge.
	for _, name := range []string{scm.CapPullRequestRead, scm.CapReviewSummary} {
		found := false
		for _, grant := range n.Tools {
			if grant.Name == name && tools.CheckRepository(grant, r.Connection) == nil {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("%w: selected agent needs an explicit repository-scoped %s grant", reviewroute.ErrInvalid, name)
		}
	}
	return nil
}
func (a *App) runReviewRoutes(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	lastFailure := ""
	for {
		if a.ReviewRoutes != nil {
			err := a.ReviewRoutes.Tick(ctx)
			if err == nil {
				lastFailure = ""
			} else if ctx.Err() == nil && err.Error() != lastFailure {
				a.Log.Warn("portable review routing requires attention", "error", err)
				lastFailure = err.Error()
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
