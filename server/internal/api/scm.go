package api

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/woliveiras/weft/server/internal/scm"
)

// SCM connections hold the server-side credentials push and pull request
// tools use. Reads never return a token; writes need an administrator;
// connection tests (which call the provider) need an operator.

func decodeSCMInput(r *http.Request) (scm.Input, error) {
	var in scm.Input
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		// Never echo the body: it may hold a token.
		return in, badRequest("invalid SCM connection body")
	}
	return in, nil
}

func (s *Server) scmConnections(w http.ResponseWriter, r *http.Request) error {
	list, err := s.SCM.List(r.Context())
	if err != nil {
		return err
	}
	writeJSON(w, 200, list)
	return nil
}

func (s *Server) createSCMConnection(w http.ResponseWriter, r *http.Request) error {
	in, err := decodeSCMInput(r)
	if err != nil {
		return err
	}
	v, err := s.SCM.Save(r.Context(), in, identityFrom(r.Context()).Name, false)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, v)
	return nil
}

func (s *Server) updateSCMConnection(w http.ResponseWriter, r *http.Request) error {
	in, err := decodeSCMInput(r)
	if err != nil {
		return err
	}
	in.Name = r.PathValue("name")
	v, err := s.SCM.Save(r.Context(), in, identityFrom(r.Context()).Name, true)
	if err != nil {
		return err
	}
	writeJSON(w, 200, v)
	return nil
}

func (s *Server) deleteSCMConnection(w http.ResponseWriter, r *http.Request) error {
	if err := s.SCM.Delete(r.Context(), r.PathValue("name"), identityFrom(r.Context()).Name); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) testSCMConnection(w http.ResponseWriter, r *http.Request) error {
	id, err := s.SCM.Test(r.Context(), r.PathValue("name"))
	if err != nil {
		writeJSON(w, 200, map[string]any{"ok": false, "error": err.Error()})
		return nil
	}
	writeJSON(w, 200, map[string]any{"ok": true, "identity": id})
	return nil
}

func (s *Server) scmKinds(w http.ResponseWriter, r *http.Request) error {
	writeJSON(w, http.StatusOK, scm.Kinds())
	return nil
}

func (s *Server) setSCMInlinePublication(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Enabled  *bool     `json:"enabled"`
		Revision time.Time `json:"revision"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 512))
	dec.DisallowUnknownFields()
	if dec.Decode(&in) != nil || dec.Decode(&struct{}{}) != io.EOF || in.Enabled == nil || in.Revision.IsZero() {
		return badRequest("expected an explicit choice and the reviewed connection revision")
	}
	saved, err := s.SCM.SetInlinePublication(r.Context(), r.PathValue("name"), in.Revision, *in.Enabled, identityFrom(r.Context()).Name)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, saved)
	return nil
}
