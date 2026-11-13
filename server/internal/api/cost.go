package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/woliveiras/weft/server/internal/cost"
	"github.com/woliveiras/weft/server/internal/storage"
)

// Monetary cost control. Prices are readable by viewers (they are not
// secret) and changed by administrators; every change is a new catalogue
// version so recorded costs stay explainable.

func (s *Server) prices() cost.Prices { return cost.Prices{Store: s.Store, Bus: s.Bus} }

func (s *Server) getPrices(w http.ResponseWriter, r *http.Request) error {
	c, rev, err := s.prices().Get(r.Context())
	if err != nil {
		return err
	}
	w.Header().Set("ETag", strconv.Quote(strconv.FormatInt(rev, 10)))
	writeJSON(w, 200, c)
	return nil
}

func (s *Server) putPrices(w http.ResponseWriter, r *http.Request) error {
	rev, err := ifMatchRevision(r)
	if err != nil {
		return err
	}
	var in cost.CatalogueInput
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return badRequest("invalid price catalogue body: %s", err.Error())
	}
	c, err := s.prices().Put(r.Context(), in, identityFrom(r.Context()).Name, rev)
	if err != nil {
		return err
	}
	_, newRev, err := s.prices().Get(r.Context())
	if err != nil {
		return err
	}
	w.Header().Set("ETag", strconv.Quote(strconv.FormatInt(newRev, 10)))
	writeJSON(w, 200, c)
	return nil
}

func (s *Server) budgets() cost.Budgets { return cost.Budgets{Store: s.Store, Bus: s.Bus} }

func (s *Server) getCostPolicy(w http.ResponseWriter, r *http.Request) error {
	p, rev, err := s.budgets().Policy(r.Context())
	if err != nil {
		return err
	}
	w.Header().Set("ETag", strconv.Quote(strconv.FormatInt(rev, 10)))
	writeJSON(w, 200, p)
	return nil
}

func (s *Server) putCostPolicy(w http.ResponseWriter, r *http.Request) error {
	rev, err := ifMatchRevision(r)
	if err != nil {
		return err
	}
	var in struct {
		TaskRunBudgetMicros int64 `json:"taskRunBudgetMicros"`
		WarningPercent      int   `json:"warningPercent"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return badRequest("invalid cost policy body: %s", err.Error())
	}
	p, err := s.budgets().SetPolicy(r.Context(), cost.Policy{TaskRunBudgetMicros: in.TaskRunBudgetMicros, WarningPercent: in.WarningPercent}, identityFrom(r.Context()).Name, rev)
	if err != nil {
		return err
	}
	writeJSON(w, 200, p)
	return nil
}

func (s *Server) getBudget(w http.ResponseWriter, r *http.Request) error {
	v, err := s.budgets().Ledger(r.Context(), r.PathValue("scope"), r.PathValue("budget"))
	if err != nil {
		return err
	}
	if v == nil {
		return fmt.Errorf("%w: no %s budget for %q", storage.ErrNotFound, r.PathValue("scope"), r.PathValue("budget"))
	}
	writeJSON(w, 200, v)
	return nil
}

// raiseBudget is the audited human decision that lets a paused run
// continue: it raises the limit and resumes the graph the budget paused.
func (s *Server) raiseBudget(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		LimitMicros int64 `json:"limitMicros"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return badRequest(`body must be {"limitMicros": <new limit>}`)
	}
	actor := identityFrom(r.Context()).Name
	v, resume, err := s.budgets().Raise(r.Context(), r.PathValue("scope"), r.PathValue("budget"), in.LimitMicros, actor)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
	defer cancel()
	result := struct {
		cost.View
		NotResumed []string `json:"notResumed,omitempty"`
	}{View: v}
	for _, graph := range resume {
		if err := s.Engine.BudgetGraphAction(ctx, graph, "resume", actor); err != nil {
			result.NotResumed = append(result.NotResumed, graph)
			s.Log.Warn("budget raised but graph was not resumed", "graph", graph, "err", err)
		}
	}
	writeJSON(w, 200, result)
	return nil
}

func (s *Server) getBudgetDefinitions(w http.ResponseWriter, r *http.Request) error {
	defs, rev, err := s.budgets().Definitions(r.Context())
	if err != nil {
		return err
	}
	w.Header().Set("ETag", strconv.Quote(strconv.FormatInt(rev, 10)))
	writeJSON(w, 200, map[string]any{"budgets": defs, "period": cost.Period(time.Now())})
	return nil
}

func (s *Server) putBudgetDefinitions(w http.ResponseWriter, r *http.Request) error {
	rev, err := ifMatchRevision(r)
	if err != nil {
		return err
	}
	var in struct {
		Budgets []cost.Definition `json:"budgets"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return badRequest("invalid budgets body: %s", err.Error())
	}
	defs, err := s.budgets().SetDefinitions(r.Context(), in.Budgets, identityFrom(r.Context()).Name, rev)
	if err != nil {
		return err
	}
	writeJSON(w, 200, map[string]any{"budgets": defs, "period": cost.Period(time.Now())})
	return nil
}

func periodParam(r *http.Request) string {
	if p := r.URL.Query().Get("period"); p != "" {
		return p
	}
	return cost.Period(time.Now())
}

// getSpend aggregates the month's cost records (optionally for one graph).
func (s *Server) getSpend(w http.ResponseWriter, r *http.Request) error {
	rep, err := cost.Spend(r.Context(), s.Store, periodParam(r), r.URL.Query().Get("graph"))
	if err != nil {
		return err
	}
	writeJSON(w, 200, rep)
	return nil
}

// getLedgers lists the month's budget ledgers and the task runs a budget
// paused.
func (s *Server) getLedgers(w http.ResponseWriter, r *http.Request) error {
	views, err := s.budgets().Ledgers(r.Context(), periodParam(r))
	if err != nil {
		return err
	}
	writeJSON(w, 200, views)
	return nil
}

// previewBudgets lists the budgets a new task by the caller for a workflow
// and project would be held to.
func (s *Server) previewBudgets(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	views, err := s.budgets().Preview(r.Context(), cost.Target{User: identityFrom(r.Context()).Name, Project: q.Get("project"), Workflow: q.Get("workflow")})
	if err != nil {
		return err
	}
	p, _, err := s.budgets().Policy(r.Context())
	if err != nil {
		return err
	}
	cat, _, err := s.prices().Get(r.Context())
	if err != nil {
		return err
	}
	writeJSON(w, 200, map[string]any{"taskRunBudgetMicros": p.TaskRunBudgetMicros, "currency": cat.Currency, "budgets": views})
	return nil
}
