package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/woliveiras/weft/server/internal/llmconfig"
)

// Model provider management. Reading is open to viewers (nothing secret is
// ever returned); tests cost tokens, so they need an operator; changing
// providers and credentials needs an admin. Writes accept If-Match with the
// settings revision (GET /api/v1/provider-settings) for strict optimistic
// concurrency; without it each operation applies to the latest settings.

func ifMatchRevision(r *http.Request) (int64, error) {
	im := strings.Trim(r.Header.Get("If-Match"), `" W/`)
	if im == "" {
		return 0, nil
	}
	n, err := strconv.ParseInt(im, 10, 64)
	if err != nil || n <= 0 {
		return 0, badRequest("If-Match must be a settings revision number")
	}
	return n, nil
}

func decodeProviderInput(r *http.Request) (llmconfig.ProviderInput, error) {
	var in llmconfig.ProviderInput
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		// Never echo the body: it may hold a credential.
		var se *json.SyntaxError
		var te *json.UnmarshalTypeError
		switch {
		case errors.Is(err, io.EOF):
			return in, badRequest("a JSON body is required")
		case errors.As(err, &se), errors.As(err, &te):
			return in, badRequest("invalid JSON body")
		case strings.HasPrefix(err.Error(), "json: unknown field"):
			return in, badRequest("%s", err.Error())
		default:
			return in, badRequest("invalid JSON body")
		}
	}
	return in, nil
}

func (s *Server) writeProviderMeta(w http.ResponseWriter) {
	meta := s.Providers.Meta()
	w.Header().Set("ETag", strconv.Quote(strconv.FormatInt(meta.Revision, 10)))
}

func (s *Server) providers(w http.ResponseWriter, r *http.Request) error {
	s.writeProviderMeta(w)
	writeJSON(w, 200, s.Providers.List())
	return nil
}

func (s *Server) getProvider(w http.ResponseWriter, r *http.Request) error {
	v, err := s.Providers.Get(r.PathValue("name"))
	if err != nil {
		return err
	}
	s.writeProviderMeta(w)
	writeJSON(w, 200, v)
	return nil
}

func (s *Server) createProvider(w http.ResponseWriter, r *http.Request) error {
	rev, err := ifMatchRevision(r)
	if err != nil {
		return err
	}
	in, err := decodeProviderInput(r)
	if err != nil {
		return err
	}
	v, err := s.Providers.Create(r.Context(), in, identityFrom(r.Context()).Name, rev)
	if err != nil {
		return err
	}
	s.writeProviderMeta(w)
	writeJSON(w, http.StatusCreated, v)
	return nil
}

func (s *Server) updateProvider(w http.ResponseWriter, r *http.Request) error {
	rev, err := ifMatchRevision(r)
	if err != nil {
		return err
	}
	in, err := decodeProviderInput(r)
	if err != nil {
		return err
	}
	v, err := s.Providers.Update(r.Context(), r.PathValue("name"), in, identityFrom(r.Context()).Name, rev)
	if err != nil {
		return err
	}
	s.writeProviderMeta(w)
	writeJSON(w, 200, v)
	return nil
}

func (s *Server) deleteProvider(w http.ResponseWriter, r *http.Request) error {
	rev, err := ifMatchRevision(r)
	if err != nil {
		return err
	}
	if err := s.Providers.Delete(r.Context(), r.PathValue("name"), identityFrom(r.Context()).Name, rev); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) testProvider(w http.ResponseWriter, r *http.Request) error {
	res, err := s.Providers.Test(r.Context(), r.PathValue("name"), identityFrom(r.Context()).Name)
	if err != nil {
		return err
	}
	writeJSON(w, 200, res)
	return nil
}

func (s *Server) testProviderDraft(w http.ResponseWriter, r *http.Request) error {
	in, err := decodeProviderInput(r)
	if err != nil {
		return err
	}
	res, err := s.Providers.TestDraft(r.Context(), in, identityFrom(r.Context()).Name)
	if err != nil {
		return err
	}
	writeJSON(w, 200, res)
	return nil
}

func (s *Server) providerPlugins(w http.ResponseWriter, r *http.Request) error {
	s.writeProviderMeta(w)
	writeJSON(w, 200, s.Providers.Plugins())
	return nil
}

func (s *Server) updateProviderPlugin(w http.ResponseWriter, r *http.Request) error {
	rev, err := ifMatchRevision(r)
	if err != nil {
		return err
	}
	var body struct {
		Enabled *bool `json:"enabled"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil || body.Enabled == nil {
		return badRequest(`body must be {"enabled": true|false}`)
	}
	v, err := s.Providers.SetPluginEnabled(r.Context(), r.PathValue("id"), *body.Enabled, identityFrom(r.Context()).Name, rev)
	if err != nil {
		return err
	}
	s.writeProviderMeta(w)
	writeJSON(w, 200, v)
	return nil
}

func (s *Server) providerSettings(w http.ResponseWriter, r *http.Request) error {
	s.writeProviderMeta(w)
	writeJSON(w, 200, s.Providers.Meta())
	return nil
}

func (s *Server) updateProviderSettings(w http.ResponseWriter, r *http.Request) error {
	rev, err := ifMatchRevision(r)
	if err != nil {
		return err
	}
	var body struct {
		Default *string `json:"default"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Default == nil {
		return badRequest(`body must be {"default": "<provider name or empty for the configuration default>"}`)
	}
	meta, err := s.Providers.SetDefault(r.Context(), *body.Default, identityFrom(r.Context()).Name, rev)
	if err != nil {
		return err
	}
	s.writeProviderMeta(w)
	writeJSON(w, 200, meta)
	return nil
}
