package mcpconfig

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/mail"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// JiraRESTConfig opts a connection into independent, cloud-bound REST access.
// Absence retains the legacy static credential behavior; a cleared explicit
// slot remains selected and can never fall back to that legacy behavior.
type JiraRESTConfig struct {
	CloudID string `json:"cloudId"`
}

type JiraRESTView struct {
	CloudID              string `json:"cloudId"`
	CredentialConfigured bool   `json:"credentialConfigured"`
}

var jiraRESTCloudID = regexp.MustCompile(`^[a-fA-F0-9]{8}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{12}$`)

func jiraRESTConnection(v View) bool {
	return (v.Service == "atlassian" || v.Service == "jira") &&
		canonicalMCPResource(v.Endpoint) == atlassianMCPEndpoint &&
		v.LocalProcess == nil && len(v.ArgumentPolicy) == 0 &&
		v.AtlassianPolicy == nil && len(v.MetadataHeaders) == 0
}

func validateJiraREST(in Input) error {
	if in.JiraREST != nil && !jiraRESTCloudID.MatchString(in.JiraREST.CloudID) {
		return fmt.Errorf("%w: jiraRest requires a valid Cloud ID", ErrInvalid)
	}
	if c := in.JiraRESTCredential; c != nil {
		if c.Clear {
			if c.Username != "" || c.Value != "" {
				return fmt.Errorf("%w: clear cannot replace a Jira REST credential", ErrInvalid)
			}
			return nil
		}
		address, err := mail.ParseAddress(c.Username)
		if err != nil || address.Address != c.Username || len(c.Username) > 320 ||
			strings.ContainsAny(c.Username, ":") || strings.ContainsFunc(c.Username, unicode.IsControl) {
			return fmt.Errorf("%w: Jira REST requires a plain Atlassian account email", ErrInvalid)
		}
		if strings.TrimSpace(c.Value) == "" || len(c.Value) > 16384 || strings.ContainsFunc(c.Value, unicode.IsControl) {
			return fmt.Errorf("%w: invalid Jira REST replacement token", ErrInvalid)
		}
	}
	return nil
}

// Resolve preservation before validating the destination. An omitted REST input
// cannot implicitly discard the mode or rebind its credential to another scope.
func jiraRESTState(r record, in Input) (*JiraRESTView, error) {
	var state *JiraRESTView
	if r.JiraREST != nil {
		copy := *r.JiraREST
		state = &copy
	}
	if in.JiraREST != nil {
		state = &JiraRESTView{CloudID: in.JiraREST.CloudID}
	}
	if state == nil {
		if in.JiraRESTCredential != nil {
			return nil, fmt.Errorf("%w: select a Jira REST Cloud ID before updating its credential", ErrInvalid)
		}
		return nil, nil
	}
	if !jiraRESTConnection(View{Service: in.Service, Endpoint: in.Endpoint, LocalProcess: in.LocalProcess, ArgumentPolicy: in.ArgumentPolicy, AtlassianPolicy: in.AtlassianPolicy, MetadataHeaders: in.MetadataHeaders}) {
		return nil, fmt.Errorf("%w: Jira REST requires an unrestricted first-party Atlassian or Jira MCP connection", ErrInvalid)
	}
	if r.JiraREST != nil && state.CloudID != r.JiraREST.CloudID && in.JiraRESTCredential == nil {
		return nil, fmt.Errorf("%w: replace or clear the Jira REST credential when changing its Cloud ID", ErrInvalid)
	}
	return state, nil
}

func jiraRESTAAD(id, cloud string) string { return prefix + id + "/jira-rest/" + cloud }

func (r *record) syncJiraRESTPresence() {
	if r.JiraREST != nil {
		r.JiraREST.CredentialConfigured = r.JiraRESTSealed != ""
	}
}

func (m *Manager) updateJiraREST(r *record, in Input) (bool, error) {
	state, err := jiraRESTState(*r, in)
	if err != nil {
		return false, err
	}
	changed := state != nil && (r.JiraREST == nil || state.CloudID != r.JiraREST.CloudID || in.JiraRESTCredential != nil)
	if c := in.JiraRESTCredential; c != nil {
		if c.Clear {
			r.JiraRESTSealed = ""
		} else {
			r.JiraRESTSealed, err = m.Box.Seal([]byte(c.Username+":"+c.Value), jiraRESTAAD(in.ID, state.CloudID))
			if err != nil {
				return false, fmt.Errorf("Jira REST credential could not be encrypted")
			}
		}
	}
	r.JiraREST = state
	r.syncJiraRESTPresence()
	return changed, nil
}

// Trusted gateway callers still have to prove the complete URL path scope before
// any credential is opened or the shared connection quota is consumed. Query
// escaping is independent from path escaping and remains supported.
func jiraRESTPath(cloud, value string) bool {
	if !jiraRESTCloudID.MatchString(cloud) || !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") || strings.ContainsAny(value, "#\\") || strings.ContainsFunc(value, unicode.IsControl) {
		return false
	}
	u, err := url.ParseRequestURI(value)
	return err == nil && u.Scheme == "" && u.Host == "" && u.Opaque == "" &&
		u.RawPath == "" && !strings.Contains(u.EscapedPath(), "%") && path.Clean(u.Path) == u.Path &&
		strings.HasPrefix(u.Path, "/ex/jira/"+cloud+"/rest/api/3/")
}

func (m *Manager) jiraRESTToken(r record) (string, error) {
	if r.JiraREST == nil || r.JiraRESTSealed == "" || m.Box == nil {
		return "", fmt.Errorf("Jira REST credential unavailable")
	}
	raw, err := m.Box.Open(r.JiraRESTSealed, jiraRESTAAD(r.ID, r.JiraREST.CloudID))
	if err != nil {
		return "", fmt.Errorf("Jira REST credential unavailable")
	}
	return base64.StdEncoding.EncodeToString(raw), nil
}

// Detect both JSON-escaped and partially percent-encoded reusable material.
// Decode valid escapes independently: a malformed escape elsewhere in an
// untrusted response must not hide a reflected credential.
func containsJiraRESTCredential(raw json.RawMessage, token string) bool {
	ctx := context.WithValue(context.Background(), credentialSchemeKey{}, "basic")
	check := func(value string) bool {
		decoded := make([]byte, 0, len(value))
		for i := 0; i < len(value); i++ {
			if value[i] == '%' && i+2 < len(value) {
				if b, err := strconv.ParseUint(value[i+1:i+3], 16, 8); err == nil {
					decoded = append(decoded, byte(b))
					i += 2
					continue
				}
			}
			decoded = append(decoded, value[i])
		}
		return containsTransportCredential(ctx, []byte(value), token) || containsTransportCredential(ctx, decoded, token)
	}
	if check(string(raw)) {
		return true
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return false
	}
	var contains func(any) bool
	contains = func(value any) bool {
		switch v := value.(type) {
		case string:
			return check(v)
		case []any:
			for _, child := range v {
				if contains(child) {
					return true
				}
			}
		case map[string]any:
			for key, child := range v {
				if check(key) || contains(child) {
					return true
				}
			}
		}
		return false
	}
	return contains(value)
}
