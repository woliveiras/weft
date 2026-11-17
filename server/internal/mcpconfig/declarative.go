package mcpconfig

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	"github.com/woliveiras/weft/server/internal/storage"
)

func (m *Manager) Preview(ctx context.Context, in Input) error {
	if in.LocalProcess != nil && !m.AllowLocalProcesses {
		return fmt.Errorf("%w: local processes require the native installation", ErrInvalid)
	}
	if err := validate(in); err != nil {
		return err
	}
	r, err := m.load(ctx, in.ID)
	if errors.Is(err, storage.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if (r.Endpoint != in.Endpoint || credentialKind(r.CredentialType) != credentialKind(in.CredentialType) || !reflect.DeepEqual(r.MetadataHeaders, in.MetadataHeaders)) && r.Sealed != "" && r.OAuth == nil && in.OAuth == nil && in.Credential == nil {
		return fmt.Errorf("%w: replace the credential when changing endpoint, credential type or metadata headers", ErrInvalid)
	}
	return nil
}

// SecretSlots exposes presence, never ciphertext, for export placeholders.
func (m *Manager) SecretSlots(ctx context.Context, id string) (map[string]bool, error) {
	r, err := m.load(ctx, id)
	if err != nil {
		return nil, err
	}
	return map[string]bool{"credential": r.Sealed != "", "oauthClient": r.OAuthClientSealed != "", "processEnvironment": r.ProcessEnvironmentSealed != ""}, nil
}
func (m *Manager) CredentialMatches(ctx context.Context, id, slot, value string) (bool, error) {
	r, err := m.load(ctx, id)
	if err != nil {
		return false, err
	}
	sealed, aad := r.Sealed, prefix+id
	if slot == "oauthClient" {
		sealed, aad = r.OAuthClientSealed, prefix+id+"/oauth-client"
	}
	if sealed == "" {
		return false, nil
	}
	raw, err := m.Box.Open(sealed, aad)
	if err != nil {
		return false, err
	}
	return subtle.ConstantTimeCompare(raw, []byte(value)) == 1, nil
}

// MergeEnvironment retains omitted keys while resolving only named reference updates.
func (m *Manager) MergeEnvironment(ctx context.Context, id string, updates map[string]string) (map[string]string, error) {
	out := map[string]string{}
	r, err := m.load(ctx, id)
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		return nil, err
	}
	if r.ProcessEnvironmentSealed != "" {
		raw, e := m.Box.Open(r.ProcessEnvironmentSealed, prefix+id+"/process-environment")
		if e != nil {
			return nil, e
		}
		if e = json.Unmarshal(raw, &out); e != nil {
			return nil, errors.New("invalid stored process environment")
		}
	}
	for k, v := range updates {
		out[k] = v
	}
	return out, nil
}

// TrackerCompatible shares the trusted tracker endpoint rules with declarative previews.
func TrackerCompatible(v View, kind string) bool { return trackerOrigin(v, kind) != "" }
