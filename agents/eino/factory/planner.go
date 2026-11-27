package factory

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/woliveiras/weft/agents/eino/protocol"
)

type task struct {
	ID          string   `json:"id,omitempty"`
	TaskID      string   `json:"task_id"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Files       []string `json:"files"`
}
type question struct {
	ID       string `json:"id"`
	Question string `json:"question"`
}
type savedPlan struct {
	Request   string   `json:"request_id"`
	Title     string   `json:"title"`
	Tasks     []task   `json:"tasks"`
	Pending   []string `json:"pending"`
	Research  []string `json:"research"`
	Done      bool     `json:"done,omitempty"`
	LastInput string   `json:"lastInput,omitempty"`
}

func Planner(ctx context.Context) (protocol.Agent, error) {
	return messageAgent(ctx, "plan", map[string]action{"feature.request": planFeature, "research.done": resumePlan})
}

var bullet = regexp.MustCompile(`^(?:[-*•]|[0-9]+[.)])\s+`)
var sentence = regexp.MustCompile(`[.!?]\s+`)

func heuristicPlan(text string) (tasks []task, questions []question) {
	var parts []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if bullet.MatchString(line) {
			parts = append(parts, bullet.ReplaceAllString(line, ""))
		}
	}
	if len(parts) == 0 {
		text = strings.ReplaceAll(text, "\n", " ")
		last := 0
		for _, idx := range sentence.FindAllStringIndex(text, -1) {
			parts = append(parts, text[last:idx[0]+1])
			last = idx[1]
		}
		parts = append(parts, text[last:])
	}
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if strings.HasSuffix(p, "?") {
			questions = append(questions, question{Question: p})
		} else {
			r := []rune(strings.TrimRight(p, ".!"))
			tasks = append(tasks, task{Title: string(r[:min(len(r), 80)]), Description: p})
		}
	}
	if len(tasks) == 0 {
		tasks = append(tasks, task{Title: first(strings.TrimSpace(text), "implement feature"), Description: first(strings.TrimSpace(text), "implement feature")})
	}
	return tasks, questions
}

func planFeature(ctx context.Context, j *job) error {
	rid := first(j.text("id"), j.message.ID)
	title := first(j.text("title"), "untitled feature")
	description := first(j.text("description"), j.text("text"))
	acceptance := stringList(j.item["acceptance"])
	input, err := json.Marshal(map[string]any{"title": title, "description": description, "acceptance": acceptance})
	if err != nil {
		return err
	}
	reply, err := j.model(ctx, `Break this request into small tasks. Return JSON {"tasks":[{"id":"t1","title":"...","description":"...","files":[]}],"questions":[{"id":"q1","question":"..."}]}. Request: `+string(input), true)
	if err != nil {
		return err
	}
	var response struct {
		Tasks     []task
		Questions []question
	}
	if !decodeObject(reply, &response) {
		response.Tasks = nil
		response.Questions = nil
	}
	response.Tasks = slices.DeleteFunc(response.Tasks, func(t task) bool { return strings.TrimSpace(t.Title) == "" })
	response.Questions = slices.DeleteFunc(response.Questions, func(q question) bool { return strings.TrimSpace(q.Question) == "" })
	if len(response.Tasks) == 0 {
		response.Tasks, response.Questions = heuristicPlan(first(description, title))
	}
	plan := savedPlan{Request: rid, Title: title, Tasks: response.Tasks, Research: []string{}}
	var doc strings.Builder
	fmt.Fprintf(&doc, "# Plan %s: %s\n\n%s\n", rid, title, description)
	if len(acceptance) > 0 {
		fmt.Fprint(&doc, "\n## Acceptance\n")
		for _, a := range acceptance {
			fmt.Fprintf(&doc, "- %s\n", a)
		}
	}
	fmt.Fprint(&doc, "\n## Tasks\n")
	for i := range plan.Tasks {
		t := &plan.Tasks[i]
		t.TaskID = rid + "-" + first(t.ID, fmt.Sprintf("t%d", i+1))
		t.Description = first(t.Description, t.Title)
		if t.Files == nil {
			t.Files = []string{}
		}
		fmt.Fprintf(&doc, "- [ ] `%s` %s\n", t.TaskID, t.Title)
	}
	if len(response.Questions) > 0 {
		fmt.Fprint(&doc, "\n## Open questions\n")
	}
	for i, q := range response.Questions {
		qid := rid + "-" + first(q.ID, fmt.Sprintf("q%d", i+1))
		fmt.Fprintf(&doc, "- %s\n", q.Question)
		plan.Pending = append(plan.Pending, qid)
		j.emit("research.question", map[string]string{"request_id": rid, "question_id": qid, "question": q.Question})
	}
	if err := j.write(ctx, "plans/"+rid+".md", doc.String()); err != nil {
		return err
	}
	if len(plan.Pending) > 0 {
		return j.save(ctx, "plan:"+rid, plan)
	}
	emitTasks(j, &plan)
	return nil
}

func resumePlan(ctx context.Context, j *job) error {
	rid, qid := j.text("request_id"), j.text("question_id")
	if rid == "" || qid == "" {
		return nil
	}
	var plan savedPlan
	found, err := j.load(ctx, "plan:"+rid, &plan)
	if err != nil || !found {
		return err
	}
	if plan.Done {
		// A redelivered final input can follow a lost turn response. Replay the
		// same logical task IDs. A distinct research message cannot reopen it.
		if j.message.ID != "" && j.message.ID == plan.LastInput {
			emitTasks(j, &plan)
		}
		return nil
	}
	i := slices.Index(plan.Pending, qid)
	if i < 0 {
		return nil
	}
	plan.Pending = slices.Delete(plan.Pending, i, i+1)
	if path := j.text("findings_path"); path != "" {
		plan.Research = append(plan.Research, path)
	}
	if len(plan.Pending) == 0 {
		plan.Done, plan.LastInput = true, j.message.ID
		emitTasks(j, &plan)
	}
	return j.save(ctx, "plan:"+rid, plan)
}

func emitTasks(j *job, plan *savedPlan) {
	for _, t := range plan.Tasks {
		j.emit("task.ready", map[string]any{"request_id": plan.Request, "task_id": t.TaskID, "title": t.Title, "description": t.Description, "files": t.Files, "research": plan.Research})
	}
	j.summary = fmt.Sprintf("%s: emitted %d tasks", plan.Request, len(plan.Tasks))
}
