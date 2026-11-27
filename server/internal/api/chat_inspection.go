package api

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/activity"
	"github.com/woliveiras/weft/server/internal/agentcatalog"
	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/cost"
	"github.com/woliveiras/weft/server/internal/delegation"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/instructions"
	"github.com/woliveiras/weft/server/internal/projectconfig"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/taskconfig"
)

var errInspectionUnavailable = errors.New("inspection service unavailable")

func (s *Server) inspectChatConsole(ctx context.Context, caller Identity, plan chat.Plan) chat.Interpretation {
	destination := chat.InspectionDestination(plan.Area)
	result := chat.Interpretation{Plan: plan, Destination: destination}
	if destination == "" {
		result.Text = "Choose a supported console resource."
		return result
	}
	view := chat.Inspection{Area: plan.Area}
	now := time.Now()
	if err := s.readChatInspection(ctx, caller, &view, now); err != nil {
		result.Text = "The current records could not be read. Open the console page or retry this request."
		result.Retryable = true
		return result
	}
	if len(view.References) > 0 {
		result.Inventory = &chat.Inventory{Area: view.Area, Items: view.References}
	}
	result.Text = view.Text(now)
	return result
}

// Only these display fields enter a transcript. URLs, connection
// credentials, raw errors, instruction bodies and approval payloads stay
// in their source services.
func (s *Server) readChatInspection(ctx context.Context, caller Identity, v *chat.Inspection, now time.Time) error {
	limit := chat.InspectionLimit
	if v.Search != nil {
		limit = 100
	}
	switch v.Area {
	case "tasks":
		rows, err := (taskconfig.Manager{Store: s.Store}).ListRecentlyUpdated(ctx, "", limit)
		if err != nil {
			return err
		}
		for _, t := range rows.Items {
			line := fmt.Sprintf("%s · %s · %s", t.ID, t.Title, t.Status)
			if v.Search != nil {
				addChatTaskChoice(v.Search, t, line)
			} else {
				v.AddResource(t.ID, line)
			}
		}
		v.More = rows.Next != ""
	case "workflows":
		rows, err := s.Store.ListGraphRecords(ctx)
		if err != nil {
			return err
		}
		for _, g := range rows {
			if !strings.HasPrefix(g.ID, domain.TaskRunGraphPrefix) {
				v.AddResource(g.ID, fmt.Sprintf("%s · version %d · %s", g.ID, g.Version, g.State))
			}
		}
	case "agents":
		rows, err := (agentcatalog.Manager{Store: s.Store}).List(ctx)
		if err != nil {
			return err
		}
		for _, a := range rows {
			v.CatalogResource(a.ID, fmt.Sprintf("%s · %s · version %d", a.ID, a.Name, a.Latest), chat.CatalogCandidate{Name: a.Name, Version: a.Latest})
		}
	case "projects":
		rows, err := (projectconfig.Manager{Store: s.Store}).List(ctx)
		if err != nil {
			return err
		}
		for _, p := range rows {
			v.CatalogResource(p.ID, fmt.Sprintf("%s · %s · revision %d", p.ID, p.Name, p.Revision), chat.CatalogCandidate{Name: p.Name, Revision: p.Revision})
		}
	case "skills":
		rows, err := (instructions.Manager{Store: s.Store}).List(ctx)
		if err != nil {
			return err
		}
		for _, sk := range rows {
			if !sk.Deleted {
				v.CatalogResource(sk.ID, fmt.Sprintf("%s · %s · revision %d", sk.ID, sk.Title, sk.Revision), chat.CatalogCandidate{Name: sk.Title, Revision: sk.Revision})
			}
		}
	case "approvals":
		rows, err := s.Store.ListApprovals(ctx, storage.ApprovalFilter{Status: domain.ApprovalPending, Limit: limit + 1})
		if err != nil {
			return err
		}
		if len(rows) > limit {
			v.More = true
			rows = rows[:limit]
		}
		for _, a := range rows {
			if v.Search != nil {
				if a.ExpiresAt == nil || now.Before(*a.ExpiresAt) {
					v.Search.Add(a.ID, a.Title, fmt.Sprintf("%s · %s · %s · %s", a.Title, a.GraphID, a.Kind, a.ID))
				}
				continue
			}
			v.AddResource(a.ID, fmt.Sprintf("%s · %s · %s · pending", a.ID, a.GraphID, a.Kind))
		}
	case "activity":
		query, err := activity.ParseQuery(nil)
		if err != nil {
			return err
		}
		b := activity.Builder{Store: s.Store}
		if s.Reviews != nil {
			b.Reviews = s.Reviews
		}
		result, err := b.Build(ctx, query, activity.Viewer{Name: caller.Name, CanReview: caller.Role.Allows(config.RoleOperator)})
		if err != nil {
			return err
		}
		v.Add("Window " + result.Window)
		for _, c := range []struct {
			name      string
			count     *int
			truncated bool
		}{
			{"Running", result.Counters.Running, result.Truncated.Running}, {"Waiting", result.Counters.Waiting, result.Truncated.NeedsYou}, {"Failed", result.Counters.Failed, result.Truncated.Executions}, {"Finished", result.Counters.Finished, result.Truncated.Executions},
		} {
			value := "unavailable"
			if c.count != nil {
				value = fmt.Sprint(*c.count)
				if c.truncated {
					value += "+"
				}
			}
			v.Add(c.name + " · " + value)
		}
	case "triggers":
		rows, err := s.triggers().List(ctx, "")
		if err != nil {
			return err
		}
		for _, r := range rows {
			v.Named(r.ID, r.Name, fmt.Sprintf("%s · %s · %s · %s", r.ID, r.Name, r.Workflow, r.State))
		}
	case "review-routes":
		if s.ReviewRoutes == nil {
			return errInspectionUnavailable
		}
		rows, after, err := s.ReviewRoutes.ListPage(ctx, "", limit)
		if err != nil {
			return err
		}
		for _, r := range rows {
			v.Named(r.Name, r.GraphID, fmt.Sprintf("%s · %s version %d · enabled %t", r.Name, r.GraphID, r.GraphVersion, r.Enabled))
		}
		v.More = after != ""
	case "mcp":
		if s.MCP == nil {
			return errInspectionUnavailable
		}
		rows, err := s.MCP.List(ctx)
		if err != nil {
			return err
		}
		for _, c := range rows {
			v.Named(c.ID, c.Service, fmt.Sprintf("%s · %s · %s · %d discovered tools", c.ID, c.Service, c.Status, len(c.Tools)))
		}
	case "scm":
		rows, err := s.SCM.List(ctx)
		if err != nil {
			return err
		}
		for _, c := range rows {
			v.Named(c.Name, c.Repository, fmt.Sprintf("%s · %s · %s", c.Name, c.Kind, c.Repository))
		}
	case "issue-connections":
		rows, err := s.IssueConnections.List(ctx)
		if err != nil {
			return err
		}
		for _, c := range rows {
			v.Named(c.Name, c.Name, fmt.Sprintf("%s · %s · revision %d · enabled %t", c.Name, c.Kind, c.Revision, c.Enabled))
		}
		if s.CloudProfiles != nil {
			profiles, err := s.CloudProfiles.List(ctx)
			if err != nil {
				return err
			}
			for _, c := range profiles {
				v.Named("jira-cloud/"+c.Name, c.Name, fmt.Sprintf("%s · Jira Cloud · revision %d", c.Name, c.Revision))
			}
		}
	case "models":
		if s.Models == nil {
			return errInspectionUnavailable
		}
		for _, m := range s.Models.List() {
			v.Named(m.Name, m.DefaultModel, fmt.Sprintf("%s · %s · %s", m.Name, m.Type, m.DefaultModel))
		}
	case "chat-model":
		m, err := s.readChatModel(ctx)
		if err != nil {
			return err
		}
		v.Add(m.Status)
		if m.Model != nil {
			v.Add(m.Model.Provider + " / " + m.Model.Name)
		}
	case "plugins":
		if s.Providers == nil {
			return errInspectionUnavailable
		}
		for _, p := range s.Providers.Plugins() {
			v.Named(p.ID, p.ID, fmt.Sprintf("%s · enabled %t · %d providers", p.ID, p.Enabled, len(p.Instances)))
		}
	case "tools":
		if s.Tools == nil {
			return errInspectionUnavailable
		}
		for _, t := range s.Tools.List() {
			v.Named(t.Name(), t.Capability(), t.Name()+" · "+t.Capability())
		}
	case "delegation":
		current, err := (delegation.Manager{Store: s.Store}).Policy(ctx)
		if err != nil {
			return err
		}
		p := current.Policy
		v.Add(fmt.Sprintf("Revision %d · enabled %t · approval %s", current.Revision, p.Enabled, p.ApprovalMode))
		v.Add(fmt.Sprintf("Maximum depth %d · %d children per execution · %d concurrent children", p.MaxDepth, p.MaxChildrenPerExecution, p.MaxConcurrentChildren))
		v.Add(fmt.Sprintf("Tree budget · %d tokens", p.MaxTreeTokens))
	case "network":
		current, err := s.networkSettings().Get(ctx)
		if err != nil {
			return err
		}
		v.Add(fmt.Sprintf("Network policy revision %d", current.Revision))
		v.Add(fmt.Sprintf("Setup network · %s", strings.Join(current.Network.SetupNetworks, ", ")))
		v.Add(fmt.Sprintf("Run network · %s", strings.Join(current.Network.RunNetworks, ", ")))
	case "retention":
		if s.Environments == nil {
			return errInspectionUnavailable
		}
		p, err := s.Environments.Retention(ctx)
		if err != nil {
			return err
		}
		v.Add(fmt.Sprintf("Blocked-access history · %d days · up to %d events", p.Policy.Days, p.Policy.MaxEvents))
		v.Add("Chat history · retained indefinitely, including archived conversations")
	case "usage":
		r, err := cost.Spend(ctx, s.Store, cost.Period(now), "")
		if err != nil {
			return err
		}
		v.Add(fmt.Sprintf("%s · %s · %d billed micros · %d estimated micros · %d unpriced calls", r.Period, r.Currency, r.Total.CostMicros, r.Total.EstimatedCost, r.Total.UnpricedCalls))
		if r.Truncated {
			v.Add("The retained cost report was truncated.")
		}
	case "prices":
		c, _, err := s.prices().Get(ctx)
		if err != nil {
			return err
		}
		v.Add(fmt.Sprintf("%s · catalogue version %d · unpriced policy %s", c.Currency, c.Version, c.Unpriced))
		for _, p := range c.Prices {
			v.Add(fmt.Sprintf("%s · %s · %d input / %d output micros per million tokens", p.Provider, p.Model, p.InputMicrosPerMTok, p.OutputMicrosPerMTok))
		}
	case "budgets":
		policy, _, err := s.budgets().Policy(ctx)
		if err != nil {
			return err
		}
		defs, _, err := s.budgets().Definitions(ctx)
		if err != nil {
			return err
		}
		v.Add(fmt.Sprintf("Task run ceiling · %d micros · warning at %d%%", policy.TaskRunBudgetMicros, policy.WarningPercent))
		for _, d := range defs {
			v.Named(d.Scope+"/"+d.ID, d.ID, fmt.Sprintf("%s / %s · %d micros", d.Scope, d.ID, d.LimitMicros))
		}
	default:
		return errInspectionUnavailable
	}
	return nil
}
