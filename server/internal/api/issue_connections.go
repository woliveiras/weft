package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"github.com/woliveiras/weft/server/internal/jiradc"
)

func (s *Server) issueConnections(w http.ResponseWriter, r *http.Request) error {
	out, err := s.IssueConnections.List(r.Context())
	if err != nil {
		return err
	}
	writeJSON(w, 200, out)
	return nil
}
func (s *Server) getIssueConnection(w http.ResponseWriter, r *http.Request) error {
	out, err := s.IssueConnections.Get(r.Context(), r.PathValue("name"))
	if err != nil {
		return err
	}
	w.Header().Set("ETag", strconv.Quote(strconv.FormatInt(out.Revision, 10)))
	writeJSON(w, 200, out)
	return nil
}
func decodeIssueConnection(r *http.Request) (jiradc.ConnectionInput, error) {
	var in jiradc.ConnectionInput
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var extra any
	if dec.Decode(&in) != nil || dec.Decode(&extra) != io.EOF {
		return in, badRequest("invalid native tracker connection body")
	}
	return in, nil
}
func (s *Server) createIssueConnection(w http.ResponseWriter, r *http.Request) error {
	in, err := decodeIssueConnection(r)
	if err != nil {
		return err
	}
	out, err := s.IssueConnections.Put(r.Context(), in, 0, identityFrom(r.Context()).Name)
	if err != nil {
		return err
	}
	writeJSON(w, 201, out)
	return nil
}
func issueConnectionRevision(r *http.Request) (int64, error) {
	rev, err := ifMatchRevision(r)
	if err != nil {
		return 0, err
	}
	if rev < 1 {
		return 0, badRequest("a positive If-Match revision is required")
	}
	return rev, nil
}
func (s *Server) updateIssueConnection(w http.ResponseWriter, r *http.Request) error {
	rev, err := issueConnectionRevision(r)
	if err != nil {
		return err
	}
	in, err := decodeIssueConnection(r)
	if err != nil {
		return err
	}
	in.Name = r.PathValue("name")
	out, err := s.IssueConnections.Put(r.Context(), in, rev, identityFrom(r.Context()).Name)
	if err != nil {
		return err
	}
	writeJSON(w, 200, out)
	return nil
}
func (s *Server) testIssueConnection(w http.ResponseWriter, r *http.Request) error {
	rev, err := issueConnectionRevision(r)
	if err != nil {
		return err
	}
	client, err := s.IssueConnections.Open(r.Context(), r.PathValue("name"), rev)
	if err != nil {
		return err
	}
	identity, err := client.Identity(r.Context())
	if err != nil {
		writeJSON(w, 200, map[string]any{"ok": false, "error": err.Error()})
		return nil
	}
	writeJSON(w, 200, map[string]any{"ok": true, "identity": identity, "scope": "authenticated identity only; project permissions are checked for each issue"})
	return nil
}
