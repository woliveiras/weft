package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/engine"
	"github.com/woliveiras/weft/server/internal/graph"
	"github.com/woliveiras/weft/server/internal/instructions"
	"github.com/woliveiras/weft/server/internal/storage"
)

func (s *Server) skills(w http.ResponseWriter, r *http.Request) error {
	v, err := (instructions.Manager{Store: s.Store}).List(r.Context())
	if err != nil {
		return err
	}
	writeJSON(w, 200, v)
	return nil
}
func (s *Server) skill(w http.ResponseWriter, r *http.Request) error {
	v, err := (instructions.Manager{Store: s.Store}).Get(r.Context(), r.PathValue("sid"))
	if err != nil {
		return err
	}
	w.Header().Set("ETag", strconv.Quote(strconv.FormatInt(v.Revision, 10)))
	writeJSON(w, 200, v)
	return nil
}
func instructionInvalid(message string) error {
	return &engine.ValidationError{Report: &graph.Report{Errors: []graph.Issue{{Path: "skill", Message: message}}}}
}
func (s *Server) saveSkill(w http.ResponseWriter, r *http.Request) error {
	m := instructions.Manager{Store: s.Store}
	if instructions.IsBuiltin(r.PathValue("sid")) {
		return instructionInvalid(instructions.ErrBuiltinReadOnly.Error())
	}
	var in instructions.Input
	rev := int64(0)
	deleted := r.Method == http.MethodDelete
	if r.Method != http.MethodPost {
		var err error
		rev, err = ifMatchRevision(r)
		if err != nil {
			return err
		}
		if rev == 0 {
			return badRequest("If-Match is required")
		}
		old, err := m.Get(r.Context(), r.PathValue("sid"))
		if err != nil {
			return err
		}
		in = old.Input
	}
	if !deleted {
		r.Body = http.MaxBytesReader(w, r.Body, 128*1024)
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		in = instructions.Input{}
		if err := dec.Decode(&in); err != nil {
			return badRequest("invalid skill JSON")
		}
		var extra any
		if err := dec.Decode(&extra); err != io.EOF {
			return badRequest("expected one JSON object")
		}
		if r.Method != http.MethodPost && in.ID != r.PathValue("sid") {
			return instructionInvalid("id must match URL")
		}
		if instructions.IsBuiltin(in.ID) {
			return instructionInvalid(instructions.ErrBuiltinReadOnly.Error())
		}
		if err := instructions.Validate(in); err != nil {
			return instructionInvalid(err.Error())
		}
	}
	if deleted {
		// Conservative reference guard: remove associations from every latest graph
		// before deletion. Already-running old versions retain immutable snapshots.
		gs, err := s.Store.ListGraphs(r.Context())
		if err != nil {
			return err
		}
		records, err := s.Store.ListGraphRecords(r.Context())
		if err != nil {
			return err
		}
		for _, rec := range records {
			if rec.State == domain.GraphRunning || rec.State == domain.GraphPaused || rec.State == domain.GraphStopping {
				active, err := s.Store.GetGraph(r.Context(), rec.ID, rec.Version)
				if err != nil {
					return err
				}
				gs = append(gs, active)
			}
		}
		for _, g := range gs {
			if in.Global && g.Spec.Instructions.InstallationDefaults {
				return storage.ErrConflict
			}
			for _, id := range g.Spec.Instructions.SkillIDs {
				if id == in.ID {
					return storage.ErrConflict
				}
			}
		}
	}
	v, err := m.Save(r.Context(), in, rev, identityFrom(r.Context()).Name, deleted)
	if err != nil {
		return err
	}
	s.Bus.Publish(r.Context(), domain.NewEvent("instruction.skill.updated", "", map[string]any{"skill_id": v.ID, "revision": v.Revision, "deleted": deleted}).WithActor(v.Actor))
	w.Header().Set("ETag", strconv.Quote(strconv.FormatInt(v.Revision, 10)))
	status := 200
	if r.Method == http.MethodPost {
		status = 201
	}
	writeJSON(w, status, v)
	return nil
}
func (s *Server) inspectInstructions(w http.ResponseWriter, r *http.Request) error {
	g, err := s.Store.GetGraph(r.Context(), r.PathValue("id"), 0)
	if err != nil {
		return err
	}
	n := g.Node(r.URL.Query().Get("node"))
	if n == nil {
		return badRequest("a valid node is required")
	}
	root, err := s.Workspaces.GraphDir(g)
	if err != nil {
		return err
	}
	if n.Workspace.Mode == domain.WorkspaceWorktree {
		replica := 0
		if value := r.URL.Query().Get("replica"); value != "" {
			replica, err = strconv.Atoi(value)
			if err != nil || replica < 0 {
				return badRequest("replica must be a nonnegative integer")
			}
		}
		root = filepath.Join(s.Workspaces.Root, ".worktrees", g.Metadata.ID, fmt.Sprintf("%s-%d", n.ID, replica))
	}
	v, err := (instructions.Manager{Store: s.Store}).Resolve(r.Context(), g, n, root, graph.Substitute(n.Prompt, graph.Params(g)), nil)
	if errors.Is(err, instructions.ErrRequiredBudget) {
		return instructionInvalid(err.Error())
	}
	if err != nil {
		return err
	}
	writeJSON(w, 200, v)
	return nil
}
func (s *Server) executionInstructions(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("eid")
	if _, err := s.Store.GetExecution(r.Context(), id); err != nil {
		return err
	}
	v, err := (instructions.Manager{Store: s.Store}).Snapshot(r.Context(), id)
	if err != nil {
		return err
	}
	writeJSON(w, 200, v)
	return nil
}
