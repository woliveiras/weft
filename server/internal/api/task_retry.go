package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"github.com/woliveiras/weft/server/internal/taskconfig"
)

func (s *Server) retryTask(w http.ResponseWriter, r *http.Request) error {
	var input struct {
		Confirm bool `json:"confirm"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || !input.Confirm {
		return badRequest("explicit confirmation is required")
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return badRequest("expected one JSON object")
	}
	revision, err := ifMatchRevision(r)
	if err != nil {
		return err
	}
	if revision < 1 {
		return badRequest("If-Match is required")
	}
	draft, created, err := (taskconfig.Manager{Store: s.Store}).RetryFailed(r.Context(), r.PathValue("tid"), revision, identityFrom(r.Context()).Name)
	if err != nil {
		return taskError(err)
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	w.Header().Set("ETag", strconv.Quote(strconv.FormatInt(draft.Revision, 10)))
	w.Header().Set("Location", "/api/v1/tasks/"+draft.ID)
	writeJSON(w, status, draft)
	return nil
}
