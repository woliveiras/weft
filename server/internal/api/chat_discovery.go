package api

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/cost"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/taskconfig"
)

// A slot points to the selector in this request. All remaining parameters are
// preserved when a choice resumes its native operation.
type discoverySlot struct {
	area     string
	value    *string
	workflow *string // constrains a node, or receives its workflow on selection
}

func discoverySlots(p *chat.Plan) []discoverySlot {
	var slots []discoverySlot
	add := func(area string, v *string) {
		if *v != "" {
			slots = append(slots, discoverySlot{area: area, value: v})
		}
	}
	node := func(w, n *string) {
		add("workflows", w)
		if *n != "" {
			slots = append(slots, discoverySlot{"nodes", n, w})
		}
	}
	switch p.Kind {
	case chat.FindResource:
		slots = append(slots, discoverySlot{area: p.Area, value: &p.Query})
	case chat.ControlWorkflow:
		node(&p.Control.Workflow, &p.Control.Node)
	case chat.ScaleWorkflowNode:
		node(&p.Scale.Workflow, &p.Scale.Node)
	case chat.GuideNode:
		node(&p.Guidance.Graph, &p.Guidance.Node)
	case chat.EditInstructions:
		node(&p.Instructions.Graph, &p.Instructions.Node)
	case chat.AdoptAgentVersion:
		node(&p.Adoption.Workflow, &p.Adoption.Node)
	case chat.CreateWorkflow:
		add("agents", &p.Workflow.Agent)
	case chat.PublishAgent:
		add("agents", &p.Publication.Agent)
	case chat.PrepareWorkflowEnvironment:
		add("workflows", &p.EnvironmentWorkflow)
	case chat.CreateTask:
		add("workflows", &p.TaskDraft.Workflow)
		for i := range p.TaskDraft.Repositories {
			add("projects", &p.TaskDraft.Repositories[i].Project)
		}
	case chat.ControlTrigger:
		add("triggers", &p.Trigger.Target)
	case chat.SaveSkill:
		if p.Skill.Action == "revise" {
			add("skills", &p.Skill.ID)
		}
	case chat.CancelExecution, chat.InspectExecution:
		add("executions", &p.ExecutionID)
	case chat.DecideApproval:
		add("approvals", &p.Approval.Approval)
		add("tasks", &p.Task)
	case chat.RaiseBudget:
		switch p.Budget.Scope {
		case cost.TaskRun:
			add("tasks", &p.Budget.Target)
		case cost.Project:
			add("projects", &p.Budget.Target)
		case cost.Workflow:
			add("workflows", &p.Budget.Target)
		}
	case chat.StartTask, chat.CorrectTask, chat.CancelTask, chat.InspectTask, chat.WatchTask, chat.PrepareTaskEnvironment, chat.AuthorizeTask, chat.RetryTask:
		add("tasks", &p.Task)
	}
	return slots
}

func (b discoverySlot) bind(id string) {
	if b.workflow != nil {
		graph, node, ok := strings.Cut(id, "/")
		if ok {
			*b.workflow = graph
			*b.value = node
		}
	} else {
		*b.value = id
	}
}

func (s *Server) searchChatResources(ctx context.Context, actor Identity, b discoverySlot, query string) (chat.ResourceSearch, error) {
	result := chat.ResourceSearch{Query: query}
	switch b.area {
	case "workflows", "nodes":
		graphs, err := s.Store.ListGraphs(ctx)
		if err != nil {
			return result, err
		}
		for _, g := range graphs {
			if strings.HasPrefix(g.Metadata.ID, domain.TaskRunGraphPrefix) || g.Metadata.Labels[domain.LabelTask] != "" {
				continue
			}
			if b.area == "workflows" {
				result.Add(g.Metadata.ID, g.Metadata.Name, fmt.Sprintf("%s · %s · version %d", g.Metadata.Name, g.Metadata.ID, g.Metadata.Version))
				continue
			}
			if b.workflow != nil && *b.workflow != "" && *b.workflow != g.Metadata.ID {
				continue
			}
			for _, n := range g.Spec.Nodes {
				id := g.Metadata.ID + "/" + n.ID
				result.Add(id, n.DisplayName(), fmt.Sprintf("%s · %s · %s", n.DisplayName(), g.Metadata.Name, id))
			}
		}
		return result, nil
	case "executions":
		row, err := s.Store.GetExecution(ctx, query)
		add := func(e *domain.Execution) {
			result.Add(e.ID, e.GraphID+"/"+e.NodeID, fmt.Sprintf("%s · %s/%s · %s", e.ID, e.GraphID, e.NodeID, e.Status))
		}
		if err == nil {
			add(row)
			return result, nil
		}
		if !errors.Is(err, storage.ErrNotFound) {
			return result, err
		}
		rows, err := s.Store.ListExecutions(ctx, storage.ExecutionFilter{Limit: 101})
		if err != nil {
			return result, err
		}
		if len(rows) > 100 {
			result.Partial = true
			rows = rows[:100]
		}
		for _, e := range rows {
			add(e)
		}
		return result, nil
	case "tasks":
		// A canonical task ID remains directly addressable beyond the search window.
		if query != "" {
			t, err := (taskconfig.Manager{Store: s.Store}).Get(ctx, query)
			if err == nil {
				result.Add(t.ID, t.Title, fmt.Sprintf("%s · %s · %s", t.Title, t.ID, t.Status))
				return result, nil
			}
			if !errors.Is(err, storage.ErrNotFound) {
				return result, err
			}
		}
	}
	view := chat.Inspection{Area: b.area, Search: &result}
	err := s.readChatInspection(ctx, actor, &view, time.Now())
	result.Partial = view.More
	return result, err
}

