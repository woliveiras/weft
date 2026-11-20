// Package reviewroute routes new, revision-scoped SCM delivery facts to an
// explicitly reviewed running graph. It neither starts graphs nor grants tools.
package reviewroute

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/scm"
	"github.com/woliveiras/weft/server/internal/storage"
)

const (
	RoutePrefix                        = "scm/review-routes/"
	ReceiptPrefix                      = "scm/review-route-receipts/"
	ChangeRequested   domain.EventType = "scm.review_route_change_requested"
	RecoveryRequested domain.EventType = "scm.review_route_recovery_requested"
	MessageType                        = "review.requested"
)

var (
	ErrInvalid     = errors.New("invalid review route")
	ErrUnavailable = errors.New("review route dispatch is unavailable")
	ErrUnconfirmed = errors.New("review route dispatch outcome is unconfirmed; automatic replay is refused")
	namePattern    = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,62}$`)
	hashPattern    = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)
	idPattern      = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

// Input pins both the resource authority and the existing destination graph.
// ConnectionRevision is the SCM connection's RFC3339Nano updatedAt revision.
type Input struct {
	Name               string `json:"name"`
	Connection         string `json:"connection"`
	ConnectionRevision string `json:"connectionRevision"`
	Provider           string `json:"provider"`
	Instance           string `json:"instance"`
	Repository         string `json:"repository"`
	GraphID            string `json:"graphId"`
	GraphVersion       int    `json:"graphVersion"`
	ChannelID          string `json:"channelId"`
	NodeID             string `json:"nodeId"`
	Enabled            bool   `json:"enabled"`
}

type Route struct {
	Input
	ActivationSeq int64     `json:"activationSeq"`
	Revision      int64     `json:"revision"`
	UpdatedAt     time.Time `json:"updatedAt"`
	UpdatedBy     string    `json:"updatedBy"`
}

// PullRequest is identity metadata only; provider content is not authorization.
type PullRequest struct {
	Connection         string `json:"connection"`
	ConnectionRevision string `json:"connectionRevision"`
	Provider           string `json:"provider"`
	Instance           string `json:"instance"`
	Repository         string `json:"repository"`
	Number             int    `json:"number"`
	URL                string `json:"url"`
	HeadSHA            string `json:"headSha"`
	BaseSHA            string `json:"baseSha"`
}

// Receipt is globally unique for a scoped PR revision, across routes/events.
// Dispatching is journaled before the callback. It is never automatically sent
// again, including when the process dies before it can record the outcome.
type Receipt struct {
	ID string `json:"id"`
	PullRequest
	Route             Route     `json:"route"`
	EventID           string    `json:"eventId"`
	EventSeq          int64     `json:"eventSeq"`
	SourceGraphID     string    `json:"sourceGraphId"`
	SourceExecutionID string    `json:"sourceExecutionId"`
	State             string    `json:"state"` // queued, dispatching, published, unknown, failed
	ExpectedRunID     string    `json:"expectedRunId,omitempty"`
	DispatchRunID     string    `json:"dispatchRunId,omitempty"`
	MessageID         string    `json:"messageId"`
	Error             string    `json:"error,omitempty"`
	CreatedAt         time.Time `json:"createdAt"`
	UpdatedAt         time.Time `json:"updatedAt"`
	Revision          int64     `json:"revision"`
}

// Validate must be a local read-only check of effective graph grants and the
// reviewed input contract. It is called by Preview as well as before dispatch.
// Dispatch must inject only into ExpectedRunID, using MessageID unchanged, and
// return that same run ID. Neither callback may start a graph or add grants.
type Manager struct {
	Store    storage.Store
	SCM      scm.Manager
	Validate func(context.Context, Route) error
	Dispatch func(context.Context, Route, Receipt) (dispatchRunID string, err error)
	Now      func() time.Time
}

func (m Manager) now() time.Time {
	if m.Now != nil {
		return m.Now().UTC()
	}
	return time.Now().UTC()
}

func instance(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 2048 || u.Host == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1"))) {
		return "", ErrInvalid
	}
	u.Host = strings.ToLower(u.Host)
	return strings.TrimRight(u.String(), "/"), nil
}
func normalize(in Input) (Input, error) {
	if !namePattern.MatchString(in.Name) || !namePattern.MatchString(in.Connection) || in.GraphVersion < 1 {
		return in, ErrInvalid
	}
	for _, v := range []string{in.GraphID, in.ChannelID, in.NodeID, in.Repository} {
		if v == "" || len(v) > 200 || !utf8.ValidString(v) || strings.TrimSpace(v) != v || strings.ContainsAny(v, "\x00\r\n") {
			return in, ErrInvalid
		}
	}
	if in.Provider != "github" && in.Provider != "bitbucket-cloud" && in.Provider != "bitbucket-dc" {
		return in, ErrInvalid
	}
	t, err := time.Parse(time.RFC3339Nano, in.ConnectionRevision)
	if err != nil || t.IsZero() {
		return in, ErrInvalid
	}
	canonical, err := instance(in.Instance)
	if err != nil {
		return in, err
	}
	in.Instance = canonical
	return in, nil
}
func connectionInstance(c scm.Connection) (string, error) {
	raw := c.APIURL
	if raw == "" {
		switch c.Kind {
		case "github":
			raw = "https://api.github.com"
		case "bitbucket-cloud":
			raw = "https://api.bitbucket.org/2.0"
		}
	}
	return instance(raw)
}

// connection uses the SCM manager's credential-free view, never Open/decryption.
// The surrounding document revision guards the selected identity through CAS.
func (m Manager) connection(ctx context.Context, in Input) (int64, error) {
	before, err := m.Store.GetSetting(ctx, scm.SettingKey)
	if err != nil {
		return 0, err
	}
	manager := m.SCM
	manager.Store = m.Store // The identity read and its CAS guard share one store.
	views, err := manager.List(ctx)
	if err != nil {
		return 0, err
	}
	after, err := m.Store.GetSetting(ctx, scm.SettingKey)
	if err != nil {
		return 0, err
	}
	if before.Revision != after.Revision {
		return 0, storage.ErrConflict
	}
	for _, c := range views {
		if c.Name != in.Connection {
			continue
		}
		u, err := connectionInstance(c.Connection)
		if err != nil {
			return 0, err
		}
		if !c.TokenConfigured || c.Kind != in.Provider || u != in.Instance || c.Repository != in.Repository || c.UpdatedAt.UTC().Format(time.RFC3339Nano) != in.ConnectionRevision {
			return 0, storage.ErrConflict
		}
		return after.Revision, nil
	}
	return 0, storage.ErrNotFound
}
func (m Manager) validate(ctx context.Context, r Route) (int64, error) {
	if m.Store == nil || m.Validate == nil {
		return 0, ErrUnavailable
	}
	docRev, err := m.connection(ctx, r.Input)
	if err != nil {
		return 0, err
	}
	g, err := m.Store.GetGraph(ctx, r.GraphID, r.GraphVersion)
	if err != nil {
		return 0, err
	}
	channel := g.Channel(r.ChannelID)
	if g.Metadata.Version != r.GraphVersion || g.Node(r.NodeID) == nil || channel == nil || channel.From != "" || !slices.Contains(g.Consumers(channel), r.NodeID) {
		return 0, ErrInvalid
	}
	if len(channel.Types) > 0 {
		accepted := false
		for _, pattern := range channel.Types {
			accepted = accepted || domain.GlobMatch(pattern, MessageType)
		}
		if !accepted {
			return 0, ErrInvalid
		}
	}
	if err = m.Validate(ctx, r); err != nil {
		return 0, fmt.Errorf("review route policy: %w", err)
	}
	return docRev, nil
}
func receiptID(p PullRequest) string {
	raw, _ := json.Marshal([]any{p.Connection, p.ConnectionRevision, p.Provider, p.Instance, strings.ToLower(p.Repository), p.Number, p.HeadSHA, p.BaseSHA})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func validPull(p PullRequest) bool {
	_, err := normalize(Input{Name: "validation", Connection: p.Connection, ConnectionRevision: p.ConnectionRevision, Provider: p.Provider, Instance: p.Instance, Repository: p.Repository, GraphID: "g", GraphVersion: 1, ChannelID: "input", NodeID: "reviewer"})
	u, e := url.Parse(p.URL)
	return err == nil && p.Number > 0 && hashPattern.MatchString(p.HeadSHA) && hashPattern.MatchString(p.BaseSHA) && len(p.URL) <= 4096 && e == nil && u.Host != "" && u.User == nil && u.Fragment == "" && (u.Scheme == "https" || (u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1")))
}
