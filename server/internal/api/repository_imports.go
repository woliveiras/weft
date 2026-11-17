package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/woliveiras/weft/server/internal/projectconfig"
	"github.com/woliveiras/weft/server/internal/repositoryimport"
)

func (s *Server) listRepositoryImports(w http.ResponseWriter, r *http.Request) error {
	if s.RepositoryImports == nil {
		return badRequest("Repository imports are unavailable on this server")
	}
	result, err := s.RepositoryImports.List(r.Context())
	if err != nil {
		return err
	}
	writeJSON(w, 200, result)
	return nil
}
func (s *Server) getRepositoryImport(w http.ResponseWriter, r *http.Request) error {
	if s.RepositoryImports == nil {
		return badRequest("Repository imports are unavailable on this server")
	}
	result, err := s.RepositoryImports.Get(r.Context(), r.PathValue("iid"))
	if err != nil {
		return err
	}
	writeJSON(w, 200, result)
	return nil
}
func (s *Server) startRepositoryImport(w http.ResponseWriter, r *http.Request) error {
	if s.RepositoryImports == nil {
		return badRequest("Repository imports are unavailable on this server")
	}
	var in struct {
		repositoryimport.Input
		Confirmed bool `json:"confirmed"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16384)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return badRequest("invalid import request")
	}
	if dec.Decode(&struct{}{}) != io.EOF || !in.Confirmed {
		return badRequest("Confirm repository download into a managed directory")
	}
	result, err := s.RepositoryImports.Start(r.Context(), in.Input, identityFrom(r.Context()).Name)
	if errors.Is(err, repositoryimport.ErrInvalid) || errors.Is(err, projectconfig.ErrInvalid) {
		return badRequest("%s", err.Error())
	}
	if err != nil {
		return err
	}
	writeJSON(w, 202, result)
	return nil
}
func (s *Server) cancelRepositoryImport(w http.ResponseWriter, r *http.Request) error {
	if s.RepositoryImports == nil {
		return badRequest("Repository imports are unavailable on this server")
	}
	rev, err := ifMatchRevision(r)
	if err != nil {
		return err
	}
	if rev < 1 {
		return badRequest("If-Match is required")
	}
	result, err := s.RepositoryImports.Cancel(r.Context(), r.PathValue("iid"), rev, identityFrom(r.Context()).Name)
	if err != nil {
		return err
	}
	writeJSON(w, 202, result)
	return nil
}
