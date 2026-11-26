package chat

import (
	"bytes"
	"encoding/json"
	"io"
	"slices"
	"time"

	"github.com/woliveiras/weft/server/internal/llm"
)

var taskStates = []string{
	"draft", "preparing", "environment_pending", "environment_build_requested",
	"environment_cancel_requested", "starting", "running", "awaiting_approval", "blocked",
	"succeeded", "failed", "interrupted", "cancel_requested", "cancelled", "delivery_pending",
}

var taskQueryModes = []string{"list", "count", "exists"}

func TaskQuerySchema() llm.ToolSchema {
	// These lists contain only strings and are also used by the argument validator.
	states, _ := json.Marshal(taskStates)
	modes, _ := json.Marshal(taskQueryModes)
	return llm.ToolSchema{
		Name:        "query_tasks",
		Description: "Query Weft tasks by lifecycle status. Count all matches, check existence, or list the most recently updated matches. Omit status to include all tasks.",
		Parameters: json.RawMessage(`{
			"type":"object",
			"properties":{
				"mode":{"type":"string","description":"Use count for how many, exists for whether any match (yes or no), and list for names or recent individual tasks.","enum":` + string(modes) + `},
				"status":{"type":"string","description":"Lifecycle filter: running means work in progress, succeeded means finished successfully, failed means ended with an error. Omit only for questions about all states. Never use an empty string.","enum":` + string(states) + `},
				"limit":{"type":"integer","minimum":1,"maximum":8,"description":"Maximum items for list mode only"},
				"title":{"type":"string","description":"Short neutral conversation title in the user's language"}
			},
			"required":["mode"],
			"additionalProperties":false
		}`),
	}
}

type TaskQuery struct {
	Mode   string `json:"mode"`
	Status string `json:"status,omitempty"`
	Limit  int    `json:"limit,omitempty"`
	Title  string `json:"title,omitempty"`
}

func ParseTaskQuery(raw json.RawMessage) (TaskQuery, error) {
	var query TaskQuery
	dec := json.NewDecoder(bytes.NewReader(raw))
	if start, err := dec.Token(); err != nil || start != json.Delim('{') {
		return TaskQuery{}, ErrInvalidPlan
	}
	seen := map[string]bool{}
	for dec.More() {
		key, err := dec.Token()
		name, ok := key.(string)
		if err != nil || !ok || seen[name] {
			return TaskQuery{}, ErrInvalidPlan
		}
		seen[name] = true
		if name == "limit" {
			if err := dec.Decode(&query.Limit); err != nil || query.Limit < 1 || query.Limit > InspectionLimit {
				return TaskQuery{}, ErrInvalidPlan
			}
			continue
		}
		var value *string
		if err := dec.Decode(&value); err != nil || value == nil {
			return TaskQuery{}, ErrInvalidPlan
		}
		switch name {
		case "mode":
			query.Mode = *value
		case "status":
			if !slices.Contains(taskStates, *value) {
				return TaskQuery{}, ErrInvalidPlan
			}
			query.Status = *value
		case "title":
			query.Title = *value
		default:
			return TaskQuery{}, ErrInvalidPlan
		}
	}
	if end, err := dec.Token(); err != nil || end != json.Delim('}') || dec.Decode(new(any)) != io.EOF ||
		!slices.Contains(taskQueryModes, query.Mode) || query.Mode != "list" && seen["limit"] {
		return TaskQuery{}, ErrInvalidPlan
	}
	if query.Mode == "list" && !seen["limit"] {
		query.Limit = InspectionLimit
	}
	query.Title = cleanTitle(query.Title)
	return query, nil
}

type TaskQueryItem struct {
	ID        string    `json:"id"`
	Title     string    `json:"title,omitempty"`
	Status    string    `json:"status"`
	UpdatedAt time.Time `json:"updatedAt"`
}
type TaskQueryResult struct {
	Mode   string          `json:"mode"`
	Status string          `json:"status,omitempty"`
	Total  int             `json:"total"`
	Items  []TaskQueryItem `json:"items,omitempty"`
}

func (q *TaskQueryResult) Add(item TaskQueryItem, limit int) error {
	if !slices.Contains(taskStates, item.Status) || !validReferenceID(item.ID) {
		return ErrReadUnavailable
	}
	if q.Status != "" && item.Status != q.Status {
		return nil
	}
	q.Total++
	if len(q.Items) >= limit {
		return nil
	}
	item.Title = cleanTitle(item.Title)
	q.Items = append(q.Items, item)
	return nil
}
