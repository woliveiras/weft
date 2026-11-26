package chat

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/storage"
)

const GuideNode = "node.guide"
const ReviewQueued = "queued"
const ReviewConsumed = "consumed"
const ReviewRunEnded = "run_ended"

type Guidance struct {
	Graph   string `json:"graph"`
	Node    string `json:"node"`
	Replica string `json:"replica"`
	Text    string `json:"text"`
	Run     string `json:"run,omitempty"`
	Version int    `json:"version,omitempty"`
}

func (g Guidance) ReplicaIndex() (int, error) {
	if g.Replica == "any" {
		return -1, nil
	}
	r, err := strconv.Atoi(g.Replica)
	if err != nil || r < 0 || strconv.Itoa(r) != g.Replica {
		return 0, ErrInvalidPlan
	}
	return r, nil
}

func (g Guidance) Valid() bool {
	_, err := g.ReplicaIndex()
	return err == nil && shortLabel(g.Graph) && shortLabel(g.Node) && strings.TrimSpace(g.Text) != "" && safeText(g.Text, 2000)
}

func NewGuidanceReview(owner, session string, g Guidance, now time.Time) (*OperationReview, error) {
	if !g.Valid() || g.Run == "" || g.Version <= 0 {
		return nil, ErrInvalidPlan
	}
	return newReview(owner, session, now, OperationReview{Guidance: &g})
}

// Operator delivery can be retried by the runtime. Record the latest observed
// receipt independently of any outcome of a turn.
func (s Sessions) ObserveGuidance(ctx context.Context, owner, sessionID, id, prior, state string, consumed *time.Time) (*Session, error) {
	return s.update(ctx, owner, sessionID, func(session *Session) (bool, error) {
		for i := range session.Messages {
			m := &session.Messages[i]
			r := m.Review
			if r == nil || r.Guidance == nil || r.ID != id {
				continue
			}
			if r.State != prior {
				return false, nil
			}
			switch r.State {
			case ReviewQueued, ReviewConsumed, ReviewApplying, ReviewUnknown, ReviewRunEnded:
			default:
				return false, nil
			}
			if r.State == state && (r.ConsumedAt == nil && consumed == nil || r.ConsumedAt != nil && consumed != nil && r.ConsumedAt.Equal(*consumed)) {
				return false, nil
			}
			r.State, r.ConsumedAt = state, consumed
			m.Unread = m.Unread || state == ReviewConsumed || state == ReviewRunEnded
			session.LastActivity = time.Now().UTC()
			return true, nil
		}
		return false, storage.ErrNotFound
	})
}
