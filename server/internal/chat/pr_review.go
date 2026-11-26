package chat

import (
	"context"
	"regexp"
	"time"

	"github.com/woliveiras/weft/server/internal/storage"
)

const AnalyzePR = "pr.analyze"

type PRSelector struct {
	Number             string
	Workflow           string
	Connection         string
	Repository         string
	URL                string
	Node               string
	ConnectionRevision string
	WorkflowVersion    int
	Run                string
}

func (p PRSelector) Valid() bool {
	_, ok := p.NumberAndRepository()
	return ok && (p.Workflow == "" || shortLabel(p.Workflow)) &&
		(p.Connection == "" || shortLabel(p.Connection)) && (p.Node == "" || shortLabel(p.Node))
}

// PRAnalysis contains only the immutable source identity and the execution
// destination. Repository content is never stored in a conversation proposal.
type PRAnalysis struct {
	Number             int    `json:"number"`
	Connection         string `json:"connection"`
	ConnectionRevision string `json:"connectionRevision"`
	Provider           string `json:"provider"`
	Repository         string `json:"repository"`
	Draft              bool   `json:"draft,omitempty"`
	HeadRef            string `json:"headRef"`
	BaseRef            string `json:"baseRef"`
	Head               string `json:"head"`
	Base               string `json:"base"`
	Graph              string `json:"graph"`
	Version            int    `json:"version"`
	Node               string `json:"node"`
	Channel            string `json:"channel"`
	Run                string `json:"run"`
	ModelProvider      string `json:"modelProvider"`
	ModelName          string `json:"modelName"`
}

var commitSHA = regexp.MustCompile("^(?:[a-f0-9]{40}|[a-f0-9]{64})$")

func NewPRReview(owner, session string, p PRAnalysis, now time.Time) (*OperationReview, error) {
	if p.Number <= 0 || !commitSHA.MatchString(p.Head) || !commitSHA.MatchString(p.Base) || p.Version <= 0 {
		return nil, ErrInvalidPlan
	}
	for _, v := range []string{p.HeadRef, p.BaseRef, p.Connection, p.Provider, p.Repository, p.Graph, p.Node, p.Channel, p.Run, p.ModelProvider, p.ModelName, p.ConnectionRevision} {
		if !shortLabel(v) {
			return nil, ErrInvalidPlan
		}
	}
	return newReview(owner, session, now, OperationReview{PR: &p})
}

func (s Sessions) ObservePR(ctx context.Context, owner, session, id, prior, state, execution, summary string) (*Session, error) {
	summary, comments := parsePRDraft(summary)
	return s.update(ctx, owner, session, func(v *Session) (bool, error) {
		for i := range v.Messages {
			m := &v.Messages[i]
			r := m.Review
			if r == nil || r.PR == nil || r.ID != id {
				continue
			}
			if r.State != prior || (prior != ReviewApplying && prior != ReviewUnknown && prior != ReviewQueued) || (prior == state && r.ResultID == execution && r.PRSummary == summary) {
				return false, nil
			}
			r.State, r.ResultID, r.PRSummary = state, execution, summary
			r.PRComments = comments
			m.Unread = m.Unread || state == ReviewApplied || state == ReviewIncomplete || state == ReviewUnknown
			v.LastActivity = time.Now().UTC()
			return true, nil
		}
		return false, storage.ErrNotFound
	})
}

// SafeReviewContent applies the same secret and control-character checks as chat
// before the bounded repository diff leaves the control plane.
func SafeReviewContent(diff string) bool { return safeText(diff, 12<<10) }
