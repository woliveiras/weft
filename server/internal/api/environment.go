package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/engine"
	"github.com/woliveiras/weft/server/internal/environment"
	"github.com/woliveiras/weft/server/internal/graph"
)

// Project environments (spec.environment): state of the build for the
// latest saved graph version, explicit builds and resets. The definition
// itself is part of the graph document and saved with PUT /graphs/{id}.

func (s *Server) getEnvironment(w http.ResponseWriter, r *http.Request) error {
	if s.Environments == nil {
		return engine.ErrNotFound
	}
	g, _, err := s.Engine.GetGraph(r.Context(), r.PathValue("id"), 0)
	if err != nil {
		return err
	}
	v, err := s.Environments.Get(r.Context(), g)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, v)
	return nil
}

type buildRequest struct {
	// Force rebuilds even when the environment is up to date.
	Force bool `json:"force"`
}

func (s *Server) buildEnvironment(w http.ResponseWriter, r *http.Request) error {
	if s.Environments == nil {
		return engine.ErrNotFound
	}
	var req buildRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		return badRequest("invalid JSON: %v", err)
	}
	g, _, err := s.Engine.GetGraph(r.Context(), r.PathValue("id"), 0)
	if err != nil {
		return err
	}
	v, err := s.Environments.BuildSaved(r.Context(), g, req.Force, identityFrom(r.Context()).Name)
	if err != nil {
		return err
	}
	// 202 while a build runs in the background; 200 when the environment
	// was already up to date and nothing was started.
	code := http.StatusOK
	if v.State == environment.StateBuilding {
		code = http.StatusAccepted
	}
	writeJSON(w, code, v)
	return nil
}

func (s *Server) resetEnvironment(w http.ResponseWriter, r *http.Request) error {
	if s.Environments == nil {
		return engine.ErrNotFound
	}
	id := r.PathValue("id")
	g, rec, err := s.Engine.GetGraph(r.Context(), id, 0)
	if err != nil {
		return err
	}
	// Running agents use the image and cache volumes being removed.
	if rec.State != domain.GraphStopped && rec.State != domain.GraphFailed {
		return fmt.Errorf("%w: graph %s is %s; stop it before resetting its environment", engine.ErrConflict, id, rec.State)
	}
	if err := s.Environments.Reset(r.Context(), id, identityFrom(r.Context()).Name); err != nil {
		return err
	}
	v, err := s.Environments.Get(r.Context(), g)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, v)
	return nil
}

// Discovery reads only an existing graph checkout; it does not clone or build.
func (s *Server) discoverEnvironment(w http.ResponseWriter, r *http.Request) error {
	g, _, err := s.Engine.GetGraph(r.Context(), r.PathValue("id"), 0)
	if err != nil {
		return err
	}
	if g.Spec.Environment != nil {
		writeJSON(w, http.StatusOK, environment.Discovery{Source: "interface", Status: "configured", Candidates: []environment.EnvironmentCandidate{}})
		return nil
	}
	if s.Workspaces == nil {
		return engine.ErrNotFound
	}
	dir, err := s.Workspaces.GraphDir(g)
	if err != nil {
		return badRequest("invalid workspace")
	}
	result, err := environment.Discover(r.Context(), dir)
	if err != nil {
		return badRequest("environment discovery unavailable: %v", err)
	}
	writeJSON(w, http.StatusOK, result)
	return nil
}

// resolveEnvironment validates a selected candidate for explicit review. Saving
// the returned definition uses the existing versioned graph write path.
func (s *Server) resolveEnvironment(w http.ResponseWriter, r *http.Request) error {
	g, _, err := s.Engine.GetGraph(r.Context(), r.PathValue("id"), 0)
	if err != nil {
		return err
	}
	var selection environment.Selection
	decoder := json.NewDecoder(io.LimitReader(r.Body, 4097))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&selection); err != nil {
		return badRequest("invalid environment selection")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return badRequest("expected one selection")
	}
	if s.Workspaces == nil {
		return engine.ErrNotFound
	}
	dir, err := s.Workspaces.GraphDir(g)
	if err != nil {
		return badRequest("invalid workspace")
	}
	env, err := environment.Resolve(r.Context(), dir, selection)
	if err != nil {
		return err
	}
	if s.Environments != nil {
		if err := s.Environments.ValidatePolicy(r.Context(), env); err != nil {
			return err
		}
	}
	writeJSON(w, http.StatusOK, env)
	return nil
}

func (s *Server) validateEnvironmentOrigin(ctx context.Context, g *domain.Graph) error {
	if s.Environments != nil {
		if err := s.Environments.ValidatePolicy(ctx, g.Spec.Environment); err != nil {
			return err
		}
	}
	if g.Spec.Environment == nil || g.Spec.Environment.Origin == nil {
		return nil
	}
	if s.Workspaces == nil {
		return engine.ErrNotFound
	}
	copy := *g
	if copy.Metadata.ID == "" {
		copy.Metadata.ID = graph.Slug(copy.Metadata.Name)
	}
	dir, err := s.Workspaces.GraphDir(&copy)
	if err != nil {
		return badRequest("invalid workspace")
	}
	if s.Environments != nil {
		return s.Environments.ValidateDefinition(ctx, g, dir)
	}
	if g.Spec.Environment.Origin.Source == "agent" {
		return badRequest("proposal approval validator is unavailable")
	}
	return environment.ValidateAccepted(ctx, dir, g.Spec.Environment)
}

