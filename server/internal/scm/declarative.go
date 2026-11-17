package scm

import (
	"context"
	"crypto/subtle"
	"strings"
)

func (m Manager) Preview(ctx context.Context, in Input) (Input, error) {
	d, _, err := m.read(ctx)
	if err != nil {
		return in, err
	}
	update := false
	for _, c := range d.Connections {
		if c.Name == in.Name {
			update = true
		}
	}
	if err := m.validate(&in, d, update); err != nil {
		return in, err
	}
	return in, nil
}
func (m Manager) CredentialMatches(ctx context.Context, name, value string) (bool, error) {
	c, err := m.Get(ctx, name)
	if err != nil {
		return false, err
	}
	if c.Token == "" {
		return false, nil
	}
	raw, err := m.Box.Open(c.Token, tokenAAD(name))
	if err != nil {
		return false, err
	}
	return subtle.ConstantTimeCompare(raw, []byte(strings.TrimSpace(value))) == 1, nil
}
