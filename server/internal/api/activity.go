package api

import (
	"errors"
	"net/http"

	"github.com/woliveiras/weft/server/internal/activity"
	"github.com/woliveiras/weft/server/internal/config"
)

// activityView serves GET /api/v1/activity: one bounded, role-filtered
// summary of running agents, pending decisions and recent executions.
func (s *Server) activityView(w http.ResponseWriter, r *http.Request) error {
	q, err := activity.ParseQuery(r.URL.Query())
	if errors.Is(err, activity.ErrInvalid) {
		return badRequest("%v", err)
	}
	if err != nil {
		return err
	}
	id := identityFrom(r.Context())
	b := activity.Builder{Store: s.Store}
	if s.Reviews != nil {
		b.Reviews = s.Reviews
	}
	view, err := b.Build(r.Context(), q, activity.Viewer{Name: id.Name, CanReview: id.Role.Allows(config.RoleOperator)})
	if err != nil {
		return err
	}
	for _, e := range view.Errors {
		if e.Err != nil {
			s.Log.Warn("activity section unavailable", "section", e.Section, "err", e.Err)
		}
	}
	writeJSON(w, http.StatusOK, view)
	return nil
}
