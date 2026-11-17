package mcpconfig

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
)

// LocalProcess contains reviewed, non-secret launch information. Credentials
// belong in the write-only ProcessEnvironment input, never in arguments.
type LocalProcess struct {
	Command   string   `json:"command"`
	Args      []string `json:"args"`
	Directory string   `json:"directory"`
}

func validateLocalProcess(in Input) error {
	if in.LocalProcess == nil {
		if in.ProcessEnvironment != nil {
			return fmt.Errorf("%w: process environment requires a local process", ErrInvalid)
		}
		return nil
	}
	if in.Service != "generic" || in.Endpoint != "" || in.OAuth != nil || in.OAuthClientSecret != nil || in.Credential != nil || in.CredentialType != "" || len(in.MetadataHeaders) > 0 || in.Template != nil || in.TransportProfile != "" || in.AtlassianPolicy != nil {
		return fmt.Errorf("%w: local processes cannot use remote connection or OAuth settings", ErrInvalid)
	}
	spec := processSpec{Command: in.LocalProcess.Command, Args: in.LocalProcess.Args, Directory: in.LocalProcess.Directory}
	if in.ProcessEnvironment != nil {
		spec.Environment = *in.ProcessEnvironment
	}
	if err := spec.validate(); err != nil {
		return fmt.Errorf("%w: %s", ErrInvalid, err)
	}
	return nil
}

func (m *Manager) updateProcess(r *record, in Input) error {
	if (r.LocalProcess == nil) != (in.LocalProcess == nil) && r.Revision > 0 {
		return fmt.Errorf("%w: create a separate connection to change between HTTP and local process", ErrInvalid)
	}
	if !reflect.DeepEqual(r.LocalProcess, in.LocalProcess) && r.ProcessEnvironmentSealed != "" && in.ProcessEnvironment == nil {
		return fmt.Errorf("%w: replace or clear environment values when changing the process", ErrInvalid)
	}
	if in.ProcessEnvironment == nil {
		return nil
	}
	r.EnvironmentKeys = nil
	r.ProcessEnvironmentSealed = ""
	if len(*in.ProcessEnvironment) == 0 {
		return nil
	}
	encoded, err := json.Marshal(*in.ProcessEnvironment)
	if err != nil {
		return err
	}
	if m.Box == nil {
		return fmt.Errorf("process credential storage unavailable")
	}
	r.ProcessEnvironmentSealed, err = m.Box.Seal(encoded, prefix+in.ID+"/process-environment")
	if err != nil {
		return err
	}
	for key := range *in.ProcessEnvironment {
		r.EnvironmentKeys = append(r.EnvironmentKeys, key)
	}
	sort.Strings(r.EnvironmentKeys)
	return nil
}

func (m *Manager) processSpec(r record) (processSpec, error) {
	if !m.AllowLocalProcesses || r.LocalProcess == nil {
		return processSpec{}, discoveryError("local MCP processes require the native installation")
	}
	spec := processSpec{Command: r.LocalProcess.Command, Args: r.LocalProcess.Args, Directory: r.LocalProcess.Directory}
	if r.ProcessEnvironmentSealed != "" {
		if m.Box == nil {
			return spec, discoveryError("MCP process environment unavailable")
		}
		raw, err := m.Box.Open(r.ProcessEnvironmentSealed, prefix+r.ID+"/process-environment")
		if err != nil || json.Unmarshal(raw, &spec.Environment) != nil {
			return spec, discoveryError("MCP process environment unavailable")
		}
	}
	return spec, nil
}

func (m *Manager) discoverConnection(ctx context.Context, r record, token string) ([]Tool, error) {
	if r.LocalProcess == nil {
		return m.Discover(ctx, r.Endpoint, token)
	}
	spec, err := m.processSpec(r)
	if err != nil {
		return nil, err
	}
	return processOperation(ctx, spec, nil)
}

func (m *Manager) callConnection(ctx context.Context, r record, token, name string, args json.RawMessage) (json.RawMessage, error) {
	if r.LocalProcess == nil {
		return m.Call(ctx, r.Endpoint, token, name, args)
	}
	spec, err := m.processSpec(r)
	if err != nil {
		return nil, err
	}
	var raw json.RawMessage
	_, err = processOperation(ctx, spec, func(call rpcCall) error {
		var callErr error
		raw, callErr = call(2, "tools/call", map[string]any{"name": name, "arguments": args})
		return callErr
	})
	return raw, err
}
