package api

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/environment"
)

func (s *Server) historyRetention(w http.ResponseWriter, r *http.Request) error {
	v, err := s.Environments.Retention(r.Context())
	if err != nil {
		return err
	}
	writeJSON(w, 200, v)
	return nil
}
func (s *Server) saveHistoryRetention(w http.ResponseWriter, r *http.Request) error {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var in struct {
		Policy   environment.RetentionPolicy `json:"policy"`
		Revision *int64                      `json:"revision"`
		Confirm  bool                        `json:"confirm"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return badRequest("invalid retention JSON")
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return badRequest("expected one JSON object")
	}
	if in.Revision == nil || *in.Revision < 0 {
		return badRequest("revision is required and must be nonnegative")
	}
	if !in.Confirm {
		return badRequest("confirm retention changes before saving")
	}
	if err := in.Policy.Validate(); err != nil {
		return badRequest("%s", err)
	}
	actor := identityFrom(r.Context()).Name
	v, err := s.Environments.SaveRetention(r.Context(), in.Policy, *in.Revision, actor)
	if err != nil {
		return err
	}
	s.Bus.Publish(r.Context(), domain.NewEvent("installation.history_retention.updated", "", map[string]any{"revision": v.Revision, "days": v.Policy.Days, "maxEvents": v.Policy.MaxEvents}).WithActor(actor))
	s.Log.Info("blocked history retention updated", "revision", v.Revision, "days", v.Policy.Days, "maxEvents", v.Policy.MaxEvents, "actor", actor)
	writeJSON(w, 200, v)
	return nil
}
