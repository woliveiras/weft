package chat

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/cost"
	"github.com/woliveiras/weft/server/internal/llm"
)

var ErrSpendUnavailable = errors.New("the spend report could not be read")

var spendArguments = []operationArgument{
	requiredArgument("groupBy", "Use total for the installation summary, or group ranked costs by the requested dimension. Provider groups include the model.", "total", "provider", "user", "project", "workflow", "task"),
	optionalArgument("period", "UTC calendar month as YYYY-MM, current or previous. Defaults to current."),
	optionalArgument("search", "Case-insensitive substring of the group name or model in this breakdown."),
	optionalArgument("page", "Page of 20 matching rows from the native report, most expensive first.", "1", "2", "3", "4", "5"),
	optionalArgument("title", "Short neutral conversation title in the user's language."),
}

func SpendQuerySchema() llm.ToolSchema {
	schema, _ := json.Marshal(operationObjectSchema(spendArguments))
	return llm.ToolSchema{Name: "query_spend", Description: "Read recorded Weft inference spend, calls and token usage for one month, including breakdowns, estimated amounts and unpriced calls. Amounts are in millionths of the stated currency. The installation total covers every group; search filters rows only. Report incomplete history when truncated. This tool does not change budgets or prices.", Parameters: schema}
}

type SpendQuery struct {
	GroupBy string
	Period  string
	Search  string
	Page    int
	Title   string
}

func ParseSpendQuery(raw json.RawMessage, now time.Time) (SpendQuery, error) {
	fields, err := decodeOperationArguments(raw, spendArguments)
	if err != nil {
		return SpendQuery{}, err
	}
	var values map[string]string
	if err := json.Unmarshal(raw, &values); err != nil {
		return SpendQuery{}, ErrInvalidPlan
	}
	q := SpendQuery{GroupBy: values["groupBy"], Period: values["period"], Search: strings.TrimSpace(values["search"]), Page: 1, Title: cleanTitle(values["title"])}
	switch q.Period {
	case "", "current":
		q.Period = cost.Period(now)
	case "previous":
		utc := now.UTC()
		q.Period = cost.Period(time.Date(utc.Year(), utc.Month()-1, 1, 0, 0, 0, 0, time.UTC))
	default:
		if _, err := time.Parse("2006-01", q.Period); err != nil {
			return SpendQuery{}, ErrInvalidPlan
		}
	}
	if fields["search"] != nil && !shortLabel(q.Search) || q.GroupBy == "total" && (fields["search"] != nil || fields["page"] != nil) {
		return SpendQuery{}, ErrInvalidPlan
	}
	if page, ok := values["page"]; ok {
		q.Page, err = strconv.Atoi(page)
		if err != nil {
			return SpendQuery{}, ErrInvalidPlan
		}
	}
	return q, nil
}

type SpendObservation struct {
	Period         string      `json:"period"`
	Currency       string      `json:"currency"`
	Total          cost.Line   `json:"installationTotal"`
	Truncated      bool        `json:"historyTruncated"`
	GroupBy        string      `json:"groupBy"`
	Rows           []cost.Line `json:"rows,omitempty"`
	MatchingGroups int         `json:"matchingGroupsInReport"`
	More           bool        `json:"hasMorePages"`
}

func (q SpendQuery) Observe(r cost.Report) (SpendObservation, error) {
	result := SpendObservation{Period: r.Period, Currency: r.Currency, Total: r.Total, Truncated: r.Truncated, GroupBy: q.GroupBy}
	var rows []cost.Line
	switch q.GroupBy {
	case "total":
		return result, nil
	case "provider":
		rows = r.ByProvider
	case "user":
		rows = r.ByUser
	case "project":
		rows = r.ByProject
	case "workflow":
		rows = r.ByWorkflow
	case "task":
		rows = r.ByTask
	default:
		return SpendObservation{}, ErrInvalidPlan
	}
	const pageSize = 20
	start := (q.Page - 1) * pageSize
	search := strings.ToLower(q.Search)
	for _, line := range rows {
		if !strings.Contains(strings.ToLower(line.Key+" "+line.Model), search) {
			continue
		}
		result.MatchingGroups++
		if result.MatchingGroups <= start || result.MatchingGroups > start+pageSize {
			continue
		}
		if !shortLabel(line.Key) || line.Model != "" && !shortLabel(line.Model) {
			return SpendObservation{}, ErrSpendUnavailable
		}
		result.Rows = append(result.Rows, line)
	}
	result.More = result.MatchingGroups > start+pageSize
	return result, nil
}
