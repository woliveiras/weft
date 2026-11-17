package api

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/environmentpolicy"
)

func (s *Server) networkSettings() environmentpolicy.Settings {
	return environmentpolicy.Settings{Store: s.Store, Base: s.Config.Isolation.Docker.EnvironmentPolicy, DefaultRun: s.Config.Isolation.Docker.Network}
}
func (s *Server) networkPolicy(w http.ResponseWriter, r *http.Request) error {
	settings := s.networkSettings()
	v, err := settings.Get(r.Context())
	if err != nil {
		return err
	}
	result, err := settings.Preview(r.Context(), v.Network, v.Revision)
	if err != nil {
		return err
	}
	writeJSON(w, 200, result)
	return nil
}

type networkPolicyRequest struct {
	Revision *int64                            `json:"revision"`
	Network  environmentpolicy.NetworkSettings `json:"network"`
}

func decodeNetworkPolicy(r *http.Request) (networkPolicyRequest, error) {
	var in networkPolicyRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return in, badRequest("invalid network policy JSON")
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return in, badRequest("expected one JSON object")
	}
	if in.Revision == nil || *in.Revision < 0 {
		return in, badRequest("revision is required and must be nonnegative")
	}
	if err := in.Network.Validate(); err != nil {
		return in, badRequest("%s", err)
	}
	return in, nil
}
func (s *Server) previewNetworkPolicy(w http.ResponseWriter, r *http.Request) error {
	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	in, err := decodeNetworkPolicy(r)
	if err != nil {
		return err
	}
	v, err := s.networkSettings().Preview(r.Context(), in.Network, *in.Revision)
	if err != nil {
		return err
	}
	writeJSON(w, 200, v)
	return nil
}
func (s *Server) saveNetworkPolicy(w http.ResponseWriter, r *http.Request) error {
	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	in, err := decodeNetworkPolicy(r)
	if err != nil {
		return err
	}
	actor := identityFrom(r.Context()).Name
	v, err := s.networkSettings().Save(r.Context(), in.Network, *in.Revision, actor)
	if err != nil {
		return err
	}
	// No task is stopped automatically. Admission reads this durable revision;
	// running work must be stopped explicitly by the developer to revoke access.
	s.Bus.Publish(r.Context(), domain.NewEvent("installation.network_policy.updated", "", map[string]any{"revision": v.Revision}).WithActor(actor))
	s.Log.Info("installation network policy updated", "revision", v.Revision, "actor", actor)
	writeJSON(w, 200, v)
	return nil
}
