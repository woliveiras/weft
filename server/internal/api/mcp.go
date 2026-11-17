package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/woliveiras/weft/server/internal/mcpconfig"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/tools"
)

func (s *Server) mcpList(w http.ResponseWriter, r *http.Request) error {
	if s.MCP == nil {
		return storage.ErrNotFound
	}
	v, e := s.MCP.List(r.Context())
	if e != nil {
		return e
	}
	writeJSON(w, 200, v)
	return nil
}
func (s *Server) mcpGet(w http.ResponseWriter, r *http.Request) error {
	if s.MCP == nil {
		return storage.ErrNotFound
	}
	v, e := s.MCP.Get(r.Context(), r.PathValue("connection"))
	if e != nil {
		return e
	}
	writeJSON(w, 200, v)
	return nil
}
func (s *Server) mcpPut(w http.ResponseWriter, r *http.Request) error {
	if s.MCP == nil {
		return storage.ErrNotFound
	}
	var in mcpconfig.Input
	d := json.NewDecoder(io.LimitReader(r.Body, 32769))
	d.DisallowUnknownFields()
	if d.Decode(&in) != nil {
		return badRequest("invalid MCP connection body")
	}
	if d.Decode(new(any)) != io.EOF {
		return badRequest("invalid MCP connection body")
	}
	rev := int64(0)
	status := http.StatusCreated
	if r.Method == http.MethodPut {
		var e error
		rev, e = ifMatchRevision(r)
		if e != nil {
			return e
		}
		if rev == 0 {
			return badRequest("If-Match revision required")
		}
		if in.ID != r.PathValue("connection") {
			return badRequest("connection id cannot change")
		}
		status = 200
	}
	v, e := s.MCP.Put(r.Context(), in, rev, identityFrom(r.Context()).Name)
	if e != nil {
		return e
	}
	writeJSON(w, status, v)
	return nil
}
func (s *Server) mcpRefresh(w http.ResponseWriter, r *http.Request) error {
	if s.MCP == nil {
		return storage.ErrNotFound
	}
	v, e := s.MCP.Refresh(r.Context(), r.PathValue("connection"), identityFrom(r.Context()).Name)
	if e != nil {
		return e
	}
	writeJSON(w, 200, v)
	return nil
}

func (s *Server) mcpTools(w http.ResponseWriter, r *http.Request) error {
	if s.MCP == nil {
		return storage.ErrNotFound
	}
	views, e := s.MCP.List(r.Context())
	if e != nil {
		return e
	}
	out := []tools.ToolInfo{}
	for _, v := range views {
		if !v.Enabled || v.Status != "ready" {
			continue
		}
		for _, t := range v.Tools {
			out = append(out, tools.ToolInfo{Name: "mcp." + v.ID + "." + t.Name, Capability: "MCP tool", Description: t.Description, Schema: t.InputSchema})
		}
	}
	writeJSON(w, 200, out)
	return nil
}

func (s *Server) mcpTemplates(w http.ResponseWriter, r *http.Request) error {
	writeJSON(w, http.StatusOK, mcpconfig.Templates())
	return nil
}

func (s *Server) mcpDelete(w http.ResponseWriter, r *http.Request) error {
	if s.MCP == nil {
		return storage.ErrNotFound
	}
	revision, err := ifMatchRevision(r)
	if err != nil {
		return err
	}
	if revision <= 0 {
		return badRequest("If-Match revision required")
	}
	if err = s.MCP.Delete(r.Context(), r.PathValue("connection"), revision, identityFrom(r.Context()).Name); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) mcpHistory(w http.ResponseWriter, r *http.Request) error {
	if s.MCP == nil {
		return storage.ErrNotFound
	}
	q := r.URL.Query()
	for key, values := range q {
		switch key {
		case "connection", "graph", "node", "tool", "outcome", "type", "since", "until", "before", "limit":
		default:
			return badRequest("unknown MCP history filter")
		}
		if len(values) != 1 {
			return badRequest("duplicate MCP history filter")
		}
	}
	f := mcpconfig.HistoryFilter{Connection: q.Get("connection"), Graph: q.Get("graph"), Node: q.Get("node"), Tool: q.Get("tool"), Outcome: q.Get("outcome"), Type: q.Get("type"), Limit: 50}
	var err error
	if q.Has("before") {
		f.Before, err = strconv.ParseInt(q.Get("before"), 10, 64)
		if err != nil {
			return badRequest("invalid history cursor")
		}
	}
	if q.Has("limit") {
		f.Limit, err = strconv.Atoi(q.Get("limit"))
		if err != nil {
			return badRequest("invalid history limit")
		}
	}
	if q.Get("since") != "" {
		f.Since, err = time.Parse(time.RFC3339, q.Get("since"))
		if err != nil {
			return badRequest("invalid history start time")
		}
	}
	if q.Get("until") != "" {
		f.Until, err = time.Parse(time.RFC3339, q.Get("until"))
		if err != nil {
			return badRequest("invalid history end time")
		}
	}
	page, err := s.MCP.History(r.Context(), f)
	if err != nil {
		return err
	}
	writeJSON(w, 200, page)
	return nil
}
