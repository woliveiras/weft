package api_test

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/cost"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/llm"
)

func TestChatEvalSpendBreakdown(t *testing.T) {
	e := newChatEval(t, func(req *llm.ChatRequest) (*llm.ChatResponse, error) {
		last := req.Messages[len(req.Messages)-1]
		if last.Role == llm.RoleUser {
			return evalTool("query_spend", map[string]string{"groupBy": "provider", "period": "2025-02"})
		}
		var report chat.SpendObservation
		if last.Name != "query_spend" {
			return nil, fmt.Errorf("unexpected observation: %s", last.Name)
		}
		if err := json.Unmarshal([]byte(last.Content), &report); err != nil {
			return nil, err
		}
		if len(report.Rows) == 0 {
			return nil, fmt.Errorf("empty spend breakdown")
		}
		row := report.Rows[0]
		return &llm.ChatResponse{Message: llm.ChatMessage{Role: llm.RoleAssistant, Content: fmt.Sprintf("%s / %s: %.2f %s", row.Key, row.Model, float64(row.CostMicros)/1e6, report.Currency)}}, nil
	})
	for _, x := range []struct {
		month           time.Month
		provider, model string
		micros          int64
	}{
		{time.February, "atlas", "silk-small", 1500000}, {time.February, "atlas", "silk-small", 750000}, {time.February, "birch", "linen-large", 1250000}, {time.March, "wrong-month", "winter", 9000000},
	} {
		event := domain.NewEvent(domain.EvCostRecorded, "", cost.Record{Source: "gateway", RateSource: "catalogue", Provider: x.provider, Model: x.model, Currency: "USD", Priced: true, CostMicros: x.micros})
		event.Time = time.Date(2025, x.month, 3, 12, 0, 0, 0, time.UTC)
		if err := e.app.Store.AppendEvent(t.Context(), event); err != nil {
			t.Fatal(err)
		}
	}
	s := e.ask(t, "", viewerTok, "Em fevereiro de 2025, qual combinação de provedor e modelo teve o maior gasto registrado no Weft? Me diga o nome dos dois e o valor em dólares.")
	msg := s.Messages[len(s.Messages)-1]
	if msg.Review != nil || msg.Destination != "usage" || !strings.Contains(strings.ToLower(msg.Text), "atlas") || !strings.Contains(strings.ToLower(msg.Text), "silk-small") || !regexp.MustCompile(`\b2[,.]25\b`).MatchString(msg.Text) || strings.Contains(msg.Text, "wrong-month") {
		t.Fatalf("expected the winning provider/model and cost for February: %+v", msg)
	}
	var observed bool
	for _, c := range e.calls() {
		if c.Name != "query_spend" {
			t.Fatalf("unexpected operation: %s", c.Name)
		}
		q, err := chat.ParseSpendQuery(c.Arguments, time.Now())
		if err == nil && q.GroupBy == "provider" && q.Period == "2025-02" {
			observed = true
		}
	}
	if !observed {
		t.Fatal("no verified provider report for the requested month")
	}
}
