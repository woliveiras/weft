package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"github.com/woliveiras/weft/server/internal/jiracloud"
	"github.com/woliveiras/weft/server/internal/storage"
)

func (s *Server) cloudProfiles() (*jiracloud.Profiles, error) {
	if s.CloudProfiles == nil {
		return nil, jiracloud.ErrUnavailable
	}
	return s.CloudProfiles, nil
}
func decodeCloudProfile(r *http.Request) (jiracloud.Input, error) {
	var in jiracloud.Input
	raw, err := io.ReadAll(io.LimitReader(r.Body, 4097))
	if err != nil || len(raw) > 4096 {
		return in, badRequest("invalid or oversized Jira Cloud profile")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	var extra any
	if d.Decode(&in) != nil || d.Decode(&extra) != io.EOF {
		return in, badRequest("provide one known Jira Cloud workflow profile object")
	}
	return in, nil
}
func (s *Server) listCloudProfiles(w http.ResponseWriter, r *http.Request) error {
	m, err := s.cloudProfiles()
	if err != nil {
		return err
	}
	out, err := m.List(r.Context())
	if err != nil {
		return err
	}
	writeJSON(w, 200, out)
	return nil
}
func (s *Server) getCloudProfile(w http.ResponseWriter, r *http.Request) error {
	m, err := s.cloudProfiles()
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
func (s *Server) previewCloudProfile(w http.ResponseWriter, r *http.Request) error {
	m, err := s.cloudProfiles()
	if err != nil {
		return err
	}
	in, err := decodeCloudProfile(r)
	if err != nil {
		return err
	}
	out, err := m.Preview(r.Context(), in)
	if err != nil {
		return err
	}
	writeJSON(w, 200, out)
	return nil
}
func (s *Server) saveCloudProfile(w http.ResponseWriter, r *http.Request) error {
	m, err := s.cloudProfiles()
	if err != nil {
		return err
	}
	var rev int64
	if r.Method == http.MethodPut {
		rev, err = issueConnectionRevision(r)
		if err != nil {
			return err
		}
	}
	in, err := decodeCloudProfile(r)
	if err != nil {
		return err
	}
	if rev > 0 && in.Name != r.PathValue("name") {
		return badRequest("profile name must match its URL")
	}
	out, err := m.Put(r.Context(), in, rev, identityFrom(r.Context()).Name)
	if err != nil {
		return err
	}
	code := 201
	if rev > 0 {
		code = 200
	}
	writeJSON(w, code, out)
	return nil
}
func (s *Server) discoverCloudTransitions(w http.ResponseWriter, r *http.Request) error {
	m, err := s.cloudProfiles()
	if err != nil {
		return err
	}
	rev, err := issueConnectionRevision(r)
	if err != nil {
		return err
	}
	key, err := decodeTransitionIssue(r)
	if err != nil {
		return err
	}
	name := r.PathValue("name")
	client, err := m.Open(r.Context(), name, rev, false, nil)
	if err != nil {
		return err
	}
	discovery, remoteErr := client.DiscoverTransitions(r.Context(), key)
	// Revalidate the profile and its REST credential audience/revision after all reads.
	if _, err = m.Open(r.Context(), name, rev, false, nil); err != nil {
		return err
	}
	current, err := m.Get(r.Context(), name)
	if err != nil {
		return err
	}
	if current.Revision != rev || !current.Enabled {
		return storage.ErrConflict
	}
	if remoteErr != nil {
		writeErr(w, 502, "tracker_discovery", "Jira Cloud transition discovery failed. Review the project, issue and account access.", nil)
		return nil
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"connection": name, "revision": rev, "discovery": discovery})
	return nil
}
