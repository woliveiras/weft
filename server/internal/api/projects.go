package api

import (
	"encoding/json"
	"errors"
	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/projectconfig"
	"io"
	"net/http"
)

func (s *Server) projectRoutes() {
	s.mux.Handle("GET /api/v1/repository-imports", s.wrap(route{"GET /api/v1/repository-imports", config.RoleViewer, s.listRepositoryImports}))
	s.mux.Handle("POST /api/v1/repository-imports", s.wrap(route{"POST /api/v1/repository-imports", config.RoleAdmin, s.startRepositoryImport}))
	s.mux.Handle("GET /api/v1/repository-imports/{iid}", s.wrap(route{"GET /api/v1/repository-imports/{iid}", config.RoleViewer, s.getRepositoryImport}))
	s.mux.Handle("POST /api/v1/repository-imports/{iid}/cancel", s.wrap(route{"POST /api/v1/repository-imports/{iid}/cancel", config.RoleAdmin, s.cancelRepositoryImport}))
	s.mux.Handle("PUT /api/v1/projects/{pid}/preferences", s.wrap(route{"PUT /api/v1/projects/{pid}/preferences", config.RoleAdmin, s.projectPreferences}))
	s.mux.Handle("PUT /api/v1/projects/{pid}/checks", s.wrap(route{"PUT /api/v1/projects/{pid}/checks", config.RoleAdmin, s.projectChecks}))
	s.mux.Handle("GET /api/v1/projects", s.wrap(route{"GET /api/v1/projects", config.RoleViewer, s.listProjects}))
	s.mux.Handle("POST /api/v1/projects", s.wrap(route{"POST /api/v1/projects", config.RoleAdmin, s.createProject}))
}
func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) error {
	ps, err := (projectconfig.Manager{Store: s.Store}).List(r.Context())
	if err != nil {
		return err
	}
	writeJSON(w, 200, ps)
	return nil
}
func (s *Server) createProject(w http.ResponseWriter, r *http.Request) error {
	var in projectconfig.Input
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return badRequest("invalid project JSON")
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		return badRequest("expected one JSON object")
	}
	p, err := (projectconfig.Manager{Store: s.Store}).Create(r.Context(), in, identityFrom(r.Context()).Name)
	if errors.Is(err, projectconfig.ErrInvalid) {
		return badRequest("%s", err.Error())
	}
	if err != nil {
		return err
	}
	writeJSON(w, 201, p)
	return nil
}

func (s *Server) projectPreferences(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		ExecutionMode string `json:"executionMode"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return badRequest("invalid preference JSON")
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		return badRequest("expected one JSON object")
	}
	if in.ExecutionMode == "local" && !s.Config.NativeProfile {
		return badRequest("local preference requires native Weft")
	}
	revision, err := ifMatchRevision(r)
	if err != nil {
		return err
	}
	if revision < 1 {
		return badRequest("If-Match is required")
	}
	p, err := (projectconfig.Manager{Store: s.Store}).SetExecutionPreference(r.Context(), r.PathValue("pid"), in.ExecutionMode, revision, identityFrom(r.Context()).Name)
	if errors.Is(err, projectconfig.ErrInvalid) {
		return badRequest("invalid execution preference")
	}
	if err != nil {
		return err
	}
	writeJSON(w, 200, p)
	return nil
}

// projectChecks replaces the project's declared checks (test, lint, build,
// custom). Running one still needs the agent's shell.exec grant.
func (s *Server) projectChecks(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Checks []domain.Check `json:"checks"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return badRequest("invalid checks JSON")
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		return badRequest("expected one JSON object")
	}
	revision, err := ifMatchRevision(r)
	if err != nil {
		return err
	}
	if revision < 1 {
		return badRequest("If-Match is required")
	}
	p, err := (projectconfig.Manager{Store: s.Store}).SetChecks(r.Context(), r.PathValue("pid"), in.Checks, revision, identityFrom(r.Context()).Name)
	if errors.Is(err, projectconfig.ErrInvalid) {
		return badRequest("%s", err.Error())
	}
	if err != nil {
		return err
	}
	writeJSON(w, 200, p)
	return nil
}
