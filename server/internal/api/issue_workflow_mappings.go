package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/woliveiras/weft/server/internal/jiradc"
)

func decodeWorkflowMapping(r *http.Request) (jiradc.MappingInput, error) {
	var in jiradc.MappingInput
	raw, err := io.ReadAll(io.LimitReader(r.Body, 8193))
	if err != nil || len(raw) > 8192 {
		return in, badRequest("invalid or oversized Jira workflow mapping body")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var extra any
	if dec.Decode(&in) != nil || dec.Decode(&extra) != io.EOF {
		return in, badRequest("provide one known Jira workflow mapping object")
	}
	return in, nil
}
func (s *Server) workflowMappings(r *http.Request) (jiradc.WorkflowMappings, error) {
	if strings.HasPrefix(r.URL.Path, "/api/v1/jira-cloud-workflow-mappings") {
		profiles, err := s.cloudProfiles()
		if err != nil {
			return jiradc.WorkflowMappings{}, err
		}
		return jiradc.WorkflowMappings{Backend: profiles.Backend()}, nil
	}
	return jiradc.WorkflowMappings{Connections: s.IssueConnections}, nil
}
func (s *Server) issueWorkflowMappings(w http.ResponseWriter, r *http.Request) error {
	m, err := s.workflowMappings(r)
	if err != nil {
		return err
	}
	rows, err := m.List(r.Context())
	if err != nil {
		return err
	}
	if connection := r.URL.Query().Get("connection"); connection != "" {
		filtered := make([]jiradc.WorkflowMapping, 0)
		for _, v := range rows {
			if v.Connection == connection {
				filtered = append(filtered, v)
			}
		}
		rows = filtered
	}
	writeJSON(w, 200, rows)
	return nil
}
func (s *Server) getIssueWorkflowMapping(w http.ResponseWriter, r *http.Request) error {
	m, err := s.workflowMappings(r)
	if err != nil {
		return err
	}
	out, err := m.Get(r.Context(), r.PathValue("name"))
	if err != nil {
		return err
	}
	w.Header().Set("ETag", strconv.Quote(strconv.FormatInt(out.Revision, 10)))
	writeJSON(w, 200, out)
	return nil
}
func (s *Server) createIssueWorkflowMapping(w http.ResponseWriter, r *http.Request) error {
	return s.saveIssueWorkflowMapping(w, r, false)
}
func (s *Server) updateIssueWorkflowMapping(w http.ResponseWriter, r *http.Request) error {
	return s.saveIssueWorkflowMapping(w, r, true)
}
func (s *Server) saveIssueWorkflowMapping(w http.ResponseWriter, r *http.Request, update bool) error {
	var revision int64
	var err error
	if update {
		revision, err = issueConnectionRevision(r)
		if err != nil {
			return err
		}
	}
	in, err := decodeWorkflowMapping(r)
	if err != nil {
		return err
	}
	if update {
		in.Name = r.PathValue("name")
	}
	m, err := s.workflowMappings(r)
	if err != nil {
		return err
	}
	out, err := m.Put(r.Context(), in, revision, identityFrom(r.Context()).Name)
	if errors.Is(err, jiradc.ErrMappingDiscovery) {
		writeErr(w, http.StatusBadGateway, "tracker_mapping_discovery", err.Error(), nil)
		return nil
	}
	if err != nil {
		return err
	}
	status := 201
	if update {
		status = 200
	}
	writeJSON(w, status, out)
	return nil
}