func (s *Server) getEnvironmentProposal(w http.ResponseWriter, r *http.Request) error {
	if s.Proposals == nil {
		return engine.ErrNotFound
	}
	if _, _, err := s.Engine.GetGraph(r.Context(), r.PathValue("id"), 0); err != nil {
		return err
	}
	v, err := s.Proposals.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	writeJSON(w, 200, v)
	return nil
}
func (s *Server) proposeEnvironment(w http.ResponseWriter, r *http.Request) error {
	if s.Proposals == nil {
		return engine.ErrNotFound
	}
	var body struct {
		Model  domain.ModelRef `json:"model"`
		Resume bool            `json:"resume"`
	}
	d := json.NewDecoder(io.LimitReader(r.Body, 4097))
	d.DisallowUnknownFields()
	if err := d.Decode(&body); err != nil && err != io.EOF {
		return badRequest("invalid proposal request")
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return badRequest("expected one proposal request")
	}
	g, _, err := s.Engine.GetGraph(r.Context(), r.PathValue("id"), 0)
	if err != nil {
		return err
	}
	v, err := s.Proposals.Request(r.Context(), g, body.Model, body.Resume, identityFrom(r.Context()).Name)
	if err != nil {
		return badRequest("%v", err)
	}
	writeJSON(w, http.StatusCreated, v)
	return nil
}
func (s *Server) cancelEnvironmentProposal(w http.ResponseWriter, r *http.Request) error {
	if s.Proposals == nil {
		return engine.ErrNotFound
	}
	if err := s.Proposals.Cancel(r.Context(), r.PathValue("id"), identityFrom(r.Context()).Name); err != nil {
		return badRequest("%v", err)
	}
	return s.getEnvironmentProposal(w, r)
}
func (s *Server) retryEnvironmentProposal(w http.ResponseWriter, r *http.Request) error {
	if s.Proposals == nil {
		return engine.ErrNotFound
	}
	if err := s.Proposals.Apply(r.Context(), r.PathValue("id")); err != nil {
		return badRequest("%v", err)
	}
	return s.getEnvironmentProposal(w, r)
}

// Cleanup is explicit and admin-only. Omitted dryRun previews; deletion requires
// both dryRun=false and concrete generation IDs from a previous inspection.
func (s *Server) cleanupEnvironments(w http.ResponseWriter, r *http.Request) error {
	if s.Environments == nil {
		return engine.ErrNotFound
	}
	var body struct {
		DryRun         *bool    `json:"dryRun"`
		Generations    []string `json:"generations"`
		DeferredGraphs []string `json:"deferredGraphs"`
	}
	d := json.NewDecoder(io.LimitReader(r.Body, 16385))
	d.DisallowUnknownFields()
	if err := d.Decode(&body); err != nil && err != io.EOF {
		return badRequest("invalid cleanup request")
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return badRequest("expected one cleanup request")
	}
	dryRun := body.DryRun == nil || *body.DryRun
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	if len(body.DeferredGraphs) != 0 && (dryRun || len(body.Generations) != 0) {
		return badRequest("deferredGraphs requires dryRun=false and cannot be combined with generations")
	}
	if len(body.DeferredGraphs) > 0 {
		return s.cleanupDeferredGraphs(ctx, w, body.DeferredGraphs)
	}
	result, err := s.Environments.CleanupShared(ctx, body.Generations, dryRun, identityFrom(r.Context()).Name)
	if errors.Is(err, environment.ErrInvalidCleanup) {
		return badRequest("%v", err)
	}
	if err != nil {
		return err
	}
	if dryRun && len(body.Generations) == 0 {
		result.DeferredGraphs, err = s.Environments.ListDeferredCleanup(ctx)
		if err != nil {
			return err
		}
	}
	writeJSON(w, http.StatusOK, result)
	return nil
}

func (s *Server) environmentProxies(w http.ResponseWriter, r *http.Request) error {
	if _, _, err := s.Engine.GetGraph(r.Context(), r.PathValue("id"), 0); err != nil {
		return err
	}
	if s.Environments == nil {
		return engine.ErrNotFound
	}
	result, err := s.Environments.ProxyStatus(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, result)
	return nil
}

func (s *Server) environmentBlockedHistory(w http.ResponseWriter, r *http.Request) error {
	if _, _, err := s.Engine.GetGraph(r.Context(), r.PathValue("id"), 0); err != nil {
		return err
	}
	if s.Environments == nil {
		return engine.ErrNotFound
	}
	result, err := s.Environments.BlockedHistory(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, result)
	return nil
}

// Each request selects one graph so a failed retry has an unambiguous result.
func (s *Server) cleanupDeferredGraphs(ctx context.Context, w http.ResponseWriter, ids []string) error {
	if len(ids) != 1 {
		return badRequest("select one deferred graph per cleanup request")
	}
	id := ids[0]
	if err := s.Engine.WithGraphTransition(ctx, id, func(ctx context.Context) error {
		return s.Environments.RetryDeferredCleanup(ctx, id, identityFrom(ctx).Name)
	}); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
