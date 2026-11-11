package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"github.com/woliveiras/weft/server/internal/delegation"
	"github.com/woliveiras/weft/server/internal/domain"
)

func delegationDecode(w http.ResponseWriter, r *http.Request, out any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(out); e != nil {
		return badRequest("invalid delegation configuration JSON")
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return badRequest("expected one JSON object")
	}
	return nil
}
func delegationRevision(r *http.Request) (int64, error) {
	if r.Header.Get("If-Match") == "" {
		return 0, badRequest("If-Match is required, including revision 0 for defaults")
	}
	if r.Header.Get("If-Match") == `"0"` || r.Header.Get("If-Match") == "0" {
		return 0, nil
	}
	return ifMatchRevision(r)
}
func delegationETag(w http.ResponseWriter, rev int64) {
	w.Header().Set("ETag", strconv.Quote(strconv.FormatInt(rev, 10)))
}
func (s *Server) delegationPolicy(w http.ResponseWriter, r *http.Request) error {
	m := delegation.Manager{Store: s.Store}
	if r.Method == http.MethodGet {
		v, e := m.Policy(r.Context())
		if e != nil {
			return e
		}
		delegationETag(w, v.Revision)
		writeJSON(w, 200, v)
		return nil
	}
	rev, e := delegationRevision(r)
	if e != nil {
		return e
	}
	var p delegation.Policy
	if e = delegationDecode(w, r, &p); e != nil {
		return e
	}
	if e = p.Validate(); e != nil {
		return badRequest("%s", e)
	}
	actor := identityFrom(r.Context()).Name
	v, e := m.SavePolicy(r.Context(), p, rev, actor)
	if e != nil {
		return e
	}
	s.Bus.Publish(r.Context(), domain.NewEvent("delegation.policy.updated", "", map[string]any{"revision": v.Revision, "approvalMode": p.ApprovalMode, "enabled": p.Enabled}).WithActor(actor))
	delegationETag(w, v.Revision)
	writeJSON(w, 200, v)
	return nil
}
func (s *Server) delegationProfiles(w http.ResponseWriter, r *http.Request) error {
	m := delegation.Manager{Store: s.Store}
	if r.Method == http.MethodGet {
		if id := r.PathValue("profile"); id != "" {
			v, e := m.Profile(r.Context(), id)
			if e != nil {
				return e
			}
			delegationETag(w, v.Revision)
			writeJSON(w, 200, v)
			return nil
		}
		v, e := m.Profiles(r.Context())
		if e != nil {
			return e
		}
		writeJSON(w, 200, v)
		return nil
	}
	rev := int64(0)
	var e error
	if r.Method == http.MethodPut {
		rev, e = delegationRevision(r)
		if e != nil {
			return e
		}
	}
	if r.Method == http.MethodPut {
		if _, e = m.Profile(r.Context(), r.PathValue("profile")); e != nil {
			return e
		}
	}
	var p delegation.Profile
	if e = delegationDecode(w, r, &p); e != nil {
		return e
	}
	if e = p.Validate(); e != nil {
		return badRequest("%s", e)
	}
	if r.Method == http.MethodPut && p.ID != r.PathValue("profile") {
		return badRequest("profile id must match URL")
	}
	if r.Method == http.MethodPost && p.ID == "analysis" {
		return badRequest("update the built-in analysis profile with PUT")
	}
	actor := identityFrom(r.Context()).Name
	v, e := m.SaveProfile(r.Context(), p, rev, actor)
	if e != nil {
		return e
	}
	s.Bus.Publish(r.Context(), domain.NewEvent("delegation.profile.updated", "", map[string]any{"profileId": p.ID, "revision": v.Revision, "enabled": p.Enabled}).WithActor(actor))
	delegationETag(w, v.Revision)
	status := 200
	if r.Method == http.MethodPost {
		status = 201
	}
	writeJSON(w, status, v)
	return nil
}