func (s *Server) discoverChatPlan(ctx context.Context, actor Identity, id string, plan chat.Plan) chat.Interpretation {
	if !actor.Role.Allows(plan.DiscoveryRole()) {
		return s.dispatchChatPlan(ctx, actor, id, plan)
	}
	slots := discoverySlots(&plan)
	if len(slots) == 0 {
		return s.dispatchChatPlan(ctx, actor, id, plan)
	}
	// Existing services have authoritative exact-ID lookups (including old,
	// settled or unlisted resources). Discovery handles unresolved selectors.
	var native chat.Interpretation
	if plan.Kind != chat.FindResource && !(plan.Kind == chat.ControlWorkflow && plan.Control.Workflow == "") {
		native = s.dispatchChatPlan(ctx, actor, id, plan)
		if native.Review != nil || native.Task != nil || native.Execution != nil || native.Retryable {
			return native
		}
		if native.Plan.Kind != chat.Clarify && plan.Kind != chat.RaiseBudget && plan.Kind != chat.InspectExecution && plan.Kind != chat.CancelExecution {
			return native
		}
	}
	changed := false
	for i, b := range slots {
		matches, err := s.searchChatResources(ctx, actor, b, *b.value)
		if err != nil {
			return discoveryUnavailable(plan)
		}
		switch {
		case len(matches.Choices) == 0:
			if matches.Incomplete() {
				return chat.AskDiscovery(plan, b.area, nil, "No match in the inspected records.", true)
			}
			if native.Text != "" {
				return native
			}
			return chat.Interpretation{Plan: plan, Text: "No matching " + b.area + " found in the available scope."}
		case len(matches.Choices) > 1 || matches.Incomplete():
			out := chat.AskDiscovery(plan, b.area, matches.Choices, "Which "+b.area+" entry should I use?", matches.Incomplete())
			out.Discovery.Field = i
			return out
		}
		c := matches.Choices[0]
		if plan.Kind == chat.FindResource {
			return foundResource(plan, b.area, c)
		}
		before := *b.value
		b.bind(c.ID)
		changed = changed || *b.value != before || b.workflow != nil
	}
	if !changed && native.Text != "" {
		return native
	}
	return s.dispatchChatPlan(ctx, actor, id, plan)
}

func discoveryUnavailable(plan chat.Plan) chat.Interpretation {
	return chat.Interpretation{Plan: plan, Text: "The resource search could not be completed. Retry the saved request.", Retryable: true}
}

func foundResource(plan chat.Plan, area string, c chat.Choice) chat.Interpretation {
	return chat.Interpretation{Plan: plan, Text: c.Label, Inventory: &chat.Inventory{Area: area, Items: []chat.InventoryReference{{Position: 1, ID: c.ID}}}, Destination: chat.InspectionDestination(area)}
}

func (s *Server) resumeDiscoveryChoice(ctx context.Context, actor Identity, sessionID string, d *chat.Discovery, choice chat.Choice) chat.Interpretation {
	plan := d.Plan
	slots := discoverySlots(&plan)
	if d.Field < 0 || d.Field >= len(slots) || slots[d.Field].area != d.Area {
		return chat.Interpretation{Text: "This choice is unavailable. Search again."}
	}
	slot := slots[d.Field]
	current, err := s.searchChatResources(ctx, actor, slot, choice.ID)
	if err != nil {
		return discoveryUnavailable(plan)
	}
	for _, c := range current.Choices {
		if c.ID != choice.ID {
			continue
		}
		var out chat.Interpretation
		if plan.Kind == chat.FindResource {
			out = foundResource(plan, d.Area, c)
		} else {
			slot.bind(c.ID)
			out = s.discoverChatPlan(ctx, actor, sessionID, plan)
		}
		out.Selection = &chat.Selection{Area: d.Area, ID: c.ID, Role: plan.DiscoveryRole()}
		return out
	}
	return chat.Interpretation{Text: "The selected resource is no longer available in this scope. Search again."}
}

func (s *Server) answerChatChoice(ctx context.Context, actor Identity, session *chat.Session, requestID, text string) (chat.Interpretation, bool) {
	d := session.PriorDiscovery(requestID)
	if d == nil {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(text)), "/choose ") {
			return chat.Interpretation{Text: "Start a resource search in this conversation before choosing."}, true
		}
		return chat.Interpretation{}, false
	}
	if !actor.Role.Allows(d.Plan.DiscoveryRole()) {
		if _, selection := (&chat.Discovery{}).Match(text); !selection {
			return chat.Interpretation{}, false
		}
		return chat.Interpretation{Text: "This choice is unavailable with your current permissions. Start a new request."}, true
	}
	c, ok := d.Match(text)
	if !ok {
		return chat.Interpretation{}, false
	}
	if c.ID == "" {
		return d.Reply(), true
	}
	return s.resumeDiscoveryChoice(ctx, actor, session.ID, d, c), true
}
