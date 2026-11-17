// Package taskconfig stores user requests without granting execution authority.
package taskconfig

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
)

const prefix = "tasks/drafts/"

var validID = regexp.MustCompile(`^tsk_[a-z0-9]{1,80}$`)
var ErrInvalid = errors.New("invalid task")

var scmName = regexp.MustCompile(`^[a-z0-9]([a-z0-9._-]{0,61}[a-z0-9])?$`)

type Input struct {
	Source        *IssueSource `json:"source,omitempty"`
	ExecutionMode string       `json:"executionMode,omitempty"`
	ChannelID     string       `json:"channelId,omitempty"`
	MessageType   string       `json:"messageType,omitempty"`
	PayloadJSON   string       `json:"payloadJson,omitempty"`
	ProjectID     string       `json:"projectId,omitempty"`
	BaseCommit    string       `json:"baseCommit,omitempty"`
	Title         string       `json:"title"`
	Request       string       `json:"request"`
	Outcome       string       `json:"outcome"`
	GraphID       string       `json:"graphId"`
	GraphVersion  int          `json:"graphVersion"`
	NodeID        string       `json:"nodeId"`
	ResultNodeID  string       `json:"resultNodeId,omitempty"`
	// SCMConnection, when set, lets the result agent push the task branch
	// and open a pull request through that connection.
	SCMConnection string `json:"scmConnection,omitempty"`
}
type Draft struct {
	TriggerGitHubIssue string      `json:"triggerGitHubIssue,omitempty"`
	Submitted          bool        `json:"submitted,omitempty"`
	Resolution         *Resolution `json:"resolution,omitempty"`
	ObservedState      string      `json:"observedState,omitempty"`
	Input
	ExecutionGraphID string    `json:"executionGraphId,omitempty"`
	LaunchError      string    `json:"launchError,omitempty"`
	ID               string    `json:"id"`
	Revision         int64     `json:"revision"`
	Status           string    `json:"status"`
	CreatedBy        string    `json:"createdBy"`
	UpdatedBy        string    `json:"updatedBy"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}
type Page struct {
	Items []*Draft `json:"items"`
	Next  string   `json:"next,omitempty"`
}
type Manager struct{ Store storage.Store }

func Validate(in Input) error {
	if in.Source != nil {
		if _, _, err := in.Source.ReadCall(); err != nil {
			return err
		}
	}
	switch in.ExecutionMode {
	case "", "workflow", "local", "sandbox":
	default:
		return fmt.Errorf("%w: invalid execution environment", ErrInvalid)
	}
	for _, field := range []struct {
		name, value string
		max         int
	}{
		{"title", in.Title, 200}, {"request", in.Request, 16384}, {"graphId", in.GraphID, 128}, {"nodeId", in.NodeID, 128},
	} {
		if strings.TrimSpace(field.value) == "" || len(field.value) > field.max || !utf8.ValidString(field.value) || strings.ContainsRune(field.value, 0) {
			return fmt.Errorf("%w: %s is required and must be valid text within %d bytes", ErrInvalid, field.name, field.max)
		}
	}
	if len(in.ResultNodeID) > 128 || !utf8.ValidString(in.ResultNodeID) || strings.ContainsRune(in.ResultNodeID, 0) {
		return fmt.Errorf("%w: invalid result agent", ErrInvalid)
	}
	if in.SCMConnection != "" && !scmName.MatchString(in.SCMConnection) {
		return fmt.Errorf("%w: invalid SCM connection name", ErrInvalid)
	}
	if (in.ProjectID == "") != (in.BaseCommit == "") || len(in.ProjectID) > 128 || len(in.BaseCommit) > 64 {
		return fmt.Errorf("%w: project and full base commit must be supplied together", ErrInvalid)
	}
	if in.ChannelID != "" {
		if len(in.ChannelID) > 128 || len(in.MessageType) > 128 || strings.TrimSpace(in.MessageType) == "" || strings.ContainsRune(in.MessageType, 0) || len(in.PayloadJSON) > 16384 || !json.Valid([]byte(in.PayloadJSON)) {
			return fmt.Errorf("%w: channel requires a message type and valid bounded JSON payload", ErrInvalid)
		}
	} else if in.MessageType != "" || in.PayloadJSON != "" {
		return fmt.Errorf("%w: payload requires an input channel", ErrInvalid)
	}
	switch in.Outcome {
	case "understand", "implement", "investigate", "review", "freeform":
	default:
		return fmt.Errorf("%w: unknown outcome", ErrInvalid)
	}
	if in.GraphVersion < 1 {
		return fmt.Errorf("%w: graphVersion must be positive", ErrInvalid)
	}
	return nil
}
func decode(rec *storage.Setting) (*Draft, error) {
	var d Draft
	if err := json.Unmarshal(rec.Value, &d); err != nil {
		return nil, errors.New("invalid stored task draft")
	}
	d.Revision = rec.Revision
	return &d, nil
}
func (m Manager) Get(ctx context.Context, id string) (*Draft, error) {
	if !validID.MatchString(id) {
		return nil, storage.ErrNotFound
	}
	rec, err := m.Store.GetSetting(ctx, prefix+id)
	if err != nil {
		return nil, err
	}
	return decode(rec)
}
func (m Manager) List(ctx context.Context, after string, limit int) (Page, error) {
	page := Page{Items: []*Draft{}}
	if after != "" && !validID.MatchString(after) {
		return page, fmt.Errorf("%w: invalid cursor", ErrInvalid)
	}
	if limit < 1 || limit > 100 {
		return page, fmt.Errorf("%w: limit must be between 1 and 100", ErrInvalid)
	}
	records, err := m.Store.ListSettings(ctx, prefix)
	if err != nil {
		return page, err
	}
	for _, rec := range records {
		if rec.Key <= prefix+after {
			continue
		}
		if len(page.Items) == limit {
			page.Next = page.Items[len(page.Items)-1].ID
			break
		}
		d, err := decode(rec)
		if err != nil {
			return Page{}, err
		}
		page.Items = append(page.Items, d)
	}
	return page, nil
}
func (m Manager) Save(ctx context.Context, id string, in Input, revision int64, actor string) (*Draft, error) {
	return m.save(ctx, id, in, revision, actor, "")
}

// SaveTriggered records the trusted tracker identity separately from editable task text.
func (m Manager) SaveTriggered(ctx context.Context, in Input, actor, githubIssueKey string) (*Draft, error) {
	if githubIssueKey != "" && !githubIssue.MatchString(githubIssueKey) {
		return nil, fmt.Errorf("%w: invalid trigger issue identity", ErrInvalid)
	}
	return m.save(ctx, "", in, 0, actor, githubIssueKey)
}

func (m Manager) save(ctx context.Context, id string, in Input, revision int64, actor, githubIssueKey string) (*Draft, error) {
	if err := Validate(in); err != nil {
		return nil, err
	}
	// Saving is allowed only against the explicitly selected current version.
	// Retained drafts may later become stale; reads do not discard them.
	g, err := m.Store.GetGraph(ctx, in.GraphID, 0)
	if err != nil {
		return nil, err
	}
	if g.Metadata.Version != in.GraphVersion {
		return nil, storage.ErrConflict
	}
	n := g.Node(in.NodeID)
	if n == nil || n.Disabled {
		return nil, fmt.Errorf("%w: select an enabled agent in the workflow", ErrInvalid)
	}
	now := time.Now().UTC()
	d := &Draft{Input: in, ID: id, TriggerGitHubIssue: githubIssueKey, Status: "draft", CreatedBy: actor, UpdatedBy: actor, CreatedAt: now, UpdatedAt: now}
	if id == "" {
		if revision != 0 {
			return nil, storage.ErrConflict
		}
		d.ID = domain.NewID("tsk")
	} else {
		old, err := m.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		if old.Status != "draft" || revision < 1 || revision != old.Revision {
			return nil, storage.ErrConflict
		}
		if old.Resolution != nil && old.Resolution.Repository != "" && strings.Contains(in.Request, old.Resolution.Repository) && in.ProjectID == "" {
			return nil, fmt.Errorf("%w: select a registered project for the repository URL or remove the URL from the request", ErrInvalid)
		}
		d.Submitted = old.Submitted
		d.TriggerGitHubIssue = old.TriggerGitHubIssue
		d.CreatedAt = old.CreatedAt
		d.CreatedBy = old.CreatedBy
	}
	// Audit intent before the CAS; success is represented by the persisted record.
	// Failure to audit prevents mutation. The event includes no request content.
	ev := domain.NewEvent("task.draft_save_requested", in.GraphID, map[string]any{"taskId": d.ID, "expectedRevision": revision}).WithActor(actor)
	if err := m.Store.AppendEvent(ctx, ev); err != nil {
		return nil, err
	}
	b, err := json.Marshal(d)
	if err != nil {
		return nil, err
	}
	rev, err := m.Store.PutSetting(ctx, prefix+d.ID, b, revision, actor)
	if err != nil {
		return nil, err
	}
	d.Revision = rev
	return d, nil
}

// Transition commits lifecycle state with CAS, preserving the request snapshot.
func (m Manager) Transition(ctx context.Context, d *Draft, status, actor string) (*Draft, error) {
	old, err := m.Get(ctx, d.ID)
	if err != nil {
		return nil, err
	}
	if old.Revision != d.Revision {
		return nil, storage.ErrConflict
	}
	d.Status = status
	d.UpdatedBy = actor
	d.UpdatedAt = time.Now().UTC()
	if err = m.Store.AppendEvent(ctx, domain.NewEvent("task.transition_requested", d.GraphID, map[string]any{"taskId": d.ID, "from": old.Status, "to": status, "expectedRevision": d.Revision}).WithActor(actor)); err != nil {
		return nil, err
	}
	b, err := json.Marshal(d)
	if err != nil {
		return nil, err
	}
	rev, err := m.Store.PutSetting(ctx, prefix+d.ID, b, d.Revision, actor)
	if err != nil {
		return nil, err
	}
	d.Revision = rev
	return d, nil
}
