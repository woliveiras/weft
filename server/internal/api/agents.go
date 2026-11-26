package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/woliveiras/weft/server/internal/agentcatalog"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/engine"
	"github.com/woliveiras/weft/server/internal/mcpconfig"
	"github.com/woliveiras/weft/server/internal/taskrun"
)

func (s *Server) agentCatalog(w http.ResponseWriter, r *http.Request) error {
	m := agentcatalog.Manager{Store: s.Store}
	id := r.PathValue("agent")
	if r.Method == http.MethodGet {
		if id == "" {
			items, err := m.List(r.Context())
			if err != nil {
				return err
			}
			writeJSON(w, http.StatusOK, items)
			return nil
		}
		agent, err := m.Get(r.Context(), id)
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, agent)
		return nil
	}

	r.Body = http.MaxBytesReader(w, r.Body, 256<<10)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var input struct {
		ID         string                 `json:"id"`
		Definition domain.AgentDefinition `json:"definition"`
	}
	if err := dec.Decode(&input); err != nil || dec.Decode(new(any)) != io.EOF {
		return badRequest("expected a single agent definition")
	}
	expected := int64(0)
	status := http.StatusCreated
	if id != "" {
		if input.ID != "" && input.ID != id {
			return badRequest("agent ID does not match the URL")
		}
		var err error
		expected, err = ifMatchRevision(r)
		if err != nil {
			return err
		}
		if expected == 0 {
			return badRequest("If-Match is required to publish an agent version")
		}
		status = http.StatusOK
	} else {
		id = input.ID
	}
	if err := s.validateAgentDefinition(r.Context(), &input.Definition); err != nil {
		return err
	}
	actor := identityFrom(r.Context()).Name
	agent, err := m.Publish(r.Context(), id, expected, input.Definition, actor)
	if errors.Is(err, agentcatalog.ErrInvalid) {
		return badRequest("%s", err)
	}
	if err != nil {
		return err
	}
	s.auditAgentPublication(r.Context(), agent, actor)
	writeJSON(w, status, agent)
	return nil
}

func (s *Server) auditAgentPublication(ctx context.Context, agent agentcatalog.Agent, actor string) {
	s.Bus.Publish(ctx, domain.NewEvent(domain.EvAgentVersionPublished, "", map[string]any{
		"agentId": agent.ID, "version": agent.Versions[len(agent.Versions)-1].Version,
	}).WithActor(actor))
}

func (s *Server) validateAgentDefinition(ctx context.Context, d *domain.AgentDefinition) error {
	n := domain.Node{ID: "agent"}
	agentcatalog.ApplyDefinition(&n, *d)
	g := &domain.Graph{Metadata: domain.GraphMetadata{ID: "agent-definition"}, Spec: domain.GraphSpec{Nodes: []domain.Node{n}}}
	for _, param := range d.Parameters {
		if len(param.Default) > 16<<10 {
			return badRequest("parameter %q default exceeds 16 KiB", param.Name)
		}
		g.Spec.Parameters = append(g.Spec.Parameters, domain.Parameter{Name: param.Name, Value: param.Default})
	}
	report := s.Engine.Validate(g)
	if !report.Valid {
		return &engine.ValidationError{Report: report}
	}
	if s.MCP != nil {
		if err := s.MCP.ValidateGrants(ctx, g); err != nil {
			if errors.Is(err, mcpconfig.ErrInvalid) {
				return badRequest("%s", err)
			}
			return err
		}
	}
	d.Runtime.Kind = g.Spec.Nodes[0].Runtime.Kind
	d.Isolation.Mode = g.Spec.Nodes[0].Isolation.Mode
	return nil
}

func (s *Server) agentSource(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	version, err := strconv.Atoi(q.Get("version"))
	if err != nil || version <= 0 || q.Get("workflow") == "" || q.Get("node") == "" {
		return badRequest("workflow, version and node are required")
	}
	if strings.HasPrefix(q.Get("workflow"), taskrun.GraphPrefix) {
		return engine.ErrNotFound
	}
	g, _, err := s.Engine.GetGraph(r.Context(), q.Get("workflow"), version)
	if err != nil {
		return err
	}
	n := g.Node(q.Get("node"))
	if n == nil || n.Agent != nil || g.Metadata.Labels["weft.task"] != "" {
		return engine.ErrNotFound
	}
	definition := agentcatalog.FromNode(g, n)
	writeJSON(w, http.StatusOK, definition)
	return nil
}
