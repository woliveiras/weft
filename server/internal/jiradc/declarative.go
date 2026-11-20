package jiradc

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"

	"github.com/woliveiras/weft/server/internal/storage"
)

func normalizeInput(in ConnectionInput) (ConnectionInput, error) {
	if !connectionName.MatchString(in.Name) || in.Kind != "jira-dc" {
		return in, fmt.Errorf("%w: select jira-dc and a valid connection name", ErrInvalid)
	}
	token := "validation"
	if in.Token != nil {
		token = *in.Token
	}
	client, err := New(in.SiteURL, in.ProjectID, token)
	if err != nil {
		return in, fmt.Errorf("%w: %s", ErrInvalid, err)
	}
	in.SiteURL = client.site
	return in, nil
}

// Preview validates configuration without sealing credentials, changing settings,
// contacting Jira or creating an agent grant.
func (m Connections) Preview(ctx context.Context, in ConnectionInput) (ConnectionInput, error) {
	in, err := normalizeInput(in)
	if err != nil {
		return in, err
	}
	old, err := m.load(ctx, in.Name)
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		return in, err
	}
	if in.Token == nil && (old.SealedToken == "" || old.SiteURL != in.SiteURL || old.ProjectID != in.ProjectID) {
		return in, fmt.Errorf("%w: provide a native credential when creating or changing site/project scope", ErrInvalid)
	}
	return in, nil
}

func (m Connections) CredentialMatches(ctx context.Context, name, value string) (bool, error) {
	r, err := m.load(ctx, name)
	if err != nil {
		return false, err
	}
	raw, err := m.Box.Open(r.SealedToken, connectionAAD(r.Connection))
	if err != nil {
		return false, errors.New("native Jira credential is unavailable")
	}
	return subtle.ConstantTimeCompare(raw, []byte(value)) == 1, nil
}
