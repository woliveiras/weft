package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/woliveiras/weft/server/internal/trigger"
)

// Issue triggers start tasks without a person present, under consent the
// administrator who creates or resumes them records. Reads are open to
// viewers. Operators can resolve holds; other changes need an administrator.

func (s *Server) triggers() trigger.Manager {
	if s.TriggerManager != nil {
		return *s.TriggerManager
	}
	return trigger.Manager{Store: s.Store, Bus: s.Bus, Tools: s.Tools}
}

func decodeTrigger(r *http.Request) (trigger.Input, error) {
	var in trigger.Input
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return in, badRequest("invalid trigger body: %s", err.Error())
	}
	return in, nil
}

func (s *Server) triggerPolicy(w http.ResponseWriter, r *http.Request) error {
	policy, err := s.triggers().ReadinessPolicy(r.Context())
	if err != nil {
		return err
	}
	writeJSON(w, 200, policy)
	return nil
}

func (s *Server) listTriggers(w http.ResponseWriter, r *http.Request) error {
	list, err := s.triggers().List(r.Context(), r.URL.Query().Get("workflow"))
	if err != nil {
		return err
	}
	writeJSON(w, 200, list)
	return nil
}

func (s *Server) getTrigger(w http.ResponseWriter, r *http.Request) error {
	t, err := s.triggers().Get(r.Context(), r.PathValue("trigger"))
	if err != nil {
		return err
	}
	writeJSON(w, 200, t)
	return nil
}

func (s *Server) createTrigger(w http.ResponseWriter, r *http.Request) error {
	in, err := decodeTrigger(r)
	if err != nil {
		return err
	}
	t, err := s.triggers().Create(r.Context(), in, identityFrom(r.Context()).Name)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, t)
	return nil
}

func (s *Server) updateTrigger(w http.ResponseWriter, r *http.Request) error {
	in, err := decodeTrigger(r)
	if err != nil {
		return err
	}
	t, err := s.triggers().Edit(r.Context(), r.PathValue("trigger"), in, identityFrom(r.Context()).Name)
	if err != nil {
		return err
	}
	writeJSON(w, 200, t)
	return nil
}

func (s *Server) deleteTrigger(w http.ResponseWriter, r *http.Request) error {
	if err := s.triggers().Delete(r.Context(), r.PathValue("trigger"), identityFrom(r.Context()).Name); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) resolveTriggerHold(w http.ResponseWriter, r *http.Request) error {
	var input struct {
		Key      string    `json:"key"`
		OpenedAt time.Time `json:"openedAt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil || input.Key == "" || len(input.Key) > 160 || input.OpenedAt.IsZero() {
		return badRequest("an issue key and hold opening timestamp are required")
	}
	t, err := s.triggers().ResolveHold(r.Context(), r.PathValue("trigger"), input.Key, input.OpenedAt, identityFrom(r.Context()).Name)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, t)
	return nil
}

func (s *Server) pauseTrigger(w http.ResponseWriter, r *http.Request) error {
	t, err := s.triggers().Pause(r.Context(), r.PathValue("trigger"), "Paused by "+identityFrom(r.Context()).Name+".", identityFrom(r.Context()).Name)
	if err != nil {
		return err
	}
	writeJSON(w, 200, t)
	return nil
}

func (s *Server) resumeTrigger(w http.ResponseWriter, r *http.Request) error {
	t, err := s.triggers().Resume(r.Context(), r.PathValue("trigger"), identityFrom(r.Context()).Name)
	if err != nil {
		return err
	}
	writeJSON(w, 200, t)
	return nil
}

// previewTrigger lists what the next run would do, without writing to the
// tracker or starting anything.
func (s *Server) previewTrigger(w http.ResponseWriter, r *http.Request) error {
	t, err := s.triggers().Get(r.Context(), r.PathValue("trigger"))
	if err != nil {
		return err
	}
	runner := trigger.Runner{Triggers: s.triggers(), Store: s.Store, Gateway: s.Gateway}
	m, _, _, err := runner.Match(r.Context(), t)
	if err != nil {
		writeJSON(w, 200, map[string]any{"error": err.Error()})
		return nil
	}
	writeJSON(w, 200, m)
	return nil
}
