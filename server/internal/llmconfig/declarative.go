package llmconfig

import (
	"context"
	"crypto/subtle"
	"strings"
)

// Preview validates against a projected plugin state without saving or calling upstream.
func (m *Manager) Preview(ctx context.Context, in ProviderInput, plugins map[string]bool) (ProviderInput, error) {
	d, _, err := m.read(ctx)
	if err != nil {
		return in, err
	}
	for id, enabled := range plugins {
		if d.Plugins == nil {
			d.Plugins = map[string]pluginState{}
		}
		d.Plugins[id] = pluginState{Enabled: enabled}
	}
	var existing *managedProvider
	if i := d.find(in.Name); i >= 0 {
		existing = &d.Providers[i]
	}
	if err := m.validate(&in, &d, existing); err != nil {
		return in, err
	}
	key, err := m.resolveKey(&in, existing)
	if err != nil {
		return in, err
	}
	if err := checkBuild(&in, key); err != nil {
		return in, err
	}
	return in, nil
}

// CredentialMatches only returns equality; no plaintext or credential digest crosses the manager boundary.
func (m *Manager) CredentialMatches(ctx context.Context, name, value string) (bool, error) {
	d, _, err := m.read(ctx)
	if err != nil {
		return false, err
	}
	i := d.find(name)
	if i < 0 {
		return false, nil
	}
	key, err := m.resolveKey(&ProviderInput{}, &d.Providers[i])
	if err != nil {
		return false, err
	}
	return subtle.ConstantTimeCompare([]byte(key), []byte(strings.TrimSpace(value))) == 1, nil
}
