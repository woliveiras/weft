package jiradc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/secrets"
	"github.com/woliveiras/weft/server/internal/storage"
)

const ConnectionPrefix = "tracker/jira-dc/connections/"

var connectionName = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
var ErrInvalid = errors.New("invalid Jira Data Center connection")

// Connection is credential-free. Revision fences every operation, including tests.
type Connection struct {
	Name                 string    `json:"name"`
	Kind                 string    `json:"kind"`
	SiteURL              string    `json:"siteUrl"`
	ProjectID            string    `json:"projectId"`
	Enabled              bool      `json:"enabled"`
	Revision             int64     `json:"revision"`
	CredentialConfigured bool      `json:"credentialConfigured"`
	UpdatedAt            time.Time `json:"updatedAt"`
	UpdatedBy            string    `json:"updatedBy"`
}
type ConnectionInput struct {
	Name      string  `json:"name"`
	Kind      string  `json:"kind"`
	SiteURL   string  `json:"siteUrl"`
	ProjectID string  `json:"projectId"`
	Enabled   bool    `json:"enabled"`
	Token     *string `json:"token,omitempty"`
}
type connectionRecord struct {
	Connection
	SealedToken string `json:"sealedToken"`
}
type Connections struct {
	Store storage.Store
	Box   *secrets.Box
}

func connectionAAD(c Connection) string {
	return ConnectionPrefix + c.Name + "/" + c.SiteURL + "/" + c.ProjectID + "/token"
}
func (m Connections) load(ctx context.Context, name string) (connectionRecord, error) {
	var record connectionRecord
	if !connectionName.MatchString(name) {
		return record, ErrInvalid
	}
	row, err := m.Store.GetSetting(ctx, ConnectionPrefix+name)
	if err != nil {
		return record, err
	}
	if json.Unmarshal(row.Value, &record) != nil || record.Name != name || record.Kind != "jira-dc" {
		return connectionRecord{}, errors.New("stored Jira connection is corrupt")
	}
	if _, err := New(record.SiteURL, record.ProjectID, "validation"); err != nil {
		return connectionRecord{}, errors.New("stored Jira connection scope is invalid")
	}
	record.Revision = row.Revision
	record.CredentialConfigured = record.SealedToken != ""
	return record, nil
}
func (m Connections) Get(ctx context.Context, name string) (Connection, error) {
	record, err := m.load(ctx, name)
	return record.Connection, err
}
func (m Connections) List(ctx context.Context) ([]Connection, error) {
	rows, err := m.Store.ListSettings(ctx, ConnectionPrefix)
	if err != nil {
		return nil, err
	}
	out := make([]Connection, 0, len(rows))
	for _, row := range rows {
		var r connectionRecord
		if json.Unmarshal(row.Value, &r) != nil {
			return nil, errors.New("stored Jira connection is corrupt")
		}
		v, err := m.Get(ctx, r.Name)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}
func (m Connections) Put(ctx context.Context, in ConnectionInput, expected int64, actor string) (Connection, error) {
	invalid := func(message string) (Connection, error) {
		return Connection{}, fmt.Errorf("%w: %s", ErrInvalid, message)
	}
	if expected < 0 {
		return invalid("select a valid connection revision")
	}
	var err error
	in, err = normalizeInput(in)
	if err != nil {
		return Connection{}, err
	}
	old, err := m.load(ctx, in.Name)
	if errors.Is(err, storage.ErrNotFound) {
		if expected != 0 {
			return Connection{}, storage.ErrConflict
		}
		old = connectionRecord{}
	} else if err != nil {
		return Connection{}, err
	} else if expected != old.Revision || expected == 0 {
		return Connection{}, storage.ErrConflict
	}
	next := connectionRecord{Connection: Connection{Name: in.Name, Kind: in.Kind, SiteURL: in.SiteURL, ProjectID: in.ProjectID, Enabled: in.Enabled, UpdatedAt: time.Now().UTC(), UpdatedBy: actor}, SealedToken: old.SealedToken}
	if in.Token == nil {
		if old.SealedToken == "" || old.SiteURL != in.SiteURL || old.ProjectID != in.ProjectID {
			return invalid("provide a native credential when creating or changing site/project scope")
		}
	} else {
		if _, err := New(in.SiteURL, in.ProjectID, *in.Token); err != nil {
			return invalid("provide a valid single-line PAT")
		}
		next.SealedToken, err = m.Box.Seal([]byte(*in.Token), connectionAAD(next.Connection))
		if err != nil {
			return Connection{}, err
		}
	}
	raw, err := json.Marshal(next)
	if err != nil {
		return Connection{}, err
	}
	// Audit contains no URL, project, credential or request payload. Failure prevents
	// mutation. The record's CAS is the authoritative success/revision boundary.
	ev := domain.NewEvent("tracker.connection_change_requested", "", map[string]any{"connection": in.Name, "provider": "jira-dc", "expectedRevision": expected}).WithActor(actor)
	if err = m.Store.AppendEvent(ctx, ev); err != nil {
		return Connection{}, err
	}
	rev, err := m.Store.PutSetting(ctx, ConnectionPrefix+in.Name, raw, expected, actor)
	if err != nil {
		return Connection{}, err
	}
	next.Revision = rev
	next.CredentialConfigured = true
	return next.Connection, nil
}

// Open refuses stale/disabled connections before decrypting their native PAT.
// The returned client is not itself an authorization grant.
func (m Connections) Open(ctx context.Context, name string, revision int64) (*Client, error) {
	r, err := m.load(ctx, name)
	if err != nil {
		return nil, err
	}
	if revision < 1 || r.Revision != revision || !r.Enabled {
		return nil, fmt.Errorf("%w: Jira connection changed or is disabled; review its scope again", storage.ErrConflict)
	}
	token, err := m.Box.Open(r.SealedToken, connectionAAD(r.Connection))
	if err != nil {
		return nil, errors.New("native Jira credential is unavailable")
	}
	return New(r.SiteURL, r.ProjectID, string(token))
}
