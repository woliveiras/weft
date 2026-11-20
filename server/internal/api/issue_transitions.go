package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	"github.com/woliveiras/weft/server/internal/jiradc"
	"github.com/woliveiras/weft/server/internal/storage"
)

func decodeTransitionIssue(r *http.Request) (string, error) {
	var in struct {
		IssueKey string `json:"issueKey"`
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1025))
	if err != nil || len(raw) > 1024 {
		return "", badRequest("transition discovery input exceeds its bound or could not be read")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var extra any
	if dec.Decode(&in) != nil || dec.Decode(&extra) != io.EOF || jiradc.ValidateIssueKey(in.IssueKey) != nil {
		return "", badRequest("provide one Jira issue key in the issueKey field")
	}
	return in.IssueKey, nil
}

// Discovery reads the actual issue and its current transition metadata. It does
// not create a mapping, grant, consent, or remote mutation.
func (s *Server) discoverIssueTransitions(w http.ResponseWriter, r *http.Request) error {
	rev, err := issueConnectionRevision(r)
	if err != nil {
		return err
	}
	key, err := decodeTransitionIssue(r)
	if err != nil {
		return err
	}
	name := r.PathValue("name")
	client, err := s.IssueConnections.Open(r.Context(), name, rev)
	if err != nil {
		return err
	}
	discovery, remoteErr := client.DiscoverTransitions(r.Context(), key)
	// Do not present metadata or errors from an obsolete connection as current.
	current, err := s.IssueConnections.Get(r.Context(), name)
	if err != nil {
		return err
	}
	if current.Revision != rev || !current.Enabled {
		return storage.ErrConflict
	}
	if remoteErr != nil {
		writeErr(w, http.StatusBadGateway, "tracker_discovery", remoteErr.Error(), nil)
		return nil
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"connection": name, "revision": rev, "discovery": discovery})
	return nil
}
