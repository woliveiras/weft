package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/woliveiras/weft/server/internal/jiradc"
	"github.com/woliveiras/weft/server/internal/storage"
)

func (s *Server) lifecycleSyncs(w http.ResponseWriter, r *http.Request) error {
	rows, next, err := s.triggers().LifecycleSyncPage(r.Context(), r.URL.Query().Get("after"), 100)
	if err != nil {
		return err
	}
	writeJSON(w, 200, map[string]any{"items": rows, "next": next})
	return nil
}
func (s *Server) getLifecycleSync(w http.ResponseWriter, r *http.Request) error {
	v, err := s.triggers().GetLifecycleSync(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	writeJSON(w, 200, v)
	return nil
}
func (s *Server) reconcileLifecycleSync(w http.ResponseWriter, r *http.Request) error {
	return s.recoverLifecycleSync(w, r, false)
}
func (s *Server) retryLifecycleSync(w http.ResponseWriter, r *http.Request) error {
	return s.recoverLifecycleSync(w, r, true)
}
func (s *Server) recoverLifecycleSync(w http.ResponseWriter, r *http.Request, retry bool) error {
	revision, err := issueConnectionRevision(r)
	if err != nil {
		return err
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 129))
	if err != nil || len(raw) > 128 {
		return badRequest("invalid recovery confirmation")
	}
	var in struct {
		Confirm bool `json:"confirm"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var extra any
	if dec.Decode(&in) != nil || dec.Decode(&extra) != io.EOF || !in.Confirm {
		return badRequest("confirm the reviewed synchronization recovery")
	}
	m := s.triggers()
	var out any
	if retry {
		out, err = m.RetryLifecycleSync(r.Context(), r.PathValue("id"), revision, identityFrom(r.Context()).Name)
	} else {
		out, err = m.ReconcileLifecycleSync(r.Context(), r.PathValue("id"), revision, identityFrom(r.Context()).Name)
	}
	if err != nil {
		if errors.Is(err, storage.ErrConflict) || errors.Is(err, storage.ErrNotFound) {
			return err
		}
		if errors.Is(err, jiradc.ErrSyncUnconfirmed) {
			writeJSON(w, 200, out)
			return nil
		}
		// Do not expose internal storage/remote failure text at this operator boundary.
		writeErr(w, http.StatusUnprocessableEntity, "tracker_sync_recovery", "Recovery could not be confirmed. Refresh the synchronization and review its connection, mapping and retained dispatch evidence.", nil)
		return nil
	}
	writeJSON(w, 200, out)
	return nil
}
