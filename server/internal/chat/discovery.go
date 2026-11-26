package chat

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/woliveiras/weft/server/internal/config"
)

type Choice struct {
	ID    string
	Label string
}

// Discovery is a server-generated continuation saved with the assistant reply.
// Only its numbered presentation is returned to the browser.
type Discovery struct {
	Area       string
	Field      int
	Choices    []Choice
	Plan       Plan
	Question   string
	Incomplete bool
}

type Selection struct {
	Area string
	ID   string
	Role config.Role
}

func (p Plan) DiscoveryRole() config.Role {
	switch p.Kind {
	case FindResource, InspectConsole, InspectTask, WatchTask, InspectExecution:
		return config.RoleViewer
	case AnalyzePR, GuideNode, ControlWorkflow, ScaleWorkflowNode, CancelExecution, PrepareWorkflowEnvironment, CorrectTask, CreateTask, RetryTask, DecideApproval:
		return config.RoleOperator
	default:
		return config.RoleAdmin
	}
}

func AskDiscovery(plan Plan, area string, choices []Choice, question string, incomplete bool) Interpretation {
	d := &Discovery{Area: area, Plan: plan, Question: question, Incomplete: incomplete}
	for _, c := range choices {
		if len(d.Choices) == InspectionLimit {
			d.Incomplete = true
			break
		}
		if c.ID == "" || c.Label == "" || !safeText(c.ID, 320) || !safeText(c.Label, 320) || strings.ContainsAny(c.ID+c.Label, "\n\r\t") {
			d.Incomplete = true
			continue
		}
		d.Choices = append(d.Choices, c)
	}
	return d.Reply()
}

func (d *Discovery) Reply() Interpretation {
	lines := []string{d.Question}
	for i, c := range d.Choices {
		lines = append(lines, fmt.Sprintf("%d. %s", i+1, c.Label))
	}
	if d.Incomplete {
		lines = append(lines, "Search incomplete. Choose a displayed match or narrow the scope.")
	}
	if len(d.Choices) > 0 {
		lines = append(lines, "Reply with the number or a distinguishing name.")
	}
	return Interpretation{Plan: Plan{Kind: Clarify, Title: d.Plan.Title}, Text: strings.Join(lines, "\n"), Discovery: d}
}

var discoveryOrdinals = map[string]int{
	"first": 1, "primeiro": 1, "primeira": 1,
	"second": 2, "segundo": 2, "segunda": 2,
	"third": 3, "terceiro": 3, "terceira": 3,
	"fourth": 4, "quarto": 4, "quarta": 4,
	"fifth": 5, "quinto": 5, "quinta": 5,
	"sixth": 6, "sexto": 6, "sexta": 6,
	"seventh": 7, "sétimo": 7, "sétima": 7,
	"eighth": 8, "oitavo": 8, "oitava": 8,
}

// Match accepts a displayed number or a distinguishing fragment of a label.
func (d *Discovery) Match(text string) (Choice, bool) {
	q := strings.ToLower(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(text), ".")))
	explicit := false
	for _, prefix := range []string{"/choose ", "choose ", "select ", "use ", "escolha ", "selecione ", "opção ", "option ", "the one about ", "the one in ", "o do ", "a da ", "o de ", "a de ", "a do ", "o da "} {
		if strings.HasPrefix(q, prefix) {
			q = strings.TrimSpace(strings.TrimPrefix(q, prefix))
			explicit = true
			break
		}
	}
	for _, p := range []string{"the ", "o ", "a "} {
		q = strings.TrimPrefix(q, p)
	}

	n, err := strconv.Atoi(q)
	if pos, ok := discoveryOrdinals[q]; ok {
		n = pos
		err = nil
	}
	if err == nil {
		if n >= 1 && n <= len(d.Choices) {
			return d.Choices[n-1], true
		}
		return Choice{}, true
	}
	if q == "" {
		return Choice{}, explicit
	}
	for _, c := range d.Choices {
		if c.ID == strings.TrimSpace(text) {
			return c, true
		}
	}
	var match Choice
	for _, c := range d.Choices {
		if strings.EqualFold(c.ID, q) || strings.Contains(strings.ToLower(c.Label), q) {
			if match.ID != "" {
				return Choice{}, true
			}
			match = c
		}
	}
	return match, explicit || match.ID != ""
}

func (s *Session) PriorDiscovery(requestID string) *Discovery {
	var latest *Discovery
	for _, m := range s.Messages {
		if m.Role == "user" {
			if m.ID == requestID {
				return latest
			}
			latest = nil
		} else if m.Role == "assistant" && strings.HasPrefix(m.ID, "reply-") {
			latest = m.Discovery
		}
	}
	return nil
}

// Search ranks a canonical ID ahead of exact names, then partial names.
// Only the bounded displayed candidates are retained.
type ResourceSearch struct {
	Query    string
	Choices  []Choice
	Partial  bool
	rank     int
	overflow bool
}

func (s *ResourceSearch) Add(id, name, label string) {
	q := strings.ToLower(strings.TrimSpace(s.Query))
	rank := 1
	switch {
	case id == s.Query:
		rank = 3
	case strings.EqualFold(name, q) && q != "":
		rank = 2
	case !strings.Contains(strings.ToLower(name), q) && !strings.Contains(strings.ToLower(id), q):
		return
	}
	if rank < s.rank {
		return
	}
	if rank > s.rank {
		s.Choices = nil
		s.overflow = false
		s.rank = rank
	}
	if len(s.Choices) >= InspectionLimit {
		s.overflow = true
		return
	}
	if !safeText(label, 320) || !safeText(id, 320) || strings.ContainsAny(label+id, "\r\n\t") || id == "" || label == "" {
		s.overflow = true
		return
	}
	s.Choices = append(s.Choices, Choice{ID: id, Label: label})
}

func (s *ResourceSearch) Incomplete() bool { return s.overflow || (s.Partial && s.rank != 3) }

func SearchableArea(area string) bool {
	switch area {
	case "tasks", "workflows", "agents", "projects", "nodes", "skills", "approvals", "triggers", "review-routes", "mcp", "scm", "issue-connections", "models", "plugins", "tools", "budgets", "executions":
		return true
	}
	return false
}

func ExplicitFind(text string) (Interpretation, bool) {
	fields := strings.Fields(text)
	if len(fields) == 0 || fields[0] != "/find" {
		return Interpretation{}, false
	}
	if len(fields) < 2 || !SearchableArea(fields[1]) {
		return Interpretation{Text: "Use /find followed by tasks, workflows, nodes, agents, projects, executions, skills, approvals, triggers, review-routes, mcp, scm, issue-connections, models, plugins, tools or budgets, and optionally a name."}, true
	}
	query := strings.Join(fields[2:], " ")
	if !safeText(query, 200) {
		return Interpretation{Text: "Use a shorter resource name."}, true
	}
	return Interpretation{Plan: Plan{Kind: FindResource, Area: fields[1], Query: query, Title: "Find " + fields[1]}}, true
}

const FindResource = "resource.find"
